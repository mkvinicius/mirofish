"""
Fechamentos (wheeling) com certificacao exata de garantia.

Um fechamento e a unica tecnica de loteria que entrega garantia matematica:
escolhendo um conjunto S de dezenas e um subconjunto bem escolhido de jogos
dentro de S, e possivel provar afirmacoes do tipo

    "se 13 das minhas 18 dezenas forem sorteadas, pelo menos um dos meus
     jogos fara no minimo 13 pontos"

Isso nao aumenta a chance de acertar — nada aumenta. O que muda e a
conversao: dado que voce chegou perto, o fechamento assegura o premio
secundario em vez de deixa-lo ao acaso da distribuicao dos seus jogos.

A garantia nao e prometida, e **verificada**: a certificacao roda o bilhete
contra o espaco completo de sorteios e devolve o piso real observado. Se o
fechamento nao entrega o que se esperava, o numero sai menor — sem maquiagem.

Nota de implementacao: a construcao trabalha em "bits locais" — as dezenas
do conjunto base sao remapeadas para 0..b-1, entao todas as mascaras cabem
em uint64 mesmo quando o universo tem 80 dezenas (Quina). So a conversao
final volta para as dezenas reais.
"""

from dataclasses import dataclass, field
from itertools import combinations
from math import comb
from typing import Any, Dict, List, Optional, Sequence, Tuple, Union

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

    def ticket_statements(self) -> List[str]:
        """Desempenho do bilhete inteiro — vale em qualquer modo de carteira."""
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


# ------------------------------------------------------------------ certificar

def _best_hits_per_draw(
    lottery: LotteryDef,
    games: Sequence[Sequence[int]],
    space,
) -> Tuple[np.ndarray, int]:
    """Melhor resultado do bilhete para cada sorteio possivel do espaco.

    `space` pode ser um SpaceCache (masks ou combos) ou, por retrocompat,
    um ndarray de mascaras.
    """
    if isinstance(space, np.ndarray):
        game_masks = cb.masks_from_games(games, dtype=space.dtype)
        return cb.max_hits_against(game_masks, space), len(space)

    best = np.zeros(space.size, dtype=np.uint8)
    for game in games:
        np.maximum(best, space.hits(game), out=best)
    return best, space.size


def _overlap_with_base(lottery: LotteryDef, base_numbers: Sequence[int], space) -> np.ndarray:
    """Quantas dezenas do conjunto base cada sorteio do espaco contem."""
    if isinstance(space, np.ndarray):
        base_mask = space.dtype.type(cb.mask_from_numbers(base_numbers))
        return cb.popcount(space & base_mask)
    return space.hits(base_numbers)


def certify(
    lottery: LotteryDef,
    games: Sequence[Sequence[int]],
    base_numbers: Sequence[int],
    space,
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

    best, n_draws = _best_hits_per_draw(lottery, games, space)
    overlap = _overlap_with_base(lottery, base_numbers, space)

    guarantee: Dict[int, int] = {}
    probability: Dict[int, float] = {}
    total_space = float(n_draws)

    for c in np.unique(overlap):
        selection = overlap == c
        count = int(selection.sum())
        if not count:
            continue
        guarantee[int(c)] = int(best[selection].min())
        probability[int(c)] = count / total_space

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
        verified_against=n_draws,
        exhaustive=True,
    )


# ------------------------------------------------------------------- construir

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


def _local_subsets(b: int, size: int) -> np.ndarray:
    """Mascaras locais (bits 0..b-1) de todos os subconjuntos de `size`."""
    dtype = np.uint32 if b <= 32 else np.uint64
    out = np.empty(comb(b, size), dtype=dtype)
    for i, combo in enumerate(combinations(range(b), size)):
        m = 0
        for bit in combo:
            m |= 1 << bit
        out[i] = m
    return out


def _coverage(candidate: np.ndarray, scenarios: np.ndarray, target_hits: int) -> np.ndarray:
    """Para um jogo (mascara local), quais cenarios ele cobre."""
    return cb.popcount(scenarios & candidate) >= target_hits


def build_wheel(
    lottery: LotteryDef,
    base_numbers: Sequence[int],
    n_games: int,
    target_c: Optional[int] = None,
    target_hits: Optional[int] = None,
    max_candidate_games: int = 60_000,
    seed: int = 0,
    improve_rounds: int = 2,
) -> Wheel:
    """Constroi um fechamento por cobertura gulosa + melhoria local.

    Estrategia: entre todos os jogos possiveis dentro do conjunto base,
    escolhe repetidamente aquele que cobre o maior numero de cenarios ainda
    descobertos. Um cenario e "qual subconjunto de `target_c` dezenas do
    conjunto base saiu"; ele esta coberto quando algum jogo ja escolhido faz
    pelo menos `target_hits` pontos nele.

    Depois do greedy, uma passada de melhoria local tenta trocar cada jogo
    escolhido por um candidato de fora que aumente a cobertura total — o
    greedy fica preso em otimos locais que uma troca simples destrava.

    O resultado e certificado exatamente depois: o numero divulgado ao
    usuario e o piso real, nao a intencao do algoritmo.
    """
    base = sorted(int(n) for n in base_numbers)
    picks = lottery.picks
    b = len(base)

    if b < picks:
        raise ValueError(
            f"Conjunto base precisa de pelo menos {picks} dezenas (recebido {b})"
        )
    if n_games < 1:
        raise ValueError("n_games deve ser >= 1")

    notes: List[str] = []

    # Alvo padrao: cobrir o cenario "quase la" — uma dezena a menos que o
    # conjunto todo — garantindo a maior pontuacao viavel.
    if target_c is None:
        target_c = min(b, picks) - 1
    target_c = max(1, min(target_c, b, picks))
    if target_hits is None:
        target_hits = max(1, target_c - 1)
    target_hits = min(target_hits, target_c, picks)

    n_candidates = comb(b, picks)
    if n_candidates > max_candidate_games:
        raise ValueError(
            f"Conjunto base de {b} dezenas gera {n_candidates:,} jogos "
            f"candidatos (limite {max_candidate_games:,}). Reduza o conjunto."
        )

    # Tudo em bits locais: candidato/cenario sao subconjuntos do base.
    candidates = _local_subsets(b, picks)
    scenarios = _local_subsets(b, target_c)

    uncovered = np.ones(len(scenarios), dtype=bool)
    chosen: List[int] = []
    already_chosen = np.zeros(len(candidates), dtype=bool)
    rng = np.random.default_rng(seed)

    # Limite de elementos por bloco no calculo de ganho, para manter o pico
    # de memoria previsivel mesmo com conjuntos base grandes.
    block_budget = 20_000_000

    for _ in range(n_games):
        if not uncovered.any():
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
            remaining = np.nonzero(~already_chosen)[0]
            if not len(remaining):
                break
            extra = rng.choice(remaining, size=min(n_games - len(chosen), len(remaining)), replace=False)
            for i in np.atleast_1d(extra):
                chosen.append(int(i))
                already_chosen[int(i)] = True
            notes.append(
                "Cobertura completa atingida antes do orçamento; jogos "
                "adicionais foram sorteados dentro do conjunto base."
            )
            break

        chosen.append(best_idx)
        already_chosen[best_idx] = True
        uncovered &= ~_coverage(candidates[best_idx], scenarios, target_hits)

    # ------------------------------------------------- melhoria local (troca)
    if uncovered.any() and len(chosen) > 1 and improve_rounds > 0:
        cover_matrix = np.stack([
            _coverage(candidates[i], scenarios, target_hits) for i in chosen
        ])
        for _ in range(improve_rounds):
            improved = False
            for slot in range(len(chosen)):
                others = cover_matrix.sum(axis=0) - cover_matrix[slot]
                # Cenarios cobertos apenas por este jogo ou por ninguem:
                open_if_removed = (others == 0)
                current_gain = int((cover_matrix[slot] & open_if_removed).sum())

                pending = scenarios[open_if_removed]
                if not len(pending):
                    continue
                chunk = max(1, block_budget // len(pending))
                gains = np.zeros(len(candidates), dtype=np.int64)
                for start in range(0, len(candidates), chunk):
                    block = candidates[start:start + chunk]
                    hits = cb.popcount(block[:, None] & pending[None, :])
                    gains[start:start + chunk] = (hits >= target_hits).sum(axis=1)
                gains[already_chosen] = -1

                best_idx = int(np.argmax(gains))
                if int(gains[best_idx]) > current_gain:
                    already_chosen[chosen[slot]] = False
                    already_chosen[best_idx] = True
                    chosen[slot] = best_idx
                    cover_matrix[slot] = _coverage(candidates[best_idx], scenarios, target_hits)
                    improved = True
            if not improved:
                break
        covered = cover_matrix.any(axis=0)
        uncovered = ~covered

    # Converte mascaras locais de volta para dezenas reais.
    def _to_numbers(local_mask: int) -> Tuple[int, ...]:
        return tuple(base[bit] for bit in range(b) if (local_mask >> bit) & 1)

    games = [_to_numbers(int(candidates[i])) for i in chosen[:n_games]]

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


def enumeration_space(lottery: LotteryDef):
    """Espaco de sorteios para certificacao, sem o custo das features.

    Certificar so precisa de `hits` sobre o espaco completo — as features do
    SpaceCache (caras de calcular) sao desnecessarias aqui.
    """
    from .worlds import SpaceCache  # import tardio para evitar ciclo

    if lottery.total_numbers <= 64:
        return SpaceCache(
            lottery=lottery,
            features={},
            masks=cb.enumerate_masks(lottery.total_numbers, lottery.picks),
        )
    return SpaceCache(
        lottery=lottery,
        features={},
        combos=cb.enumerate_combos(lottery.total_numbers, lottery.picks),
    )
