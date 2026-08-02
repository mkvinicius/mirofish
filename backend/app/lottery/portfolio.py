"""
Otimizador de carteira: maximiza diretamente o que o apostador sente.

O retorno esperado de um bilhete e a soma dos retornos dos jogos — linear,
entao diversificar nao muda a media. O que a diversificacao muda e a
DISTRIBUICAO: a probabilidade de o bilhete inteiro atingir pelo menos uma
faixa de premio num concurso qualquer. Foi o que a medicao mostrou: 10 jogos
espalhados fazem >=11 pontos em 72,9% dos concursos; 10 concentrados num
fechamento, em 39,1% — mesmo custo.

Este modulo otimiza esse objetivo de frente, em vez de por heuristica de
sobreposicao: selecao gulosa de jogos maximizando P(bilhete >= faixa alvo),
estimada numa amostra uniforme de sorteios e certificada exatamente sobre o
espaco completo no final. Como a selecao gulosa de uma funcao de cobertura
e submodular, o resultado fica provadamente a >= (1 - 1/e) do otimo.

O desempate e anti-popular: entre jogos com ganho de cobertura equivalente,
prefere o menos apostado — cobertura decide a frequencia de premio, rateio
decide o tamanho dele.
"""

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Sequence, Tuple

import numpy as np

from . import combinatorics as cb
from .catalog import LotteryDef
from .worlds import ParallelWorld, SpaceCache, WorldContext, get_world


@dataclass
class OptimizedPortfolio:
    """Resultado da otimizacao."""

    games: List[Tuple[int, ...]]
    target_hits: int
    p_target_estimated: float          # P(bilhete >= alvo) na amostra
    p_target_baseline: float           # mesma metrica para a selecao heuristica
    sample_size: int
    pool_size: int
    notes: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "jogos": [list(g) for g in self.games],
            "alvo_pontos": self.target_hits,
            "p_alvo_otimizada": round(self.p_target_estimated, 4),
            "p_alvo_heuristica": round(self.p_target_baseline, 4),
            "amostra_sorteios": self.sample_size,
            "pool_candidatos": self.pool_size,
            "observacoes": self.notes,
        }


def _sample_draws(space: SpaceCache, n_sample: int, seed: int) -> SpaceCache:
    """Amostra uniforme de sorteios possiveis (sem reposicao)."""
    if n_sample >= space.size:
        return space
    rng = np.random.default_rng(seed)
    idx = rng.choice(space.size, size=n_sample, replace=False)
    idx.sort()
    return space.take(idx)


def _hits_matrix(
    lottery: LotteryDef,
    pool_games: List[Tuple[int, ...]],
    sample: SpaceCache,
) -> np.ndarray:
    """Matriz (pool, amostra) de acertos de cada candidato em cada sorteio."""
    H = np.empty((len(pool_games), sample.size), dtype=np.uint8)
    for row, game in enumerate(pool_games):
        H[row] = sample.hits(game)
    return H


def optimize_portfolio(
    ctx: WorldContext,
    n_games: int,
    target_hits: Optional[int] = None,
    source_world: str = "hibrido",
    pool_size: int = 1_500,
    sample_size: int = 120_000,
) -> OptimizedPortfolio:
    """Seleciona `n_games` jogos maximizando P(bilhete >= `target_hits`).

    Args:
        target_hits: faixa alvo. Padrao: a segunda faixa mais baixa que paga
            premio — discriminante o bastante para a otimizacao ter o que
            otimizar, frequente o bastante para o efeito ser sentido.
        source_world: mundo que fornece o pool de candidatos (o objetivo de
            cobertura decide QUAIS entram no bilhete; o mundo decide DE ONDE
            vem — regiao menos disputada do espaco, por padrao).
    """
    lottery = ctx.lottery
    tiers = sorted(lottery.prize_tiers)
    if target_hits is None:
        target_hits = tiers[1] if len(tiers) > 1 else tiers[0]
    if target_hits not in lottery.prize_tiers:
        raise ValueError(
            f"alvo de {target_hits} pontos não é faixa premiada de "
            f"{lottery.name} (faixas: {lottery.prize_tiers})"
        )

    notes: List[str] = []
    world: ParallelWorld = get_world(source_world)

    # ------------------------------------------------------------------- pool
    # Metade do pool vem do topo do mundo (qualidade de rateio), metade e
    # amostrada do suporte historico (material de diversificacao). So com o
    # topo, os candidatos sao variantes uns dos outros e o greedy nao tem
    # com que espalhar — foi medido: o otimizador perdia para a heuristica.
    scores = world._final_scores(ctx)
    half = min(pool_size // 2, ctx.space.size)
    top_idx = np.argpartition(-scores, half - 1)[:half]

    rng = np.random.default_rng(ctx.seed + 733)
    supported = np.nonzero(ctx.support_mask(world.support_features))[0]
    extra = rng.choice(supported, size=min(half, len(supported)), replace=False)

    pool_idx = np.unique(np.concatenate([top_idx, extra]))
    order = np.argsort(-scores[pool_idx])
    pool_idx = pool_idx[order]
    pool_games = [ctx.space.numbers_at(int(i)) for i in pool_idx]

    # Score de popularidade dos candidatos, para o desempate.
    try:
        feature_values = {
            name: ctx.space.features[name][pool_idx]
            for name in ctx.popularity.feature_names
        }
        pool_popularity = ctx.popularity.score(feature_values)
    except KeyError:
        pool_popularity = np.zeros(len(pool_games))

    # --------------------------------------------------------------- objetivo
    sample = _sample_draws(ctx.space, sample_size, ctx.seed + 4241)
    H = _hits_matrix(lottery, pool_games, sample)

    best = np.zeros(sample.size, dtype=np.uint8)
    chosen: List[int] = []
    taken = np.zeros(len(pool_games), dtype=bool)

    # Escala do desempate: pequena o bastante para nunca vencer um ganho de
    # cobertura de 1 sorteio da amostra, grande o bastante para ordenar empates.
    pop_norm = pool_popularity - pool_popularity.min()
    spread = pop_norm.max() or 1.0
    tie_break = -(pop_norm / spread) * (0.4 / sample.size)

    for _ in range(n_games):
        reaches = np.maximum(H, best[None, :]) >= target_hits
        gains = reaches.mean(axis=1) + tie_break
        gains[taken] = -np.inf
        pick = int(np.argmax(gains))
        if not np.isfinite(gains[pick]):
            break
        chosen.append(pick)
        taken[pick] = True
        np.maximum(best, H[pick], out=best)

    games = [pool_games[i] for i in chosen]
    p_optimized = float((best >= target_hits).mean())

    # ------------------------------------------------------------- comparacao
    # Mesma metrica para a selecao heuristica (top + diversidade) — e a
    # resposta a pergunta "a otimizacao pagou o proprio custo?".
    baseline_candidates = world.generate(ctx, n_games)
    baseline_best = np.zeros(sample.size, dtype=np.uint8)
    for candidate in baseline_candidates:
        np.maximum(baseline_best, sample.hits(candidate.numbers), out=baseline_best)
    p_baseline = float((baseline_best >= target_hits).mean())

    notes.append(
        f"Objetivo: P(bilhete faz ≥{target_hits} pontos). Otimizado: "
        f"{p_optimized:.2%} vs seleção heurística: {p_baseline:.2%} "
        f"(amostra de {sample.size:,} sorteios; certificação exata no boletim)."
    )
    notes.append(
        f"Pool: melhores jogos do mundo '{world.name}' + amostra do suporte "
        f"histórico; a cobertura decide quais entram, a anti-popularidade "
        f"desempata."
    )

    # Sem maquiagem: se a heuristica venceu na propria metrica do otimizador,
    # e ela que o usuario leva.
    if p_baseline > p_optimized:
        games = [c.numbers for c in baseline_candidates]
        p_optimized, p_baseline = p_baseline, p_optimized
        notes.append(
            "Nesta configuração a seleção heurística superou o greedy na "
            "própria métrica-alvo, então o boletim usa a heurística."
        )

    return OptimizedPortfolio(
        games=games,
        target_hits=target_hits,
        p_target_estimated=p_optimized,
        p_target_baseline=p_baseline,
        sample_size=sample.size,
        pool_size=len(pool_games),
        notes=notes,
    )
