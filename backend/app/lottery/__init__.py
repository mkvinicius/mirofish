"""
MiroFish Loterias — motor de mundos paralelos para loterias da Caixa.

Substitui o miolo social (OASIS/Zep) por um motor estatistico-combinatorio:
cada "mundo paralelo" e uma estrategia de aposta com hipotese propria, que
compete com as demais em backtest walk-forward sobre o historico real.

Principio de projeto: o motor NAO preve resultados. Sorteios sao eventos
independentes e uniformes. O que o motor otimiza e:
  - valor esperado (evitando combinacoes populares -> menos rateio);
  - garantia matematica de premios secundarios (fechamentos);
  - transparencia (backtest honesto que mostra quais hipoteses nao se sustentam).
"""

from .catalog import LOTTERIES, LotteryDef, get_lottery

__all__ = ["LOTTERIES", "LotteryDef", "get_lottery"]
