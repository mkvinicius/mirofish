"""
Fechamentos (wheeling) com certificacao exata de garantia.

Um fechamento e a unica tecnica de loteria que entrega garantia matematica:
escolhendo um conjunto S de dezenas e um subconjunto bem escolhido de jogos
dentro de S, e possivel provar afirmacoes do tipo

    "se 13 das minhas 18 dezenas forem sorteadas, pelo menos um dos meus
     jogos fara no minimo 13 pontos"

Isso nao aumenta a chance de acertar as 15 — nada aumenta. O que muda e a
conversao: dado que voce chegou perto, o fechamento assegura o premio
secundario em vez de deixa-lo ao acaso da distribuicao dos seus jogos.

Aqui a garantia nao e prometida, e **verificada**: a certificacao roda o
bilhete contra o espaco completo de sorteios (3.268.760 na Lotofacil) e
devolve o piso real observado. Se o fechamento nao entrega o que se esperava,
o numero sai menor — sem maquiagem.
"""

from dataclasses import dataclass, field
from itertools import combinations
from typing import Any, Dict, List, Optional, Sequence, Tuple

import numpy as np

from . import combinatorics as cb
from .catalog import LotteryDef


@dataclass
class GuaranteeCertificate:
    """Garantia exata de um conjunto de jogos, verificada por enumeracao."""

    base_numbers: List[int]
    n_games: int
    cost: float
    # c (quantas dezenas do conjunto base saem) -> pior resultado possivel
    guarantee_by_c: Dict[int, int]
    # c -> probabilidade de exatamente c dezenas do conjunto base sairem
    probability_by_c: Dict[int, float]
    # faixa premiada -> probabilidade exata de o BILHETE INTEIRO atingi-la
    tier_probability: Dict[int, float] = field(default_factory=dict)
    verified_against: int = 0   # quantos sorteios foram testados
    exhaustive: bool = True

    def to_dict(self) -> Dict[str, Any]:
        return {
            "conjunto_base": self.base_numbers,
            "n_jogos": self.n_games,
            "custo": round(self.cost, 2),
            "garantia_por_acerto_no_conjunto": {
                str(c): int(g) for c, g in sorted(self.guarantee_by_c.items(), reverse=True)
            },
            "probabilidade_por_acerto_no_conjunto": {
                str(c): round(p, 6) for c, p in sorted(self.probability_by_c.items(), reverse=True)
            },
            "probabilidade_por_faixa": {
                str(t): {
                    "probabilidade": prob,
                    "um_em": round(1 / prob) if prob > 0 else 0,
                }
                for t, prob in sorted(self.tier_probability.items(), reverse=True)
            },
            "sorteios_verificados": self.verified_against,
            "verificacao_exaustiva": self.exhaustive,
            "frases": self.statements(),
            "frases_bilhete": self.ticket_statements(),
        }

    def ticket_statements(self) -> List[str]:
        """Desempenho do bilhete inteiro — vale em qualquer modo de carteira.

        Diferente da garantia condicional (que so faz sentido em fechamento),
        isto se aplica a qualquer conjunto de jogos: a probabilidade exata de
        o bilhete atingir cada faixa, contada sorteio a sorteio sobre o espaco
        completo.
        """
        out: List[str] = []
        for tier in sorted(self.tier_probability, reverse=True):
            prob = self.tier_probability[tier]
            if prob <= 0:
                continue
            one_in = f"{round(1 / prob):,}".replace(",", ".")
            out.append(
                f"Faz pelo menos {tier} pontos em 1 a cada {one_in} sorteios "
                f"({prob * 100:.4f}%)."
            )
        return out

    def statements(self) -> List[str]:
        """Traduz o certificado em frases verificaveis."""
        out: List[str] = []
        for c in sorted(self.guarantee_by_c, reverse=True):
            guaranteed = self.guarantee_by_c[c]
            prob = self.probability_by_c.get(c, 0.0)
            if guaranteed <= 0:
                continue
            out.append(
                f"Se {c} das {len(self.base_numbers)} dezenas do conjunto forem "
                f"sorteadas (chance {prob * 100:.2f}%), o bilhete faz no mínimo "
                f"{guaranteed} pontos."
            )
        return out


def certify(
    lottery: LotteryDef,
    games: Sequence[Sequence[int]],
    base_numbers: Sequence[int],
    space_masks: np.ndarray,
    cost: Optional[float] = None,
) -> GuaranteeCertificate:
    """Certifica a garantia de um bilhete contra o espaco COMPLETO de sorteios.

    Para cada sorteio possivel calculamos o melhor resultado que o bilhete
    obteria; agrupando por quantas dezenas do conjunto base sairam, o minimo
    dentro de cada grupo e exatamente a garantia. Nao ha estimativa nem
    amostragem envolvida.
    """
    if not games:
        raise ValueError("Nenhum jogo para certificar")

    dtype = cb.dtype_for(lottery.total_numbers)
    game_masks = cb.masks_from_games(games, dtype=dtype)
    base_mask = dtype(cb.mask_from_numbers(base_numbers))

    # Melhor resultado do bilhete para cada sorteio possivel.
    best = cb.max_hits_against(game_masks, space_masks)
    # Quantas dezenas do conjunto base cada sorteio contem.
    overlap = cb.popcount(space_masks & base_mask)

    guarantee: Dict[int, int] = {}
    probability: Dict[int, float] = {}
    total_space = float(len(space_masks))

    for c in np.unique(overlap):
        selection = overlap == c
        count = int(selection.sum())
        if not count:
            continue
        guarantee[int(c)] = int(best[selection].min())
        probability[int(c)] = count / total_space

    # Probabilidade exata de o bilhete inteiro atingir cada faixa premiada.
    tier_probability = {
        int(tier): float((best >= tier).mean())
        for tier in lottery.prize_tiers
    }

    if cost is None:
        cost = len(games) * lottery.base_price

    return GuaranteeCertificate(
        base_numbers=sorted(int(n) for n in base_numbers),
        n_games=len(games),
        cost=cost,
        guarantee_by_c=guarantee,
        probability_by_c=probability,
        tier_probability=tier_probability,
        verified_against=len(space_masks),
        exhaustive=True,
    )


@dataclass
class Wheel:
    """Um fechamento construido: conjunto base + jogos + certificado."""

    base_numbers: List[int]
    games: List[Tuple[int, ...]]
    certificate: Optional[GuaranteeCertificate] = None
    target_c: int = 0
    target_hits: int = 0
    notes: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "conjunto_base": self.base_numbers,
            "jogos": [list(g) for g in self.games],
            "n_jogos": len(self.games),
            "alvo": {"dezenas_do_conjunto": self.target_c, "pontos": self.target_hits},
            "certificado": self.certificate.to_dict() if self.certificate else None,
            "observacoes": self.notes,
        }


def _subsets_of(base: Sequence[int], size: int, dtype) -> np.ndarray:
    """Todas as mascaras de subconjuntos de `base` com `size` elementos."""
    return np.array(
        [cb.mask_from_numbers(c) for c in combinations(sorted(base), size)],
        dtype=dtype,
    )


def build_wheel(
    lottery: LotteryDef,
    base_numbers: Sequence[int],
    n_games: int,
    target_c: Optional[int] = None,
    target_hits: Optional[int] = None,
    max_candidate_games: int = 60_000,
    seed: int = 0,
) -> Wheel:
    """Constroi um fechamento por cobertura gulosa.

    Estrategia: entre todos os jogos possiveis dentro do conjunto base,
    escolhe repetidamente aquele que cobre o maior numero de cenarios ainda
    descobertos. Um cenario e "qual subconjunto de `target_c` dezenas do
    conjunto base saiu"; ele esta coberto quando algum jogo ja escolhido faz
    pelo menos `target_hits` pontos nele.

    Cobertura gulosa nao garante o menor bilhete possivel (o problema e
    NP-dificil), mas chega perto e, o que importa mais aqui, o resultado e
    certificado exatamente depois — o numero divulgado ao usuario e o piso
    real, nao a intencao do algoritmo.
    """
    base = sorted(int(n) for n in base_numbers)
    picks = lottery.picks

    if len(base) < picks:
        raise ValueError(
            f"Conjunto base precisa de pelo menos {picks} dezenas (recebido {len(base)})"
        )
    if n_games < 1:
        raise ValueError("n_games deve ser >= 1")

    dtype = cb.dtype_for(lottery.total_numbers)
    notes: List[str] = []

    # Alvo padrao: cobrir o cenario "quase la" — uma dezena a menos que o
    # conjunto todo — garantindo a maior pontuacao viavel.
    if target_c is None:
        target_c = min(len(base), picks) - 1
    target_c = max(1, min(target_c, len(base), picks))
    if target_hits is None:
        target_hits = max(1, target_c - 1)
    target_hits = min(target_hits, target_c, picks)

    # Jogos candidatos: todos os subconjuntos de tamanho `picks` do conjunto base.
    from math import comb
    n_candidates = comb(len(base), picks)
    if n_candidates > max_candidate_games:
        raise ValueError(
            f"Conjunto base de {len(base)} dezenas gera {n_candidates:,} jogos "
            f"candidatos (limite {max_candidate_games:,}). Reduza o conjunto."
        )
    candidates = _subsets_of(base, picks, dtype)

    # Cenarios a cobrir.
    scenarios = _subsets_of(base, target_c, dtype)

    # Matriz de cobertura: jogo x cenario -> atinge target_hits?
    uncovered = np.ones(len(scenarios), dtype=bool)
    chosen: List[int] = []
    rng = np.random.default_rng(seed)

    # Limite de elementos por bloco no calculo de ganho, para manter o pico
    # de memoria previsivel mesmo com conjuntos base grandes.
    block_budget = 20_000_000
    already_chosen = np.zeros(len(candidates), dtype=bool)

    for _ in range(n_games):
        if not uncovered.any():
            # Cobertura completa antes de gastar o orcamento: parar aqui e
            # mais barato para o usuario, mas precisa ficar explicito.
            notes.append(
                f"Cobertura total do alvo atingida com {len(chosen)} jogos — "
                f"os {n_games - len(chosen)} jogos restantes do orçamento não "
                f"acrescentariam garantia e foram dispensados."
            )
            break

        pending = scenarios[uncovered]
        chunk = max(1, block_budget // max(len(pending), 1))

        gains = np.zeros(len(candidates), dtype=np.int64)
        for start in range(0, len(candidates), chunk):
            block = candidates[start:start + chunk]
            hits = cb.popcount(block[:, None] & pending[None, :])
            gains[start:start + chunk] = (hits >= target_hits).sum(axis=1)

        gains[already_chosen] = -1
        best_idx = int(np.argmax(gains))
        best_gain = int(gains[best_idx])

        if best_gain <= 0:
            # Nenhum jogo cobre cenario novo: completa com jogos variados
            # para nao devolver um bilhete menor que o pedido.
            remaining = [i for i in range(len(candidates)) if i not in chosen]
            if not remaining:
                break
            extra = rng.choice(remaining, size=min(n_games - len(chosen), len(remaining)), replace=False)
            chosen.extend(int(i) for i in np.atleast_1d(extra))
            notes.append(
                "Cobertura completa atingida antes do orçamento; jogos "
                "adicionais foram sorteados dentro do conjunto base."
            )
            break

        chosen.append(best_idx)
        already_chosen[best_idx] = True
        covered_now = cb.popcount(scenarios & candidates[best_idx]) >= target_hits
        uncovered &= ~covered_now

    games = [cb.numbers_from_mask(int(candidates[i])) for i in chosen[:n_games]]

    if uncovered.any():
        notes.append(
            f"{int(uncovered.sum())} de {len(scenarios)} cenários de {target_c} "
            f"acertos no conjunto não atingem {target_hits} pontos com "
            f"{len(games)} jogos — aumente o orçamento para fechar a garantia."
        )

    return Wheel(
        base_numbers=base,
        games=games,
        target_c=target_c,
        target_hits=target_hits,
        notes=notes,
    )


def wheel_cost(lottery: LotteryDef, n_games: int) -> float:
    """Custo de um bilhete com `n_games` jogos simples."""
    return n_games * lottery.base_price
