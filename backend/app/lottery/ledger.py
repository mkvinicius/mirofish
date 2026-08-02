"""
Caderneta de apostas: o extrato honesto da jornada do apostador.

Guarda os jogos que o usuario REALMENTE registrou na Caixa e os confere
automaticamente contra os resultados oficiais em cache. A conferencia nao e
armazenada — e recalculada a cada leitura contra o historico mais recente,
entao nunca fica desatualizada nem inconsistente com os dados.

O objetivo declarado deste modulo e psicologico tanto quanto contabil:
nada combate a ilusao de "estou quase ganhando" melhor do que o proprio
saldo acumulado, calculado com os premios reais de cada concurso.
"""

import json
import os
import threading
import uuid
from datetime import datetime
from typing import Any, Dict, List, Optional, Sequence

from ..utils.logger import get_logger
from .catalog import LotteryDef, get_lottery
from .data_source import Draw, HistoryStore

logger = get_logger('mirofish.lottery.ledger')

DATA_DIR = os.path.join(os.path.dirname(__file__), '..', '..', 'data')
LEDGER_PATH = os.path.join(DATA_DIR, 'minhas_apostas.jsonl')

_lock = threading.Lock()


def _load_raw() -> List[Dict[str, Any]]:
    if not os.path.exists(LEDGER_PATH):
        return []
    out: List[Dict[str, Any]] = []
    with open(LEDGER_PATH, 'r', encoding='utf-8') as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError:
                logger.warning("Linha inválida ignorada na caderneta")
    return out


def _save_raw(records: List[Dict[str, Any]]) -> None:
    os.makedirs(DATA_DIR, exist_ok=True)
    tmp = LEDGER_PATH + '.tmp'
    with open(tmp, 'w', encoding='utf-8') as fh:
        for rec in records:
            fh.write(json.dumps(rec, ensure_ascii=False, separators=(',', ':')) + '\n')
    os.replace(tmp, LEDGER_PATH)


def add_bet(
    modalidade: str,
    jogos: Sequence[Sequence[int]],
    concurso: Optional[int] = None,
) -> Dict[str, Any]:
    """Registra uma aposta na caderneta.

    Args:
        concurso: numero do concurso alvo. Se omitido, assume o proximo
                  (ultimo em cache + 1).

    Apenas apostas simples (exatamente `picks` dezenas por jogo) — e o que o
    boletim gera; apostas com mais dezenas tem rateio combinatorio proprio
    que a conferencia simples nao cobre.
    """
    lottery = get_lottery(modalidade)

    if not jogos:
        raise ValueError("Informe ao menos um jogo")

    clean_games: List[List[int]] = []
    for i, game in enumerate(jogos, 1):
        try:
            numbers = sorted({int(n) for n in game})
        except (TypeError, ValueError):
            raise ValueError(f"Jogo {i}: dezenas devem ser números inteiros")
        if len(numbers) != lottery.picks:
            raise ValueError(
                f"Jogo {i}: uma aposta simples de {lottery.name} tem exatamente "
                f"{lottery.picks} dezenas distintas (recebi {len(numbers)})"
            )
        if numbers[0] < 1 or numbers[-1] > lottery.total_numbers:
            raise ValueError(
                f"Jogo {i}: dezenas devem estar entre 1 e {lottery.total_numbers}"
            )
        clean_games.append(numbers)

    draws = HistoryStore(lottery).load()
    latest = draws[-1].number if draws else 0

    if concurso is None:
        concurso = latest + 1
    concurso = int(concurso)
    if concurso < 1:
        raise ValueError("Concurso inválido")

    record = {
        "id": f"ap_{uuid.uuid4().hex[:10]}",
        "modalidade": lottery.slug,
        "concurso": concurso,
        "jogos": clean_games,
        "custo": round(len(clean_games) * lottery.base_price, 2),
        "criado_em": datetime.now().isoformat(timespec='seconds'),
    }

    with _lock:
        records = _load_raw()
        records.append(record)
        _save_raw(records)

    return record


def delete_bet(bet_id: str) -> bool:
    with _lock:
        records = _load_raw()
        remaining = [r for r in records if r.get("id") != bet_id]
        if len(remaining) == len(records):
            return False
        _save_raw(remaining)
    return True


def _settle(record: Dict[str, Any], draw: Optional[Draw], lottery: LotteryDef) -> Dict[str, Any]:
    """Confere uma aposta contra o sorteio (se ja disponivel)."""
    out = dict(record)
    if draw is None:
        out["status"] = "aguardando"
        out["resultado"] = None
        return out

    drawn = set(draw.numbers)
    games_out: List[Dict[str, Any]] = []
    total_prize = 0.0
    best_hits = 0

    for numbers in record["jogos"]:
        matched = sorted(drawn & set(numbers))
        hits = len(matched)
        best_hits = max(best_hits, hits)
        tier = draw.prizes.get(hits)
        prize = tier.value if (tier is not None and hits in lottery.prize_tiers) else 0.0
        total_prize += prize
        games_out.append({
            "dezenas": numbers,
            "acertos": hits,
            "acertadas": matched,
            "premio": round(prize, 2),
        })

    out["status"] = "conferida"
    out["resultado"] = {
        "sorteio": list(draw.numbers),
        "data_sorteio": draw.date,
        "jogos": games_out,
        "melhor_jogo": best_hits,
        "premio_total": round(total_prize, 2),
        "saldo": round(total_prize - record["custo"], 2),
    }
    return out


def list_bets(sync_pending: bool = False) -> Dict[str, Any]:
    """Lista as apostas conferidas contra o cache, com resumo consolidado.

    Args:
        sync_pending: tenta baixar da Caixa os concursos que faltam para
                      conferir apostas pendentes (best-effort, rede fora nao
                      derruba a listagem).
    """
    records = _load_raw()

    by_lottery: Dict[str, List[Dict[str, Any]]] = {}
    for rec in records:
        by_lottery.setdefault(rec["modalidade"], []).append(rec)

    settled: List[Dict[str, Any]] = []
    summary = {
        "n_apostas": len(records),
        "n_conferidas": 0,
        "n_aguardando": 0,
        "gasto_total": 0.0,
        "premio_total": 0.0,
        "saldo": 0.0,
        "por_modalidade": {},
    }

    for slug, recs in by_lottery.items():
        lottery = get_lottery(slug)
        store = HistoryStore(lottery)
        draws = store.load()
        latest = draws[-1].number if draws else 0

        pending_targets = [r["concurso"] for r in recs if r["concurso"] > latest]
        if sync_pending and pending_targets:
            try:
                store.sync(max_workers=4, limit=20)
                draws = store.load()
            except Exception as exc:
                logger.warning("Sync para conferência falhou (%s): %s", slug, exc)

        by_number = {d.number: d for d in draws}
        mod_stats = {"gasto": 0.0, "premio": 0.0, "apostas": 0}

        for rec in recs:
            draw = by_number.get(rec["concurso"])
            item = _settle(rec, draw, lottery)
            settled.append(item)

            summary["gasto_total"] += rec["custo"]
            mod_stats["gasto"] += rec["custo"]
            mod_stats["apostas"] += 1
            if item["status"] == "conferida":
                summary["n_conferidas"] += 1
                summary["premio_total"] += item["resultado"]["premio_total"]
                mod_stats["premio"] += item["resultado"]["premio_total"]
            else:
                summary["n_aguardando"] += 1

        mod_stats = {k: round(v, 2) if isinstance(v, float) else v for k, v in mod_stats.items()}
        mod_stats["saldo"] = round(mod_stats["premio"] - mod_stats["gasto"], 2)
        summary["por_modalidade"][slug] = mod_stats

    summary["gasto_total"] = round(summary["gasto_total"], 2)
    summary["premio_total"] = round(summary["premio_total"], 2)
    summary["saldo"] = round(summary["premio_total"] - summary["gasto_total"], 2)

    settled.sort(key=lambda r: (r.get("criado_em", ""), r.get("id", "")), reverse=True)
    return {"apostas": settled, "resumo": summary}
