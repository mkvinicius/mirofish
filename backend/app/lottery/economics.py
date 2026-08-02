"""
Economia da aposta: valor esperado calculado com dados reais.

Este modulo existe para que o numero mais importante do produto — quanto se
espera receber de volta por real apostado — esteja sempre visivel, calculado
a partir do rateio historico publicado pela Caixa e das probabilidades
exatas de cada faixa.

As probabilidades sao hipergeometricas e nao dependem de nenhuma estrategia.
O que varia de aposta para aposta e apenas o rateio esperado, e e por isso
que o modelo de popularidade tem efeito economico real.
"""

from dataclasses import dataclass, field
from math import comb
from typing import Any, Dict, List, Optional, Sequence

import numpy as np

from .catalog import LotteryDef
from .data_source import Draw


def hit_probability(lottery: LotteryDef, hits: int, bet_size: Optional[int] = None) -> float:
    """Probabilidade exata de fazer exatamente `hits` pontos.

    Distribuicao hipergeometrica: das `bet_size` dezenas marcadas, quantas
    coincidem com as `picks` sorteadas.
    """
    bet_size = bet_size or lottery.min_bet_size
    n, k, d = lottery.total_numbers, lottery.picks, bet_size
    if hits > min(k, d) or hits < max(0, k + d - n):
        return 0.0
    return comb(d, hits) * comb(n - d, k - hits) / comb(n, k)


@dataclass
class ExpectedValue:
    """Valor esperado de uma aposta simples."""

    lottery: LotteryDef
    bet_price: float
    per_tier: Dict[int, Dict[str, float]] = field(default_factory=dict)
    expected_return: float = 0.0
    n_draws_used: int = 0
    notes: List[str] = field(default_factory=list)

    @property
    def payout_ratio(self) -> float:
        """Fracao do valor apostado que volta, em media."""
        return self.expected_return / self.bet_price if self.bet_price else 0.0

    def to_dict(self) -> Dict[str, Any]:
        return {
            "preco_aposta": round(self.bet_price, 2),
            "retorno_esperado": round(self.expected_return, 4),
            "taxa_de_retorno": round(self.payout_ratio, 4),
            "perda_esperada_por_aposta": round(self.bet_price - self.expected_return, 4),
            "concursos_usados": self.n_draws_used,
            "por_faixa": {
                str(hits): {
                    "probabilidade": info["probabilidade"],
                    "um_em": info["um_em"],
                    "premio_medio": round(info["premio_medio"], 2),
                    "contribuicao": round(info["contribuicao"], 4),
                }
                for hits, info in sorted(self.per_tier.items(), reverse=True)
            },
            "observacoes": self.notes,
        }


def expected_value(
    lottery: LotteryDef,
    draws: Sequence[Draw],
    bet_size: Optional[int] = None,
    recent: int = 200,
) -> ExpectedValue:
    """Calcula o valor esperado usando o rateio medio recente.

    Usa os `recent` concursos mais recentes porque o valor dos premios muda
    com o tempo (arrecadacao, acumulados, concursos especiais). Faixas
    principais sao dominadas por acumulacoes raras, entao a media e volatil —
    isso fica registrado nas observacoes.
    """
    bet_size = bet_size or lottery.min_bet_size
    price = lottery.bet_price(bet_size)
    window = list(draws)[-recent:] if recent else list(draws)

    per_tier: Dict[int, Dict[str, float]] = {}
    total = 0.0

    for hits in lottery.prize_tiers:
        probability = hit_probability(lottery, hits, bet_size)
        values = [d.prizes[hits].value for d in window if hits in d.prizes and d.prizes[hits].value > 0]
        mean_prize = float(np.mean(values)) if values else 0.0
        contribution = probability * mean_prize
        total += contribution
        per_tier[hits] = {
            "probabilidade": probability,
            "um_em": round(1 / probability) if probability > 0 else 0,
            "premio_medio": mean_prize,
            "contribuicao": contribution,
        }

    notes = [
        f"prêmios médios dos últimos {len(window)} concursos",
        "a faixa principal é dominada por acumulações raras, então sua "
        "contribuição é a mais instável da conta",
    ]

    result = ExpectedValue(
        lottery=lottery,
        bet_price=price,
        per_tier=per_tier,
        expected_return=total,
        n_draws_used=len(window),
        notes=notes,
    )

    result.notes.append(
        f"conclusão: cada R$ {price:.2f} apostados devolvem em média "
        f"R$ {total:.2f} ({result.payout_ratio:.1%}). Nenhuma estratégia deste "
        f"sistema muda esse número — ele é a margem estrutural da loteria."
    )
    return result


def sharing_adjusted_value(
    base_value: ExpectedValue,
    sharing_multiplier: float,
) -> Dict[str, Any]:
    """Ajusta o valor esperado pelo rateio estimado de um jogo especifico.

    Um multiplicador de 0,5 significa "estima-se metade dos apostadores nesta
    região do espaço", o que dobra o prêmio das faixas rateadas. O ajuste so
    e aplicado as faixas altas, que sao as efetivamente rateadas — nas faixas
    baixas o premio e um valor fixo por acertador, nao um rateio.
    """
    if sharing_multiplier <= 0:
        sharing_multiplier = 1.0

    lottery = base_value.lottery
    # As duas faixas mais altas sao as rateadas de fato.
    shared_tiers = set(lottery.prize_tiers[:2])

    adjusted = 0.0
    for hits, info in base_value.per_tier.items():
        contribution = info["contribuicao"]
        if hits in shared_tiers:
            contribution = contribution / sharing_multiplier
        adjusted += contribution

    return {
        "multiplicador_rateio": round(sharing_multiplier, 4),
        "retorno_esperado_ajustado": round(adjusted, 4),
        "taxa_de_retorno_ajustada": round(
            adjusted / base_value.bet_price if base_value.bet_price else 0.0, 4
        ),
        "ganho_vs_jogo_medio": round(adjusted - base_value.expected_return, 4),
        "faixas_rateadas": sorted(shared_tiers, reverse=True),
    }
