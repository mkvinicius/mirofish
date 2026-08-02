"""
Backtest walk-forward: onde as hipoteses sao confrontadas com a realidade.

Para cada concurso alvo, cada mundo enxerga **apenas** os concursos
anteriores, gera seus jogos, e so entao o resultado real e revelado. Nao ha
como um mundo se beneficiar de informacao futura, que e o erro classico que
faz qualquer estrategia de loteria parecer boa no papel.

O criterio de sucesso e explicito: um mundo so agrega valor se superar o
mundo de controle (sorteio uniforme) por uma margem maior que o ruido
amostral. O relatorio inclui o teste estatistico dessa comparacao, com a
unidade de analise correta — a media por concurso, e nao por jogo, ja que
jogos do mesmo concurso sao fortemente correlacionados.
"""

from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Sequence

import numpy as np

from . import combinatorics as cb
from .analyzer import FeatureSet, analyze
from .catalog import LotteryDef
from .data_source import Draw
from .popularity import fit_popularity
from .worlds import SpaceCache, WorldContext, get_world


@dataclass
class WorldResult:
    """Desempenho de um mundo no backtest."""

    slug: str
    name: str
    n_draws: int
    n_games_per_draw: int
    hits_histogram: Dict[int, int] = field(default_factory=dict)
    prize_hits: Dict[int, int] = field(default_factory=dict)   # faixa -> nº de premios
    total_prize: float = 0.0
    total_cost: float = 0.0
    mean_hits: float = 0.0
    per_draw_mean_hits: List[float] = field(default_factory=list)

    @property
    def total_games(self) -> int:
        return self.n_draws * self.n_games_per_draw

    @property
    def roi(self) -> float:
        """Retorno sobre o investido. -1.0 = perdeu tudo."""
        if self.total_cost <= 0:
            return 0.0
        return (self.total_prize - self.total_cost) / self.total_cost

    def to_dict(self) -> Dict[str, Any]:
        return {
            "mundo": self.slug,
            "nome": self.name,
            "concursos_testados": self.n_draws,
            "jogos_por_concurso": self.n_games_per_draw,
            "total_jogos": self.total_games,
            "media_acertos": round(self.mean_hits, 4),
            "histograma_acertos": {str(k): v for k, v in sorted(self.hits_histogram.items())},
            "premios_por_faixa": {str(k): v for k, v in sorted(self.prize_hits.items(), reverse=True)},
            "premio_total": round(self.total_prize, 2),
            "custo_total": round(self.total_cost, 2),
            "resultado_liquido": round(self.total_prize - self.total_cost, 2),
            "roi": round(self.roi, 4),
        }


@dataclass
class BacktestReport:
    """Resultado completo do backtest, incluindo o veredito estatistico."""

    lottery: LotteryDef
    results: Dict[str, WorldResult]
    first_draw: int
    last_draw: int
    n_games_per_draw: int
    theoretical_mean_hits: float
    space_size: int
    space_sampled: bool
    verdicts: List[str] = field(default_factory=list)
    comparisons: Dict[str, Dict[str, float]] = field(default_factory=dict)

    def ranking(self) -> List[WorldResult]:
        """Mundos ordenados por ROI (o que o apostador de fato sente)."""
        return sorted(self.results.values(), key=lambda r: r.roi, reverse=True)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "modalidade": self.lottery.slug,
            "concurso_inicial": self.first_draw,
            "concurso_final": self.last_draw,
            "jogos_por_concurso": self.n_games_per_draw,
            "media_acertos_teorica": round(self.theoretical_mean_hits, 4),
            "espaco_avaliado": self.space_size,
            "espaco_amostrado": self.space_sampled,
            "ranking": [r.to_dict() for r in self.ranking()],
            "comparacao_com_controle": self.comparisons,
            "veredito": self.verdicts,
        }


def theoretical_mean_hits(lottery: LotteryDef) -> float:
    """Media de acertos de um jogo aleatorio (esperanca hipergeometrica)."""
    return lottery.picks * lottery.picks / lottery.total_numbers


def theoretical_std_hits(lottery: LotteryDef) -> float:
    """Desvio-padrao dos acertos de um jogo aleatorio."""
    n = lottery.total_numbers
    k = lottery.picks  # dezenas sorteadas
    d = lottery.picks  # dezenas marcadas (aposta minima)
    variance = d * (k / n) * ((n - k) / n) * ((n - d) / (n - 1))
    return float(np.sqrt(variance))


def run_backtest(
    draws: Sequence[Draw],
    lottery: LotteryDef,
    world_slugs: Sequence[str],
    n_test_draws: int = 100,
    n_games_per_draw: int = 5,
    train_window: Optional[int] = None,
    space_sample: Optional[int] = 400_000,
    seed: int = 20240,
    progress: Optional[Callable[[int, int, str], None]] = None,
    space: Optional[SpaceCache] = None,
    feature_set: Optional[FeatureSet] = None,
) -> BacktestReport:
    """Roda o backtest walk-forward.

    Args:
        n_test_draws: quantos concursos finais servem de teste
        n_games_per_draw: jogos que cada mundo aposta por concurso
        train_window: se informado, cada mundo so ve os N concursos anteriores
                      (janela deslizante); caso contrario ve todo o passado
        space_sample: avalia os mundos sobre uma amostra do espaco de jogos.
                      Amostragem uniforme nao favorece nenhum mundo, e reduz o
                      custo do backtest em ~8x. A geracao final de apostas
                      continua usando o espaco completo.
    """
    if len(draws) < n_test_draws + 50:
        raise ValueError(
            f"Histórico insuficiente: {len(draws)} concursos para testar "
            f"{n_test_draws} (é preciso deixar pelo menos 50 de treino)"
        )

    features = feature_set or FeatureSet(lottery)
    full_space = space or SpaceCache.build(lottery, features)

    # Amostragem do espaco para o backtest.
    sampled = False
    if space_sample and space_sample < full_space.size:
        rng = np.random.default_rng(seed)
        idx = rng.choice(full_space.size, size=space_sample, replace=False)
        idx.sort()
        eval_space = SpaceCache(
            lottery=lottery,
            masks=full_space.masks[idx],
            features={k: v[idx] for k, v in full_space.features.items()},
        )
        sampled = True
    else:
        eval_space = full_space

    worlds = [get_world(slug) for slug in world_slugs]
    results = {
        world.slug: WorldResult(
            slug=world.slug,
            name=world.name,
            n_draws=0,
            n_games_per_draw=n_games_per_draw,
        )
        for world in worlds
    }

    test_start = len(draws) - n_test_draws
    game_price = lottery.base_price

    for step, target_idx in enumerate(range(test_start, len(draws))):
        target = draws[target_idx]
        history = draws[:target_idx]
        if train_window:
            history = history[-train_window:]

        analysis = analyze(history, lottery, features)
        popularity = fit_popularity(history, lottery, features)
        target_mask = cb.mask_from_numbers(target.numbers)

        for world in worlds:
            ctx = WorldContext(
                lottery=lottery,
                space=eval_space,
                analysis=analysis,
                popularity=popularity,
                feature_set=features,
                # Semente distinta por concurso e por mundo, para que o mundo
                # de controle nao repita os mesmos jogos a cada passo.
                seed=seed + target.number * 131 + hash(world.slug) % 1000,
            )
            games = world.generate(ctx, n_games_per_draw)
            result = results[world.slug]
            result.n_draws += 1

            draw_hits: List[int] = []
            for game in games:
                hits = int(cb.popcount(
                    np.array([cb.mask_from_numbers(game.numbers)], dtype=eval_space.masks.dtype)
                    & eval_space.masks.dtype.type(target_mask)
                )[0])
                draw_hits.append(hits)
                result.hits_histogram[hits] = result.hits_histogram.get(hits, 0) + 1
                result.total_cost += game_price

                tier = target.prizes.get(hits)
                if tier is not None and hits in lottery.prize_tiers:
                    result.prize_hits[hits] = result.prize_hits.get(hits, 0) + 1
                    result.total_prize += tier.value

            if draw_hits:
                result.per_draw_mean_hits.append(float(np.mean(draw_hits)))

        if progress:
            progress(step + 1, n_test_draws, f"concurso {target.number}")

    for result in results.values():
        if result.per_draw_mean_hits:
            result.mean_hits = float(np.mean(result.per_draw_mean_hits))

    report = BacktestReport(
        lottery=lottery,
        results=results,
        first_draw=draws[test_start].number,
        last_draw=draws[-1].number,
        n_games_per_draw=n_games_per_draw,
        theoretical_mean_hits=theoretical_mean_hits(lottery),
        space_size=eval_space.size,
        space_sampled=sampled,
    )
    _add_verdicts(report)
    return report


def _add_verdicts(report: BacktestReport) -> None:
    """Compara cada mundo com o controle e escreve o veredito estatistico."""
    control = report.results.get("uniforme")
    theoretical = report.theoretical_mean_hits

    if control is None or not control.per_draw_mean_hits:
        report.verdicts.append(
            "Mundo de controle não foi executado — sem ele não há como separar "
            "habilidade de sorte. Rode o backtest incluindo o mundo 'uniforme'."
        )
        return

    control_values = np.array(control.per_draw_mean_hits, dtype=np.float64)

    for slug, result in report.results.items():
        if slug == "uniforme" or not result.per_draw_mean_hits:
            continue

        values = np.array(result.per_draw_mean_hits, dtype=np.float64)
        n = min(len(values), len(control_values))
        # Teste pareado: os dois mundos enfrentam exatamente os mesmos
        # concursos, entao a diferenca concurso a concurso elimina a variacao
        # comum (concursos "faceis" e "dificeis" afetam os dois igualmente).
        diff = values[:n] - control_values[:n]
        mean_diff = float(diff.mean())
        std_diff = float(diff.std(ddof=1)) if n > 1 else 0.0
        stderr = std_diff / np.sqrt(n) if std_diff > 0 else 0.0
        t_stat = mean_diff / stderr if stderr > 0 else 0.0

        report.comparisons[slug] = {
            "diferenca_media_acertos": round(mean_diff, 4),
            "erro_padrao": round(stderr, 4),
            "t": round(float(t_stat), 3),
            "concursos": n,
        }

        if abs(t_stat) < 2.0:
            report.verdicts.append(
                f"{result.name}: diferença de {mean_diff:+.3f} acerto por jogo em "
                f"relação ao controle (t={t_stat:+.2f}) — indistinguível de sorte, "
                f"como a teoria prevê."
            )
        elif t_stat >= 2.0:
            report.verdicts.append(
                f"{result.name}: superou o controle em {mean_diff:+.3f} acerto "
                f"(t={t_stat:+.2f}) em {n} concursos. Resultado a tratar com "
                f"ceticismo: com vários mundos testados ao mesmo tempo, um deles "
                f"se destacar por acaso é esperado."
            )
        else:
            report.verdicts.append(
                f"{result.name}: ficou {mean_diff:+.3f} acerto ABAIXO do controle "
                f"(t={t_stat:+.2f}) — a hipótese não só não ajuda como concentrou "
                f"os jogos numa região pior."
            )

    report.verdicts.append(
        f"Média de acertos de um jogo aleatório (valor teórico): "
        f"{theoretical:.2f}. Controle observado: {control.mean_hits:.2f}."
    )
    report.verdicts.append(
        "Fato matemático que enquadra todos os números acima: para qualquer jogo "
        f"fixo, a média esperada de acertos é exatamente {theoretical:.2f} "
        "(dezenas marcadas × dezenas sorteadas ÷ universo). Nenhum critério de "
        "escolha de jogos altera esse valor — só o acaso separa um mundo do outro "
        "nesta métrica. O que uma estratégia PODE mudar é o rateio do prêmio "
        "(mundo anti-popular) e a conversão de quase-acertos em prêmio "
        "(fechamento)."
    )
