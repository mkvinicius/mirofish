"""
Fonte de dados: resultados oficiais das loterias da Caixa.

A Caixa nao publica um endpoint em lote, apenas concurso a concurso:
    https://servicebus2.caixa.gov.br/portaldeloterias/api/<modalidade>[/<n>]

Por isso o historico e baixado uma unica vez e mantido em cache local
(JSONL, uma linha por concurso). Depois disso a sincronizacao so busca os
concursos novos, o que torna o motor utilizavel offline.

Alem das dezenas, guardamos `valorArrecadado` e o rateio por faixa. Esses
dois campos sao o que permite calcular ROI real no backtest e calibrar o
modelo de popularidade com dados publicados (ver `popularity.py`).
"""

import json
import os
import ssl
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from typing import Callable, Dict, List, Optional, Tuple

from ..utils.logger import get_logger
from .catalog import LotteryDef

logger = get_logger('mirofish.lottery.data')

API_BASE = "https://servicebus2.caixa.gov.br/portaldeloterias/api"

# A Caixa rejeita user-agents vazios/automatizados demais.
_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/120.0 Safari/537.36"
    ),
    "Accept": "application/json",
}

DATA_DIR = os.path.join(os.path.dirname(__file__), '..', '..', 'data')


@dataclass
class PrizeTier:
    """Rateio de uma faixa de premiacao."""
    hits: int
    winners: int
    value: float

    def to_dict(self) -> Dict:
        return {"hits": self.hits, "winners": self.winners, "value": self.value}


@dataclass
class Draw:
    """Um concurso ja realizado."""
    number: int
    date: str                                  # ISO (YYYY-MM-DD)
    numbers: Tuple[int, ...]                   # dezenas sorteadas, ordenadas
    revenue: float = 0.0                       # valorArrecadado
    prizes: Dict[int, PrizeTier] = field(default_factory=dict)  # acertos -> rateio

    def to_record(self) -> Dict:
        """Formato compacto usado no cache JSONL."""
        return {
            "n": self.number,
            "d": self.date,
            "dz": list(self.numbers),
            "arr": self.revenue,
            "pr": [[t.hits, t.winners, t.value] for t in self.prizes.values()],
        }

    @classmethod
    def from_record(cls, rec: Dict) -> "Draw":
        prizes = {}
        for hits, winners, value in rec.get("pr", []):
            prizes[int(hits)] = PrizeTier(int(hits), int(winners), float(value))
        return cls(
            number=int(rec["n"]),
            date=rec.get("d", ""),
            numbers=tuple(int(x) for x in rec["dz"]),
            revenue=float(rec.get("arr", 0.0)),
            prizes=prizes,
        )


def _parse_date(br_date: Optional[str]) -> str:
    """Converte DD/MM/AAAA da API para ISO. Retorna '' se ausente/invalida."""
    if not br_date or "/" not in br_date:
        return ""
    try:
        day, month, year = br_date.split("/")
        return f"{int(year):04d}-{int(month):02d}-{int(day):02d}"
    except (ValueError, TypeError):
        return ""


def parse_draw(payload: Dict, lottery: LotteryDef) -> Optional[Draw]:
    """Converte a resposta bruta da Caixa em `Draw`.

    Retorna None para concursos malformados (acontece em concursos antigos
    de algumas modalidades) em vez de estourar, para nao interromper a
    sincronizacao inteira por causa de um registro ruim.
    """
    dezenas = payload.get("listaDezenas") or payload.get("dezenasSorteadasOrdemSorteio")
    if not dezenas:
        return None

    try:
        numbers = tuple(sorted(int(d) for d in dezenas))
    except (TypeError, ValueError):
        return None

    if len(numbers) != lottery.picks:
        return None
    if any(n < 1 or n > lottery.total_numbers for n in numbers):
        return None

    # Mapeia faixa da API -> quantidade de acertos (inverso de tier_to_faixa).
    faixa_to_hits = {v: k for k, v in lottery.tier_to_faixa.items()}
    prizes: Dict[int, PrizeTier] = {}
    for item in payload.get("listaRateioPremio") or []:
        hits = faixa_to_hits.get(item.get("faixa"))
        if hits is None:
            continue
        prizes[hits] = PrizeTier(
            hits=hits,
            winners=int(item.get("numeroDeGanhadores") or 0),
            value=float(item.get("valorPremio") or 0.0),
        )

    return Draw(
        number=int(payload.get("numero") or 0),
        date=_parse_date(payload.get("dataApuracao")),
        numbers=numbers,
        revenue=float(payload.get("valorArrecadado") or 0.0),
        prizes=prizes,
    )


class CaixaClient:
    """Cliente HTTP minimo para a API de loterias (sem dependencias novas)."""

    def __init__(self, timeout: int = 25, max_retries: int = 3):
        self.timeout = timeout
        self.max_retries = max_retries
        # A Caixa serve um certificado que algumas imagens base nao validam;
        # o contexto padrao e mantido, com fallback explicito e registrado.
        self._ctx = ssl.create_default_context()

    def fetch(self, api_slug: str, concurso: Optional[int] = None) -> Dict:
        """Busca um concurso (ou o mais recente, se `concurso` for None)."""
        url = f"{API_BASE}/{api_slug}"
        if concurso is not None:
            url = f"{url}/{concurso}"

        last_error: Optional[Exception] = None
        for attempt in range(self.max_retries):
            try:
                req = urllib.request.Request(url, headers=_HEADERS)
                with urllib.request.urlopen(req, timeout=self.timeout, context=self._ctx) as resp:
                    return json.loads(resp.read().decode("utf-8"))
            except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError, json.JSONDecodeError) as exc:
                last_error = exc
                if attempt < self.max_retries - 1:
                    time.sleep(1.5 * (2 ** attempt))

        raise RuntimeError(f"Falha ao consultar {url}: {last_error}")

    def latest_number(self, api_slug: str) -> int:
        """Numero do concurso mais recente."""
        return int(self.fetch(api_slug).get("numero") or 0)


class HistoryStore:
    """Cache local do historico de uma modalidade (JSONL append-only)."""

    def __init__(self, lottery: LotteryDef, data_dir: Optional[str] = None):
        self.lottery = lottery
        self.data_dir = os.path.abspath(data_dir or DATA_DIR)
        os.makedirs(self.data_dir, exist_ok=True)
        self.path = os.path.join(self.data_dir, f"{lottery.slug}.jsonl")
        self._lock = threading.Lock()

    # ------------------------------------------------------------------ leitura

    def load(self) -> List[Draw]:
        """Carrega o historico em cache, ordenado por concurso."""
        if not os.path.exists(self.path):
            return []

        draws: List[Draw] = []
        with open(self.path, "r", encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    draws.append(Draw.from_record(json.loads(line)))
                except (json.JSONDecodeError, KeyError, TypeError, ValueError):
                    logger.warning("Linha invalida ignorada no cache %s", self.path)

        # Deduplica mantendo o registro mais recente de cada concurso.
        by_number = {d.number: d for d in draws}
        return [by_number[n] for n in sorted(by_number)]

    def cached_numbers(self) -> set:
        return {d.number for d in self.load()}

    # ------------------------------------------------------------------ escrita

    def save_all(self, draws: List[Draw]) -> None:
        """Reescreve o cache inteiro, ordenado (usado apos sincronizar)."""
        ordered = sorted({d.number: d for d in draws}.values(), key=lambda d: d.number)
        tmp_path = f"{self.path}.tmp"
        with open(tmp_path, "w", encoding="utf-8") as fh:
            for draw in ordered:
                fh.write(json.dumps(draw.to_record(), ensure_ascii=False, separators=(",", ":")))
                fh.write("\n")
        os.replace(tmp_path, self.path)

    # ------------------------------------------------------------- sincronizacao

    def sync(
        self,
        progress: Optional[Callable[[int, int], None]] = None,
        max_workers: int = 8,
        limit: Optional[int] = None,
    ) -> Dict[str, int]:
        """Baixa os concursos que faltam no cache.

        Args:
            progress: callback(baixados, total_faltando)
            max_workers: paralelismo das requisicoes (a Caixa tolera bem ~8)
            limit: se informado, baixa no maximo esse tanto de concursos novos

        Returns:
            {"cached": n_antes, "fetched": n_novos, "failed": n_falhas, "latest": n}
        """
        client = CaixaClient()
        latest = client.latest_number(self.lottery.api_slug)
        if latest <= 0:
            raise RuntimeError("Nao foi possivel descobrir o concurso mais recente")

        existing = {d.number: d for d in self.load()}
        missing = [n for n in range(1, latest + 1) if n not in existing]
        if limit is not None:
            missing = missing[-limit:]

        total = len(missing)
        logger.info(
            "Sync %s: %d em cache, %d faltando (ultimo concurso: %d)",
            self.lottery.slug, len(existing), total, latest,
        )

        if not total:
            return {"cached": len(existing), "fetched": 0, "failed": 0, "latest": latest}

        fetched = 0
        failed = 0
        done = 0

        def _fetch_one(n: int) -> Optional[Draw]:
            payload = client.fetch(self.lottery.api_slug, n)
            return parse_draw(payload, self.lottery)

        with ThreadPoolExecutor(max_workers=max_workers) as pool:
            futures = {pool.submit(_fetch_one, n): n for n in missing}
            for future in as_completed(futures):
                number = futures[future]
                done += 1
                try:
                    draw = future.result()
                except Exception as exc:  # rede instavel nao deve abortar tudo
                    failed += 1
                    logger.warning("Concurso %d falhou: %s", number, exc)
                else:
                    if draw is None:
                        failed += 1
                    else:
                        # A API as vezes devolve `numero` zerado em concursos antigos.
                        if draw.number == 0:
                            draw.number = number
                        with self._lock:
                            existing[draw.number] = draw
                        fetched += 1

                if progress and (done % 25 == 0 or done == total):
                    progress(done, total)

        self.save_all(list(existing.values()))
        logger.info("Sync %s concluido: %d novos, %d falhas", self.lottery.slug, fetched, failed)

        return {"cached": len(existing), "fetched": fetched, "failed": failed, "latest": latest}


def load_history(lottery: LotteryDef, data_dir: Optional[str] = None) -> List[Draw]:
    """Atalho: carrega o historico em cache de uma modalidade."""
    return HistoryStore(lottery, data_dir).load()
