"""
Catalogo de loterias suportadas.

Cada modalidade e descrita de forma declarativa para que o restante do motor
(analise, mundos paralelos, fechamento, backtest) funcione sem `if` por
modalidade. A Lotofacil e a modalidade de referencia; Quina e Mega-Sena ja
ficam declaradas porque o espaco amostral delas tambem e enumeravel.
"""

from dataclasses import dataclass, field
from math import comb
from typing import Dict, List, Optional


@dataclass(frozen=True)
class LotteryDef:
    """Definicao declarativa de uma modalidade."""

    slug: str                      # identificador interno (usado nas rotas)
    name: str                      # nome exibido
    api_slug: str                  # caminho na API da Caixa
    total_numbers: int             # tamanho do universo (ex.: 25 na Lotofacil)
    picks: int                     # dezenas sorteadas por concurso
    min_bet_size: int              # menor aposta permitida
    max_bet_size: int              # maior aposta permitida
    prize_tiers: List[int]         # acertos que pagam premio, do maior para o menor
    base_price: float              # preco da aposta minima (R$)
    grid_rows: int                 # layout do volante (usado nas features de padrao)
    grid_cols: int

    # Faixa da API (`faixa`) correspondente a cada quantidade de acertos.
    # Na Lotofacil faixa 1 = 15 acertos, faixa 2 = 14, etc.
    tier_to_faixa: Dict[int, int] = field(default_factory=dict)

    @property
    def universe(self) -> List[int]:
        return list(range(1, self.total_numbers + 1))

    @property
    def total_combinations(self) -> int:
        """Tamanho do espaco amostral completo."""
        return comb(self.total_numbers, self.picks)

    @property
    def enumerable(self) -> bool:
        """Se o espaco cabe em memoria para busca exaustiva.

        O limite de 60 milhoes vem da representacao usada em `combinatorics`
        (1 uint64 por combinacao + tabelas auxiliares); acima disso o motor
        cai para amostragem.
        """
        return self.total_combinations <= 60_000_000

    def bet_price(self, bet_size: int) -> float:
        """Preco de uma aposta com `bet_size` dezenas.

        O preco cresce com o numero de jogos simples equivalentes, que e a
        regra da Caixa: uma aposta de N dezenas equivale a C(N, picks) jogos.
        """
        if bet_size < self.min_bet_size or bet_size > self.max_bet_size:
            raise ValueError(
                f"{self.name}: aposta deve ter entre {self.min_bet_size} e "
                f"{self.max_bet_size} dezenas (recebido {bet_size})"
            )
        return comb(bet_size, self.min_bet_size) * self.base_price

    def games_in_bet(self, bet_size: int) -> int:
        """Quantos jogos simples uma aposta de `bet_size` dezenas cobre."""
        return comb(bet_size, self.min_bet_size)

    def faixa_for_hits(self, hits: int) -> Optional[int]:
        """Faixa da API correspondente a uma quantidade de acertos."""
        return self.tier_to_faixa.get(hits)


LOTOFACIL = LotteryDef(
    slug="lotofacil",
    name="Lotofácil",
    api_slug="lotofacil",
    total_numbers=25,
    picks=15,
    min_bet_size=15,
    max_bet_size=20,
    prize_tiers=[15, 14, 13, 12, 11],
    base_price=3.50,
    grid_rows=5,
    grid_cols=5,
    tier_to_faixa={15: 1, 14: 2, 13: 3, 12: 4, 11: 5},
)

QUINA = LotteryDef(
    slug="quina",
    name="Quina",
    api_slug="quina",
    total_numbers=80,
    picks=5,
    min_bet_size=5,
    max_bet_size=15,
    prize_tiers=[5, 4, 3, 2],
    base_price=3.00,
    grid_rows=8,
    grid_cols=10,
    tier_to_faixa={5: 1, 4: 2, 3: 3, 2: 4},
)

MEGA_SENA = LotteryDef(
    slug="megasena",
    name="Mega-Sena",
    api_slug="megasena",
    total_numbers=60,
    picks=6,
    min_bet_size=6,
    max_bet_size=20,
    prize_tiers=[6, 5, 4],
    base_price=6.00,
    grid_rows=6,
    grid_cols=10,
    tier_to_faixa={6: 1, 5: 2, 4: 3},
)


LOTTERIES: Dict[str, LotteryDef] = {
    LOTOFACIL.slug: LOTOFACIL,
    QUINA.slug: QUINA,
    MEGA_SENA.slug: MEGA_SENA,
}


def get_lottery(slug: str) -> LotteryDef:
    """Busca uma modalidade pelo slug, com erro explicito se nao existir."""
    try:
        return LOTTERIES[slug]
    except KeyError:
        disponiveis = ", ".join(sorted(LOTTERIES))
        raise ValueError(f"Modalidade desconhecida: {slug!r}. Disponíveis: {disponiveis}")
