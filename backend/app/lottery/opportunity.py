"""
Cacador de oportunidades: QUANDO apostar, nao O QUE apostar.

Os unicos casos documentados de lucro real em loteria (Cash WinFall/MIT,
grupo Selbee, Mandel) nao previram numero nenhum — exploraram concursos em
que a REGRA deixava o valor esperado ficar favoravel: acumulacoes grandes,
roll-downs, premio garantido. Este modulo produtiza essa vigilancia para as
loterias da Caixa.

A conta central e uma identidade exata de premio rateado. Se um bolo P e
dividido entre os acertadores de uma faixa com probabilidade p e N jogos
concorrentes, o retorno esperado de UM jogo naquela faixa e:

    EV_faixa = P * (1 - (1-p)^N) / N

(deducao: o bolo inteiro e pago se e so se alguem acerta, o que ocorre com
probabilidade 1-(1-p)^N; por simetria entre os N jogos, cada um espera 1/N
do total pago). Nenhuma suposicao sobre quais numeros — vale para qualquer
jogo, e por isso a resposta aqui e sobre o CONCURSO, nao sobre dezenas.

Os ingredientes (bolo estimado do proximo concurso, arrecadacao tipica) sao
publicados pela Caixa; o modulo le, calcula e compara as modalidades.
"""

from dataclasses import dataclass, field
from math import comb
from typing import Any, Dict, List, Optional, Sequence

import numpy as np

from ..utils.logger import get_logger
from .catalog import LOTTERIES, LotteryDef
from .data_source import CaixaClient, Draw, HistoryStore
from .economics import hit_probability

logger = get_logger('mirofish.lottery.opportunity')


@dataclass
class NextDrawOutlook:
    """Avaliacao economica do proximo concurso de uma modalidade."""

    lottery_slug: str
    lottery_name: str
    next_number: int
    next_date: str
    accumulated: bool                      # o concurso anterior acumulou?
    special: bool                          # concurso especial (nao acumula: roll-down)
    main_pool: float                       # bolo estimado/acumulado da faixa principal
    est_bets: float                        # jogos simples esperados no concurso
    ev_main: float                         # retorno esperado da faixa principal, por jogo
    ev_secondary: float                    # demais faixas (media historica)
    bet_price: float
    payout_ratio: float                    # (ev_main + ev_secondary) / preco
    breakeven_pool: float                  # bolo que tornaria o retorno = 100%
    full_buy_cost: float                   # custo de comprar todas as combinacoes
    notes: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "modalidade": self.lottery_slug,
            "nome": self.lottery_name,
            "proximo_concurso": self.next_number,
            "data": self.next_date,
            "acumulado": self.accumulated,
            "concurso_especial": self.special,
            "bolo_principal": round(self.main_pool, 2),
            "apostas_estimadas": round(self.est_bets),
            "preco_aposta": self.bet_price,
            "retorno_esperado": {
                "faixa_principal": round(self.ev_main, 4),
                "faixas_secundarias": round(self.ev_secondary, 4),
                "total": round(self.ev_main + self.ev_secondary, 4),
                "taxa": round(self.payout_ratio, 4),
            },
            "bolo_para_ev_100": round(self.breakeven_pool, 2),
            "compra_total": {
                "custo": round(self.full_buy_cost, 2),
                "cobre": bool(self.main_pool > self.full_buy_cost),
            },
            "observacoes": self.notes,
        }


def _brl(value: float) -> str:
    """Formata reais no padrao brasileiro (1.234.567,89)."""
    return f"{value:,.2f}".replace(",", "\x00").replace(".", ",").replace("\x00", ".")


def _recent_average(values: Sequence[float]) -> float:
    return float(np.mean(values)) if values else 0.0


def _estimate_bets(draws: Sequence[Draw], lottery: LotteryDef, window: int = 30) -> float:
    """Jogos simples esperados num concurso, pela arrecadacao recente.

    `arrecadacao / preco_base` e exatamente o numero de jogos simples
    equivalentes, independentemente da mistura de tamanhos de aposta
    (uma aposta de N dezenas custa e cobre C(N, picks) jogos).
    """
    revenues = [d.revenue for d in draws[-window:] if d.revenue > 0]
    if not revenues:
        return 0.0
    return _recent_average(revenues) / lottery.base_price


def _shared_ev(pool: float, p: float, n_bets: float) -> float:
    """Identidade do premio rateado: EV por jogo = P*(1-(1-p)^N)/N."""
    if pool <= 0 or p <= 0 or n_bets <= 0:
        return 0.0
    p_someone = 1.0 - (1.0 - p) ** n_bets
    return pool * p_someone / n_bets


def _secondary_ev(draws: Sequence[Draw], lottery: LotteryDef, recent: int = 100) -> float:
    """Retorno esperado das faixas nao-principais (media historica)."""
    total = 0.0
    window = list(draws)[-recent:]
    for hits in lottery.prize_tiers[1:]:
        p = hit_probability(lottery, hits)
        values = [d.prizes[hits].value for d in window
                  if hits in d.prizes and d.prizes[hits].value > 0]
        total += p * _recent_average(values)
    return total


def outlook_for(
    lottery: LotteryDef,
    payload: Optional[Dict[str, Any]] = None,
    draws: Optional[Sequence[Draw]] = None,
) -> NextDrawOutlook:
    """Avalia o proximo concurso de uma modalidade.

    Args:
        payload: resposta crua da API da Caixa para o concurso mais recente
                 (buscada se nao fornecida)
        draws: historico em cache (carregado se nao fornecido)
    """
    if payload is None:
        payload = CaixaClient().fetch(lottery.api_slug)
    if draws is None:
        draws = HistoryStore(lottery).load()

    p_main = hit_probability(lottery, lottery.prize_tiers[0])
    n_bets = _estimate_bets(draws, lottery)

    # Bolo da faixa principal do proximo concurso: o valor estimado anunciado
    # ja inclui acumulado + previsao de arrecadacao da faixa.
    estimated = float(payload.get("valorEstimadoProximoConcurso") or 0.0)
    accumulated_next = float(payload.get("valorAcumuladoProximoConcurso") or 0.0)
    main_pool = max(estimated, accumulated_next)

    special = bool(payload.get("indicadorConcursoEspecial", 0) not in (0, 1, None))
    # Alguns payloads usam 1 como "normal"; o sinal robusto de especial e o
    # acumulado para concurso especial ser relevante no proximo sorteio.
    special_pool = float(payload.get("valorAcumuladoConcursoEspecial") or 0.0)

    ev_main = _shared_ev(main_pool, p_main, n_bets)
    ev_secondary = _secondary_ev(draws, lottery)
    payout = (ev_main + ev_secondary) / lottery.base_price if lottery.base_price else 0.0

    # Bolo necessario para retorno total = 100% do preco, mantendo N fixo:
    # resolve P em  (P * (1-(1-p)^N)/N + sec) = preco.
    p_someone = 1.0 - (1.0 - p_main) ** n_bets if n_bets > 0 else 0.0
    if p_someone > 0:
        breakeven = (lottery.base_price - ev_secondary) * n_bets / p_someone
    else:
        breakeven = float("inf")

    full_buy = lottery.total_combinations * lottery.base_price

    notes: List[str] = []
    if accumulated_next > 0:
        notes.append(
            f"Acumulou: R$ {_brl(accumulated_next)} já garantidos no bolo da "
            f"faixa principal."
        )
    if special_pool > 0:
        notes.append(
            f"Reserva para concurso especial: R$ {_brl(special_pool)} — nos "
            f"especiais da Caixa o prêmio principal não acumula (desce para a "
            f"faixa seguinte), o mecanismo explorado no caso Cash WinFall."
        )
    if payout >= 1.0:
        notes.append(
            "RETORNO ESPERADO ACIMA DE 100%: janela rara. A variância continua "
            "enorme — EV positivo não é ganho garantido."
        )
    elif main_pool > 0 and breakeven < float("inf"):
        falta = breakeven - main_pool
        if falta > 0:
            notes.append(
                f"Para o retorno chegar a 100%, o bolo precisaria de mais "
                f"R$ {_brl(falta)}."
            )
    if main_pool > full_buy:
        notes.append(
            f"O bolo supera o custo de comprar TODAS as "
            f"{lottery.total_combinations:,} combinações (R$ {_brl(full_buy)}) — "
            f"na prática inviável pelo volume operacional e pelo risco de "
            f"rateio, mas o limiar matemático foi cruzado.".replace(",", ".")
        )

    return NextDrawOutlook(
        lottery_slug=lottery.slug,
        lottery_name=lottery.name,
        next_number=int(payload.get("numeroConcursoProximo") or 0),
        next_date=str(payload.get("dataProximoConcurso") or ""),
        accumulated=bool(payload.get("acumulado", False)),
        special=special or special_pool > 0,
        main_pool=main_pool,
        est_bets=n_bets,
        ev_main=ev_main,
        ev_secondary=ev_secondary,
        bet_price=lottery.base_price,
        payout_ratio=payout,
        breakeven_pool=breakeven,
        full_buy_cost=full_buy,
        notes=notes,
    )


def scan_opportunities(slugs: Optional[Sequence[str]] = None) -> Dict[str, Any]:
    """Avalia o proximo concurso de todas as modalidades e ordena por retorno."""
    outlooks: List[NextDrawOutlook] = []
    errors: Dict[str, str] = {}

    for slug in (slugs or list(LOTTERIES)):
        lottery = LOTTERIES[slug]
        try:
            outlooks.append(outlook_for(lottery))
        except Exception as exc:  # rede fora nao deve derrubar o painel
            logger.warning("Outlook de %s falhou: %s", slug, exc)
            errors[slug] = str(exc)

    outlooks.sort(key=lambda o: o.payout_ratio, reverse=True)

    verdict: List[str] = []
    if outlooks:
        top = outlooks[0]
        verdict.append(
            f"Melhor retorno esperado no momento: {top.lottery_name} "
            f"({top.payout_ratio:.1%} por real apostado)."
        )
        verdict.append(
            "Nenhuma das opções tem retorno positivo num concurso comum — a "
            "leitura certa deste painel é ESPERAR os concursos em que o bolo "
            "eleva o retorno, não apostar sempre no líder da tabela."
        )

    return {
        "modalidades": [o.to_dict() for o in outlooks],
        "erros": errors,
        "leitura": verdict,
    }
