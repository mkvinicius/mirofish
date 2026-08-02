"""
Nucleo combinatorio: representacao, enumeracao e pontuacao vetorizada.

Um jogo e representado como uma mascara de bits (bit i-1 ligado = dezena i
marcada). Isso permite duas operacoes que sao a base de todo o motor:

    acertos = popcount(jogo & sorteio)
    filtro  = feature_lut[jogo]  (qualquer feature aditiva sobre as dezenas)

Com 25 dezenas o espaco inteiro da Lotofacil (3.268.760 jogos) cabe em
~13 MB como uint32, entao "escolher os melhores jogos" deixa de ser
heuristica e vira busca exata sobre o espaco completo.
"""

from typing import Iterable, List, Sequence, Tuple

import numpy as np

# Tamanho do bloco usado nas tabelas de lookup (2^16 entradas por bloco).
_LUT_BITS = 16
_LUT_SIZE = 1 << _LUT_BITS
_LUT_MASK = _LUT_SIZE - 1


# --------------------------------------------------------------------- popcount

def _popcount_fallback(arr: np.ndarray) -> np.ndarray:
    """Popcount vetorizado para numpy < 2.0 (sem `bitwise_count`)."""
    lut = _POPCOUNT_LUT
    result = np.zeros(arr.shape, dtype=np.uint8)
    work = arr.copy()
    # Processa 16 bits por vez.
    while True:
        result += lut[(work & _LUT_MASK).astype(np.uint32)]
        work = work >> _LUT_BITS
        if not work.any():
            break
    return result


_POPCOUNT_LUT = np.array(
    [bin(i).count("1") for i in range(_LUT_SIZE)], dtype=np.uint8
)

_HAS_BITWISE_COUNT = hasattr(np, "bitwise_count")


def popcount(arr: np.ndarray) -> np.ndarray:
    """Conta bits ligados, elemento a elemento."""
    if _HAS_BITWISE_COUNT:
        return np.bitwise_count(arr).astype(np.uint8)
    return _popcount_fallback(arr)


# ------------------------------------------------------------------- conversoes

def mask_from_numbers(numbers: Iterable[int]) -> int:
    """Converte dezenas (1-indexadas) em mascara de bits."""
    mask = 0
    for n in numbers:
        mask |= 1 << (int(n) - 1)
    return mask


def numbers_from_mask(mask: int) -> Tuple[int, ...]:
    """Converte mascara de bits de volta em dezenas ordenadas."""
    mask = int(mask)
    out: List[int] = []
    position = 1
    while mask:
        if mask & 1:
            out.append(position)
        mask >>= 1
        position += 1
    return tuple(out)


def masks_from_games(games: Sequence[Sequence[int]], dtype=np.uint32) -> np.ndarray:
    """Converte uma lista de jogos em array de mascaras."""
    return np.array([mask_from_numbers(g) for g in games], dtype=dtype)


def dtype_for(total_numbers: int):
    """Menor inteiro sem sinal que comporta `total_numbers` bits.

    Universos com mais de 64 dezenas (Quina: 80) nao cabem em mascara —
    para eles o motor usa a representacao por indices (`enumerate_combos`).
    """
    if total_numbers <= 32:
        return np.uint32
    if total_numbers <= 64:
        return np.uint64
    raise ValueError(
        f"{total_numbers} dezenas nao cabem em mascara de 64 bits — "
        f"use a representacao por indices (enumerate_combos)"
    )


# ------------------------------------------------------------------ enumeracao

def enumerate_masks(total_numbers: int, picks: int, chunk: int = 1 << 22) -> np.ndarray:
    """Enumera TODAS as combinacoes de `picks` dezenas entre `total_numbers`.

    Para universos pequenos (ate 26 dezenas, caso da Lotofacil) a varredura
    de 2^n com filtro por popcount e mais rapida e mais simples que gerar
    combinacoes em Python. Acima disso caimos para geracao incremental.
    """
    dtype = dtype_for(total_numbers)

    if total_numbers <= 26:
        parts: List[np.ndarray] = []
        total_space = 1 << total_numbers
        for start in range(0, total_space, chunk):
            stop = min(start + chunk, total_space)
            block = np.arange(start, stop, dtype=dtype)
            parts.append(block[popcount(block) == picks])
        return np.concatenate(parts) if parts else np.empty(0, dtype=dtype)

    return _enumerate_masks_incremental(total_numbers, picks, dtype)


def _enumerate_masks_incremental(total_numbers: int, picks: int, dtype) -> np.ndarray:
    """Enumeracao para universos grandes (Quina, Mega-Sena).

    Constroi as combinacoes por camadas: mantendo em cada passo as mascaras
    parciais junto do indice da maior dezena usada, estendemos apenas com
    dezenas maiores. Isso gera cada combinacao exatamente uma vez.
    """
    masks = np.array([1 << i for i in range(total_numbers - picks + 1)], dtype=dtype)
    highest = np.arange(total_numbers - picks + 1, dtype=np.int16)

    for level in range(1, picks):
        # Cada mascara parcial pode ser estendida com dezenas de (highest+1)
        # ate o limite que ainda permite completar `picks` dezenas.
        limit = total_numbers - (picks - level)
        counts = (limit - highest).astype(np.int64)
        counts[counts < 0] = 0

        repeated_masks = np.repeat(masks, counts)
        repeated_high = np.repeat(highest, counts)
        offsets = np.arange(counts.sum(), dtype=np.int64) - np.repeat(
            np.concatenate(([0], np.cumsum(counts)[:-1])), counts
        )
        new_high = (repeated_high + 1 + offsets).astype(np.int16)
        masks = repeated_masks | (np.ones(1, dtype=dtype)[0] << new_high.astype(dtype))
        highest = new_high

    return masks


# ------------------------------------------------- representacao por indices
#
# Para universos com mais de 64 dezenas (Quina: 80), um jogo e uma linha
# (picks,) de dezenas uint8 em ordem crescente. As mesmas tres operacoes do
# caminho por mascara existem aqui, via gather em vez de bit a bit:
#
#     acertos  = membro[jogo].sum(axis=1)          (membro = bool por dezena)
#     feature  = pesos[jogo].sum(axis=1)
#     consecut = (diff(jogo) == 1).sum(axis=1)


def enumerate_combos(total_numbers: int, picks: int) -> np.ndarray:
    """Enumera todas as combinacoes como matriz (N, picks) de dezenas uint8.

    Mesma construcao por camadas da versao com mascara: cada parcial e
    estendida apenas com dezenas maiores que a sua ultima, gerando cada
    combinacao exatamente uma vez, em ordem lexicografica.
    """
    if total_numbers > 255:
        raise ValueError("uint8 comporta ate 255 dezenas")

    combos = np.arange(1, total_numbers - picks + 2, dtype=np.uint8).reshape(-1, 1)

    for level in range(1, picks):
        highest = combos[:, -1].astype(np.int64)
        limit = total_numbers - (picks - level - 1)
        counts = limit - highest
        counts[counts < 0] = 0

        repeated = np.repeat(combos, counts, axis=0)
        offsets = np.arange(int(counts.sum()), dtype=np.int64) - np.repeat(
            np.concatenate(([0], np.cumsum(counts)[:-1])), counts
        )
        new_col = (np.repeat(highest, counts) + 1 + offsets).astype(np.uint8)
        combos = np.column_stack([repeated, new_col])

    return combos


def combos_hits(combos: np.ndarray, numbers: Iterable[int], total_numbers: int) -> np.ndarray:
    """Acertos de cada linha contra um conjunto de dezenas."""
    member = np.zeros(total_numbers + 1, dtype=np.uint8)
    for n in numbers:
        member[int(n)] = 1
    return member[combos].sum(axis=1, dtype=np.uint8)


def combos_eval(combos: np.ndarray, weights: Sequence[float]) -> np.ndarray:
    """Soma de pesos por dezena para cada linha (feature aditiva)."""
    table = np.zeros(len(weights) + 1, dtype=np.float64)
    table[1:] = np.asarray(weights, dtype=np.float64)
    return table[combos].sum(axis=1)


def combos_consecutive_pairs(combos: np.ndarray) -> np.ndarray:
    """Pares de dezenas consecutivas em cada linha (linhas ja ordenadas)."""
    return (np.diff(combos.astype(np.int16), axis=1) == 1).sum(axis=1).astype(np.uint8)


# -------------------------------------------------------- features aditivas

class AdditiveFeature:
    """Feature que e uma soma de pesos sobre as dezenas marcadas.

    Cobre a maioria das features uteis: soma das dezenas, quantidade de
    impares, de primos, de dezenas na moldura do volante, por linha/coluna,
    quantidade de dezenas repetidas do concurso anterior, etc.

    A avaliacao usa tabelas de lookup de 16 bits, entao custa apenas alguns
    `gather` por bloco -- avaliar 3,27 milhoes de jogos leva milissegundos.
    """

    def __init__(self, weights: Sequence[float], total_numbers: int):
        if len(weights) != total_numbers:
            raise ValueError(
                f"Esperados {total_numbers} pesos (um por dezena), recebidos {len(weights)}"
            )
        self.total_numbers = total_numbers
        self.weights = np.asarray(weights, dtype=np.float64)
        self._luts = self._build_luts()

    def _build_luts(self) -> List[np.ndarray]:
        """Constroi uma LUT por bloco de 16 bits do universo."""
        n_blocks = (self.total_numbers + _LUT_BITS - 1) // _LUT_BITS
        luts: List[np.ndarray] = []

        for block in range(n_blocks):
            base = block * _LUT_BITS
            bits_here = min(_LUT_BITS, self.total_numbers - base)
            size = 1 << bits_here
            lut = np.zeros(size, dtype=np.float64)
            # Preenchimento por dobramento: lut[x | 1<<b] = lut[x] + peso[b]
            for bit in range(bits_here):
                weight = self.weights[base + bit]
                half = 1 << bit
                lut[half:half * 2] = lut[:half] + weight
            luts.append(lut)

        return luts

    def evaluate(self, masks: np.ndarray) -> np.ndarray:
        """Avalia a feature para um array de mascaras."""
        total = np.zeros(masks.shape, dtype=np.float64)
        for block, lut in enumerate(self._luts):
            shifted = (masks >> np.array(block * _LUT_BITS, dtype=masks.dtype))
            idx = (shifted & np.array(len(lut) - 1, dtype=masks.dtype)).astype(np.int64)
            total += lut[idx]
        return total


def count_feature(members: Iterable[int], total_numbers: int) -> AdditiveFeature:
    """Feature que conta quantas dezenas do jogo pertencem a `members`."""
    weights = np.zeros(total_numbers, dtype=np.float64)
    for n in members:
        if 1 <= int(n) <= total_numbers:
            weights[int(n) - 1] = 1.0
    return AdditiveFeature(weights, total_numbers)


def sum_feature(total_numbers: int) -> AdditiveFeature:
    """Feature de soma das dezenas do jogo."""
    return AdditiveFeature(np.arange(1, total_numbers + 1, dtype=np.float64), total_numbers)


# ------------------------------------------------------------ features de padrao

def consecutive_pairs(masks: np.ndarray, total_numbers: int) -> np.ndarray:
    """Quantidade de pares de dezenas consecutivas (ex.: 07 e 08).

    Truque de bits: dezenas consecutivas sao bits adjacentes, entao
    `popcount(m & (m >> 1))` conta os pares diretamente. O `& universe`
    evita contar o "vira-volta" entre o topo do universo e o lixo acima dele.
    """
    universe = masks.dtype.type((1 << total_numbers) - 1)
    return popcount((masks & (masks >> masks.dtype.type(1))) & universe)


def hits_against(masks: np.ndarray, draw_mask: int) -> np.ndarray:
    """Acertos de cada jogo contra um sorteio."""
    return popcount(masks & masks.dtype.type(draw_mask))


def max_hits_against(game_masks: np.ndarray, draw_masks: np.ndarray) -> np.ndarray:
    """Para cada sorteio, o melhor resultado obtido pelo conjunto de jogos.

    E a operacao que certifica garantias de fechamento: rodando contra o
    espaco completo de sorteios, o minimo desse vetor (dentro de um recorte)
    e a garantia exata do bilhete.
    """
    best = np.zeros(draw_masks.shape, dtype=np.uint8)
    for game in game_masks:
        np.maximum(best, popcount(draw_masks & draw_masks.dtype.type(game)), out=best)
    return best


# ------------------------------------------------------------------- utilidades

def grid_position(number: int, cols: int) -> Tuple[int, int]:
    """Linha e coluna de uma dezena no volante (0-indexadas)."""
    idx = number - 1
    return idx // cols, idx % cols


def border_numbers(total_numbers: int, rows: int, cols: int) -> List[int]:
    """Dezenas na moldura do volante (borda do retangulo).

    Na Lotofacil a moldura tem 16 dezenas e o miolo 9 -- uma das divisoes
    mais usadas por apostadores, e portanto relevante para popularidade.
    """
    out: List[int] = []
    for number in range(1, total_numbers + 1):
        row, col = grid_position(number, cols)
        if row in (0, rows - 1) or col in (0, cols - 1):
            out.append(number)
    return out


def primes_up_to(limit: int) -> List[int]:
    """Primos ate `limit` (crivo simples)."""
    if limit < 2:
        return []
    sieve = np.ones(limit + 1, dtype=bool)
    sieve[:2] = False
    for p in range(2, int(limit ** 0.5) + 1):
        if sieve[p]:
            sieve[p * p::p] = False
    return [int(i) for i in np.nonzero(sieve)[0]]


def fibonacci_up_to(limit: int) -> List[int]:
    """Numeros de Fibonacci ate `limit`."""
    out: List[int] = []
    a, b = 1, 2
    while a <= limit:
        out.append(a)
        a, b = b, a + b
    return out
