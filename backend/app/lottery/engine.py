"""
Orquestrador: conduz um estudo completo, do download dos resultados ao boletim.

Mantem a mesma progressao em etapas do MiroFish original — o que muda e o
conteudo de cada etapa:

    1. dados      (antes: upload de documentos)  -> resultados oficiais da Caixa
    2. analise    (antes: ontologia + grafo)     -> estatisticas + popularidade
    3. mundos     (antes: perfis de agentes)     -> estrategias candidatas
    4. backtest   (antes: simulacao social)      -> competicao walk-forward
    5. boletim    (antes: relatorio do agente)   -> apostas + garantias + VE
"""

import json
import os
import uuid
from collections import Counter
from dataclasses import dataclass, field, asdict
from datetime import datetime
from typing import Any, Callable, Dict, List, Optional, Sequence

import numpy as np

from ..utils.logger import get_logger
from . import combinatorics as cb
from .analyzer import FeatureSet, analyze
from .backtest import BacktestReport, run_backtest
from .catalog import LotteryDef, get_lottery
from .data_source import Draw, HistoryStore, load_history
from .economics import expected_value, sharing_adjusted_value
from .popularity import fit_popularity
from .wheeling import build_wheel, certify
from .worlds import DEFAULT_WORLDS, SpaceCache, WorldContext, get_world

logger = get_logger('mirofish.lottery.engine')

RUNS_DIR = os.path.join(os.path.dirname(__file__), '..', '..', 'uploads', 'lottery_runs')

PORTFOLIO_MODES = ("valor_esperado", "fechamento", "diversificado")


@dataclass
class RunConfig:
    """Parametros de um estudo."""

    lottery: str = "lotofacil"
    n_games: int = 8
    worlds: List[str] = field(default_factory=lambda: list(DEFAULT_WORLDS))
    portfolio_mode: str = "valor_esperado"

    # Backtest
    run_backtest: bool = True
    backtest_draws: int = 60
    backtest_games: int = 5
    backtest_space_sample: int = 400_000

    # Fechamento
    wheel_base_size: int = 18
    wheel_target_c: Optional[int] = None
    wheel_target_hits: Optional[int] = None

    # Dados
    sync_before_run: bool = True
    seed: int = 2024

    def validate(self) -> List[str]:
        errors: List[str] = []
        if self.n_games < 1 or self.n_games > 100:
            errors.append("n_games deve estar entre 1 e 100")
        if self.portfolio_mode not in PORTFOLIO_MODES:
            errors.append(f"portfolio_mode deve ser um de {PORTFOLIO_MODES}")
        if self.backtest_draws < 10 or self.backtest_draws > 500:
            errors.append("backtest_draws deve estar entre 10 e 500")
        if not self.worlds:
            errors.append("selecione ao menos um mundo")
        return errors

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class RunProgress:
    """Estado de progresso, consumido pela API durante a execucao."""

    step: str = "criado"
    percent: int = 0
    message: str = ""
    updated_at: str = field(default_factory=lambda: datetime.now().isoformat())

    def to_dict(self) -> Dict[str, Any]:
        return {
            "etapa": self.step,
            "percentual": self.percent,
            "mensagem": self.message,
            "atualizado_em": self.updated_at,
        }


class LotteryEngine:
    """Executa um estudo completo para uma modalidade."""

    def __init__(self, config: RunConfig, data_dir: Optional[str] = None):
        self.config = config
        self.lottery: LotteryDef = get_lottery(config.lottery)
        self.data_dir = data_dir
        self.progress = RunProgress()
        self._on_progress: Optional[Callable[[RunProgress], None]] = None

    # ------------------------------------------------------------------ progresso

    def set_progress_callback(self, callback: Callable[[RunProgress], None]) -> None:
        self._on_progress = callback

    def _report(self, step: str, percent: int, message: str) -> None:
        self.progress = RunProgress(step=step, percent=percent, message=message)
        logger.info("[%s] %d%% — %s", step, percent, message)
        if self._on_progress:
            self._on_progress(self.progress)

    # ------------------------------------------------------------------- execucao

    def run(self) -> Dict[str, Any]:
        """Executa as cinco etapas e devolve o boletim completo."""
        errors = self.config.validate()
        if errors:
            raise ValueError("Configuração inválida: " + "; ".join(errors))

        started = datetime.now()

        # ---------------------------------------------------------- 1. dados
        self._report("dados", 5, "carregando resultados oficiais")
        draws = self._load_draws()
        self._report(
            "dados", 15,
            f"{len(draws)} concursos carregados (até o {draws[-1].number}, {draws[-1].date})",
        )

        # -------------------------------------------------------- 2. analise
        self._report("analise", 20, "calculando estatísticas do histórico")
        features = FeatureSet(self.lottery)
        analysis = analyze(draws, self.lottery, features)

        self._report("analise", 28, "calibrando modelo de popularidade")
        popularity = fit_popularity(draws, self.lottery, features)

        ev = expected_value(self.lottery, draws)

        # --------------------------------------------------------- 3. mundos
        self._report("mundos", 35, f"enumerando {self.lottery.total_combinations:,} jogos possíveis")
        space = SpaceCache.build(self.lottery, features)
        self._report(
            "mundos", 45,
            f"espaço completo em memória ({space.memory_mb():.0f} MB)",
        )

        ctx = WorldContext(
            lottery=self.lottery,
            space=space,
            analysis=analysis,
            popularity=popularity,
            feature_set=features,
            seed=self.config.seed,
        )

        world_games: Dict[str, List[Dict[str, Any]]] = {}
        world_meta: List[Dict[str, str]] = []
        for slug in self.config.worlds:
            world = get_world(slug)
            world_meta.append(world.to_dict())
            candidates = world.generate(ctx, self.config.n_games)
            world_games[slug] = [c.to_dict() for c in candidates]

        # ------------------------------------------------------- 4. backtest
        backtest_report: Optional[BacktestReport] = None
        if self.config.run_backtest:
            self._report("backtest", 50, "iniciando competição walk-forward")

            def _bt_progress(done: int, total: int, msg: str) -> None:
                pct = 50 + int(35 * done / max(total, 1))
                self._report("backtest", pct, f"backtest {done}/{total} ({msg})")

            backtest_report = run_backtest(
                draws,
                self.lottery,
                self.config.worlds,
                n_test_draws=self.config.backtest_draws,
                n_games_per_draw=self.config.backtest_games,
                space_sample=self.config.backtest_space_sample,
                seed=self.config.seed,
                progress=_bt_progress,
                space=space,
                feature_set=features,
            )

        # -------------------------------------------------------- 5. boletim
        self._report("boletim", 88, "montando o boletim de apostas")
        portfolio = self._build_portfolio(ctx, space, ev)

        self._report("boletim", 96, "certificando garantias")
        certificate = certify(
            self.lottery,
            [g["dezenas"] for g in portfolio["jogos"]],
            portfolio["conjunto_base"],
            space.masks,
        )

        elapsed = (datetime.now() - started).total_seconds()
        self._report("concluido", 100, f"estudo concluído em {elapsed:.0f}s")

        return {
            "modalidade": {
                "slug": self.lottery.slug,
                "nome": self.lottery.name,
                "universo": self.lottery.total_numbers,
                "dezenas_sorteadas": self.lottery.picks,
                "combinacoes": self.lottery.total_combinations,
                "preco_aposta": self.lottery.base_price,
            },
            "config": self.config.to_dict(),
            "dados": {
                "concursos": len(draws),
                "primeiro": draws[0].number,
                "ultimo": draws[-1].number,
                "data_ultimo": draws[-1].date,
                "ultimo_sorteio": list(draws[-1].numbers),
            },
            "analise": analysis.to_dict(),
            "popularidade": popularity.to_dict(),
            "valor_esperado": ev.to_dict(),
            "mundos": world_meta,
            "jogos_por_mundo": world_games,
            "backtest": backtest_report.to_dict() if backtest_report else None,
            "boletim": portfolio,
            "certificado": certificate.to_dict(),
            "avisos": self._disclaimers(ev),
            "tempo_execucao_s": round(elapsed, 1),
            "gerado_em": datetime.now().isoformat(),
        }

    # -------------------------------------------------------------- auxiliares

    def _load_draws(self) -> List[Draw]:
        store = HistoryStore(self.lottery, self.data_dir)
        if self.config.sync_before_run:
            try:
                stats = store.sync()
                logger.info("Sincronização: %s", stats)
            except Exception as exc:
                # Sem rede o estudo ainda roda com o cache local — melhor
                # entregar com dados de ontem do que nao entregar.
                logger.warning("Sincronização falhou (%s); usando cache local", exc)

        draws = store.load()
        if not draws:
            raise RuntimeError(
                "Nenhum resultado em cache e a sincronização falhou. "
                "Verifique a conexão com a API da Caixa."
            )
        return draws

    def _build_portfolio(
        self,
        ctx: WorldContext,
        space: SpaceCache,
        ev,
    ) -> Dict[str, Any]:
        """Monta o conjunto final de apostas conforme o modo escolhido."""
        mode = self.config.portfolio_mode
        n_games = self.config.n_games

        if mode == "fechamento":
            games, base_numbers, notes = self._portfolio_wheel(ctx, n_games)
        elif mode == "diversificado":
            games, base_numbers, notes = self._portfolio_diverse(ctx, n_games)
        else:
            games, base_numbers, notes = self._portfolio_value(ctx, n_games)

        # Enriquece cada jogo com rateio estimado e valor esperado ajustado.
        enriched: List[Dict[str, Any]] = []
        for numbers in games:
            values = ctx.feature_set.evaluate_one(numbers)
            arrays = {k: np.array([v]) for k, v in values.items()}
            try:
                multiplier = float(ctx.popularity.sharing_multiplier(arrays)[0])
            except KeyError:
                multiplier = 1.0
            enriched.append({
                "dezenas": list(numbers),
                "features": {k: round(float(v), 1) for k, v in values.items()},
                "rateio_estimado": round(multiplier, 3),
                "valor_esperado": sharing_adjusted_value(ev, multiplier),
            })

        cost = len(enriched) * self.lottery.base_price
        return {
            "modo": mode,
            "jogos": enriched,
            "conjunto_base": base_numbers,
            "n_jogos": len(enriched),
            "custo_total": round(cost, 2),
            "observacoes": notes,
        }

    def _portfolio_value(self, ctx: WorldContext, n_games: int):
        """Todos os jogos vindos do mundo com justificativa economica."""
        world = get_world("hibrido" if "hibrido" in self.config.worlds else self.config.worlds[0])
        candidates = world.generate(ctx, n_games)
        games = [c.numbers for c in candidates]
        base = sorted({n for g in games for n in g})
        notes = [
            f"Todos os jogos vêm do mundo '{world.name}'.",
            "A justificativa é de valor esperado (rateio menor), não de "
            "probabilidade de acerto — que é idêntica para qualquer jogo.",
        ]
        return games, base, notes

    def _portfolio_diverse(self, ctx: WorldContext, n_games: int):
        """Divide o orcamento entre os mundos selecionados."""
        slugs = [s for s in self.config.worlds if s != "uniforme"] or list(self.config.worlds)
        games: List[Sequence[int]] = []
        per_world = max(1, n_games // len(slugs))

        for slug in slugs:
            if len(games) >= n_games:
                break
            world = get_world(slug)
            for candidate in world.generate(ctx, per_world):
                if len(games) < n_games:
                    games.append(candidate.numbers)

        # Completa o orcamento com o mundo de valor esperado, se sobrou espaco.
        if len(games) < n_games:
            filler = get_world("hibrido")
            for candidate in filler.generate(ctx, n_games - len(games)):
                games.append(candidate.numbers)

        base = sorted({n for g in games for n in g})
        notes = [
            f"Orçamento dividido entre {len(slugs)} mundos ({per_world} jogo(s) cada).",
            "Modo pensado para comparar as hipóteses na prática, não para "
            "maximizar valor esperado.",
        ]
        return games, base, notes

    def _portfolio_wheel(self, ctx: WorldContext, n_games: int):
        """Fechamento sobre as dezenas preferidas pelo mundo de valor esperado."""
        base_size = min(self.config.wheel_base_size, self.lottery.total_numbers)
        world = get_world("hibrido" if "hibrido" in self.config.worlds else self.config.worlds[0])

        # Conjunto base: dezenas mais recorrentes entre os melhores jogos do mundo.
        top = world.generate(ctx, 40)
        counter: Counter = Counter()
        for candidate in top:
            counter.update(candidate.numbers)
        base_numbers = sorted(n for n, _ in counter.most_common(base_size))

        # Se o mundo nao cobriu dezenas suficientes, completa pelas mais frequentes.
        if len(base_numbers) < base_size:
            for number in ctx.analysis.hot_numbers(self.lottery.total_numbers):
                if number not in base_numbers:
                    base_numbers.append(number)
                if len(base_numbers) >= base_size:
                    break
            base_numbers = sorted(base_numbers)

        wheel = build_wheel(
            self.lottery,
            base_numbers,
            n_games=n_games,
            target_c=self.config.wheel_target_c,
            target_hits=self.config.wheel_target_hits,
            seed=self.config.seed,
        )

        notes = [
            f"Fechamento sobre {len(base_numbers)} dezenas escolhidas pelo mundo "
            f"'{world.name}'.",
            "A garantia abaixo é verificada por enumeração exaustiva de todos os "
            f"{self.lottery.total_combinations:,} sorteios possíveis — é um teorema "
            "sobre este bilhete, não uma estimativa.",
        ] + wheel.notes

        return wheel.games, wheel.base_numbers, notes

    def _disclaimers(self, ev) -> List[str]:
        return [
            f"Cada R$ {ev.bet_price:.2f} apostados devolvem, em média, "
            f"R$ {ev.expected_return:.2f} ({ev.payout_ratio:.1%}). A loteria é um "
            f"jogo de retorno esperado negativo por desenho.",
            "Nenhuma estratégia deste sistema aumenta a chance de acertar. As "
            "chances são fixas e idênticas para qualquer jogo.",
            "O que o sistema otimiza: o rateio esperado do prêmio (jogar onde há "
            "menos apostadores) e a garantia de prêmios secundários (fechamento).",
            "Aposte apenas o que puder perder integralmente.",
        ]


# ----------------------------------------------------------------- persistencia

class LotteryRunManager:
    """Armazena os estudos executados em disco (JSON por estudo)."""

    RUNS_DIR = RUNS_DIR

    @classmethod
    def _ensure_dir(cls) -> None:
        os.makedirs(cls.RUNS_DIR, exist_ok=True)

    @classmethod
    def _path(cls, run_id: str) -> str:
        return os.path.join(cls.RUNS_DIR, f"{run_id}.json")

    @classmethod
    def new_id(cls) -> str:
        return f"lot_{uuid.uuid4().hex[:12]}"

    @classmethod
    def save(cls, run_id: str, payload: Dict[str, Any]) -> None:
        cls._ensure_dir()
        tmp = cls._path(run_id) + ".tmp"
        with open(tmp, "w", encoding="utf-8") as fh:
            json.dump(payload, fh, ensure_ascii=False, indent=2)
        os.replace(tmp, cls._path(run_id))

    @classmethod
    def get(cls, run_id: str) -> Optional[Dict[str, Any]]:
        path = cls._path(run_id)
        if not os.path.exists(path):
            return None
        with open(path, "r", encoding="utf-8") as fh:
            return json.load(fh)

    @classmethod
    def list(cls, limit: int = 50) -> List[Dict[str, Any]]:
        cls._ensure_dir()
        items: List[Dict[str, Any]] = []
        for name in os.listdir(cls.RUNS_DIR):
            if not name.endswith(".json"):
                continue
            data = cls.get(name[:-5])
            if not data:
                continue
            items.append({
                "run_id": name[:-5],
                "modalidade": data.get("modalidade", {}).get("slug"),
                "status": data.get("status", "concluido"),
                "gerado_em": data.get("gerado_em", ""),
                "n_jogos": data.get("boletim", {}).get("n_jogos", 0),
                "custo_total": data.get("boletim", {}).get("custo_total", 0),
            })
        items.sort(key=lambda x: x.get("gerado_em", ""), reverse=True)
        return items[:limit]

    @classmethod
    def delete(cls, run_id: str) -> bool:
        path = cls._path(run_id)
        if not os.path.exists(path):
            return False
        os.remove(path)
        return True
