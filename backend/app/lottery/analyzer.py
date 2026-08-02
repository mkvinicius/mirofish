"""
Analise estatistica do historico.

Duas responsabilidades:

1. `FeatureSet` — extrai as mesmas features tanto de sorteios historicos
   quanto de jogos candidatos. Usar o mesmo codigo nos dois lados e o que
   permite dizer "este jogo se parece com os sorteios que costumam sair"
   sem risco de comparar coisas medidas de forma diferente.

2. `HistoryAnalysis` — resume uma janela do historico (frequencia, atraso,
   repeticao, transicoes de Markov, faixas tipicas). E sempre calculada
   sobre uma janela explicita, porque no backtest walk-forward cada mundo
   so pode enxergar os concursos anteriores ao que esta sendo previsto.
"""

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Sequence

import numpy as np

from . import combinatorics as cb
from .catalog import LotteryDef
from .data_source import Draw


@dataclass
class FeatureStats:
    """Resumo de uma feature numerica."""
    name: str
    mean: float
    std: float
    minimum: float
    maximum: float
    p10: float
    p90: float
    mode_range: List[float]  # faixa [p25, p75], usada pelos filtros
    # Suporte empirico: faixa em que sorteios reais de fato caem. Fora dela
    # qualquer modelo ajustado no historico esta extrapolando.
    p01: float = 0.0
    p99: float = 0.0

    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "mean": round(self.mean, 3),
            "std": round(self.std, 3),
            "min": self.minimum,
            "max": self.maximum,
            "p10": self.p10,
            "p90": self.p90,
            "faixa_tipica": self.mode_range,
            "suporte": [self.p01, self.p99],
        }


class FeatureSet:
    """Extrator de features, construido uma vez por modalidade.

    As LUTs sao caras de montar (2^16 entradas por bloco) e baratas de
    aplicar, entao vale reaproveitar a instancia entre chamadas.
    """

    def __init__(self, lottery: LotteryDef):
        self.lottery = lottery
        n = lottery.total_numbers

        odds = [x for x in range(1, n + 1) if x % 2 == 1]
        primes = cb.primes_up_to(n)
        fibs = cb.fibonacci_up_to(n)
        border = cb.border_numbers(n, lottery.grid_rows, lottery.grid_cols)
        low_half = [x for x in range(1, n // 2 + 1)]
        multiples_of_3 = [x for x in range(1, n + 1) if x % 3 == 0]

        self.border_numbers = border
        self._features = {
            "soma": cb.sum_feature(n),
            "impares": cb.count_feature(odds, n),
            "primos": cb.count_feature(primes, n),
            "fibonacci": cb.count_feature(fibs, n),
            "moldura": cb.count_feature(border, n),
            "metade_baixa": cb.count_feature(low_half, n),
            "multiplos_3": cb.count_feature(multiples_of_3, n),
        }

        # Contagem por linha do volante: captura o vies de quem marca o
        # cartao em faixas horizontais.
        for row in range(lottery.grid_rows):
            members = [
                num for num in range(1, n + 1)
                if cb.grid_position(num, lottery.grid_cols)[0] == row
            ]
            if members:
                self._features[f"linha_{row + 1}"] = cb.count_feature(members, n)

    @property
    def names(self) -> List[str]:
        return list(self._features) + ["consecutivos"]

    def evaluate(self, masks: np.ndarray) -> Dict[str, np.ndarray]:
        """Calcula todas as features para um array de mascaras."""
        out = {name: feat.evaluate(masks) for name, feat in self._features.items()}
        out["consecutivos"] = cb.consecutive_pairs(
            masks, self.lottery.total_numbers
        ).astype(np.float64)
        return out

    def evaluate_one(self, numbers: Sequence[int]) -> Dict[str, float]:
        """Versao conveniente para um unico jogo."""
        masks = np.array([cb.mask_from_numbers(numbers)], dtype=cb.dtype_for(self.lottery.total_numbers))
        return {k: float(v[0]) for k, v in self.evaluate(masks).items()}

    def summarize(self, masks: np.ndarray) -> Dict[str, FeatureStats]:
        """Resumo estatistico de cada feature sobre um conjunto de jogos."""
        values = self.evaluate(masks)
        stats: Dict[str, FeatureStats] = {}
        for name, arr in values.items():
            stats[name] = FeatureStats(
                name=name,
                mean=float(arr.mean()),
                std=float(arr.std()),
                minimum=float(arr.min()),
                maximum=float(arr.max()),
                p10=float(np.percentile(arr, 10)),
                p90=float(np.percentile(arr, 90)),
                mode_range=[float(np.percentile(arr, 25)), float(np.percentile(arr, 75))],
                p01=float(np.percentile(arr, 1)),
                p99=float(np.percentile(arr, 99)),
            )
        return stats


@dataclass
class HistoryAnalysis:
    """Retrato estatistico de uma janela do historico."""

    lottery: LotteryDef
    n_draws: int
    first_draw: int
    last_draw: int

    frequency: np.ndarray                    # contagem por dezena
    relative_frequency: np.ndarray           # frequencia / n_draws
    gaps: np.ndarray                         # concursos desde a ultima aparicao
    mean_gap: np.ndarray                     # atraso medio historico
    repeat_counts: np.ndarray                # repeticoes em relacao ao concurso anterior
    markov_given_present: np.ndarray         # P(sai agora | saiu no anterior)
    markov_given_absent: np.ndarray          # P(sai agora | nao saiu no anterior)
    feature_stats: Dict[str, FeatureStats] = field(default_factory=dict)
    last_draw_numbers: Sequence[int] = field(default_factory=tuple)

    # ------------------------------------------------------------------ helpers

    def hot_numbers(self, k: int = 10) -> List[int]:
        """Dezenas mais frequentes na janela."""
        return [int(i + 1) for i in np.argsort(-self.frequency)[:k]]

    def cold_numbers(self, k: int = 10) -> List[int]:
        """Dezenas menos frequentes na janela."""
        return [int(i + 1) for i in np.argsort(self.frequency)[:k]]

    def overdue_numbers(self, k: int = 10) -> List[int]:
        """Dezenas com maior atraso (mais concursos sem sair)."""
        return [int(i + 1) for i in np.argsort(-self.gaps)[:k]]

    @property
    def expected_repeats(self) -> float:
        """Media de dezenas repetidas de um concurso para o seguinte."""
        return float(self.repeat_counts.mean()) if self.repeat_counts.size else 0.0

    def to_dict(self) -> Dict[str, Any]:
        n = self.lottery.total_numbers
        return {
            "modalidade": self.lottery.slug,
            "concursos_analisados": self.n_draws,
            "primeiro_concurso": self.first_draw,
            "ultimo_concurso": self.last_draw,
            "ultimo_sorteio": list(self.last_draw_numbers),
            "frequencia": {
                str(i + 1): int(self.frequency[i]) for i in range(n)
            },
            "frequencia_relativa": {
                str(i + 1): round(float(self.relative_frequency[i]), 4) for i in range(n)
            },
            "atraso_atual": {str(i + 1): int(self.gaps[i]) for i in range(n)},
            "atraso_medio": {str(i + 1): round(float(self.mean_gap[i]), 2) for i in range(n)},
            "mais_frequentes": self.hot_numbers(),
            "menos_frequentes": self.cold_numbers(),
            "mais_atrasadas": self.overdue_numbers(),
            "repeticao": {
                "media": round(self.expected_repeats, 2),
                "distribuicao": {
                    str(int(v)): int(c)
                    for v, c in zip(*np.unique(self.repeat_counts, return_counts=True))
                } if self.repeat_counts.size else {},
            },
            "markov": {
                "dado_que_saiu": {
                    str(i + 1): round(float(self.markov_given_present[i]), 4) for i in range(n)
                },
                "dado_que_nao_saiu": {
                    str(i + 1): round(float(self.markov_given_absent[i]), 4) for i in range(n)
                },
            },
            "features": {k: v.to_dict() for k, v in self.feature_stats.items()},
        }


def build_matrix(draws: Sequence[Draw], lottery: LotteryDef) -> np.ndarray:
    """Matriz booleana (concursos x dezenas) do historico."""
    matrix = np.zeros((len(draws), lottery.total_numbers), dtype=bool)
    for row, draw in enumerate(draws):
        for number in draw.numbers:
            matrix[row, number - 1] = True
    return matrix


def build_masks(draws: Sequence[Draw], lottery: LotteryDef) -> np.ndarray:
    """Array de mascaras dos sorteios historicos."""
    dtype = cb.dtype_for(lottery.total_numbers)
    return np.array([cb.mask_from_numbers(d.numbers) for d in draws], dtype=dtype)


def analyze(
    draws: Sequence[Draw],
    lottery: LotteryDef,
    feature_set: Optional[FeatureSet] = None,
) -> HistoryAnalysis:
    """Calcula o retrato estatistico de uma janela do historico.

    `draws` deve estar ordenado por concurso e conter apenas os concursos
    que o mundo tem direito de enxergar (no backtest, os anteriores ao alvo).
    """
    if not draws:
        raise ValueError("Historico vazio: nada para analisar")

    n = lottery.total_numbers
    matrix = build_matrix(draws, lottery)
    masks = build_masks(draws, lottery)

    frequency = matrix.sum(axis=0).astype(np.float64)
    relative = frequency / len(draws)

    # Atraso atual: quantos concursos desde a ultima aparicao (0 = saiu no ultimo).
    gaps = np.zeros(n, dtype=np.int64)
    for number in range(n):
        appearances = np.nonzero(matrix[:, number])[0]
        gaps[number] = len(draws) - 1 - appearances[-1] if appearances.size else len(draws)

    # Atraso medio historico entre aparicoes consecutivas.
    mean_gap = np.zeros(n, dtype=np.float64)
    for number in range(n):
        appearances = np.nonzero(matrix[:, number])[0]
        if appearances.size > 1:
            mean_gap[number] = float(np.diff(appearances).mean())
        else:
            mean_gap[number] = float(len(draws))

    # Repeticoes em relacao ao concurso imediatamente anterior.
    if len(draws) > 1:
        repeat_counts = (matrix[1:] & matrix[:-1]).sum(axis=1).astype(np.int64)
    else:
        repeat_counts = np.zeros(0, dtype=np.int64)

    # Transicoes de Markov de primeira ordem, por dezena.
    markov_present = np.zeros(n, dtype=np.float64)
    markov_absent = np.zeros(n, dtype=np.float64)
    if len(draws) > 1:
        prev, curr = matrix[:-1], matrix[1:]
        for number in range(n):
            was_present = prev[:, number]
            n_present = int(was_present.sum())
            n_absent = int((~was_present).sum())
            markov_present[number] = (
                float(curr[was_present, number].sum()) / n_present if n_present else relative[number]
            )
            markov_absent[number] = (
                float(curr[~was_present, number].sum()) / n_absent if n_absent else relative[number]
            )
    else:
        markov_present[:] = relative
        markov_absent[:] = relative

    features = feature_set or FeatureSet(lottery)

    return HistoryAnalysis(
        lottery=lottery,
        n_draws=len(draws),
        first_draw=draws[0].number,
        last_draw=draws[-1].number,
        frequency=frequency,
        relative_frequency=relative,
        gaps=gaps,
        mean_gap=mean_gap,
        repeat_counts=repeat_counts,
        markov_given_present=markov_present,
        markov_given_absent=markov_absent,
        feature_stats=features.summarize(masks),
        last_draw_numbers=draws[-1].numbers,
    )
