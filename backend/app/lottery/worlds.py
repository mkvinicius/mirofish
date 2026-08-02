"""
Mundos paralelos: cada mundo e uma hipotese de aposta que compete com as outras.

Um mundo recebe o mesmo contexto (historico visivel + espaco de jogos) e
devolve seus melhores jogos segundo a propria hipotese. Nenhum mundo tem
acesso ao concurso alvo — quem decide qual hipotese presta e o backtest
walk-forward em `backtest.py`, nao o autor do codigo.

Os mundos sao deliberadamente heterogeneos em qualidade epistemica:

  - `uniforme` e o grupo de controle. Se um mundo "esperto" nao bate o
    sorteio aleatorio de forma consistente, ele nao esta agregando nada.
  - `frequencia`, `atraso`, `markov` e `repeticao` sao as hipoteses que
    apostadores usam na pratica. Elas estao aqui para serem testadas, e o
    campo `honest_note` de cada uma ja adianta o que a teoria preve.
  - `anti_popular` e `equilibrio` sao os unicos com fundamento economico:
    nao mudam a chance de acertar, mudam quanto se leva ao acertar.
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Sequence, Tuple

import numpy as np

from . import combinatorics as cb
from .analyzer import FeatureSet, HistoryAnalysis
from .catalog import LotteryDef
from .popularity import PopularityModel


@dataclass
class SpaceCache:
    """Espaco de jogos enumerado + features intrinsecas, calculado uma vez.

    As features de um jogo (soma, impares, moldura...) nao dependem de qual
    concurso esta sendo previsto, entao calcular isso uma unica vez e
    reaproveitar em todos os mundos e em todos os passos do backtest e o que
    torna o walk-forward viavel.
    """

    lottery: LotteryDef
    masks: np.ndarray
    features: Dict[str, np.ndarray]

    @classmethod
    def build(cls, lottery: LotteryDef, feature_set: FeatureSet) -> "SpaceCache":
        if not lottery.enumerable:
            raise ValueError(
                f"{lottery.name} tem {lottery.total_combinations:,} combinações — "
                f"grande demais para enumerar. Use amostragem."
            )
        masks = cb.enumerate_masks(lottery.total_numbers, lottery.picks)
        raw = feature_set.evaluate(masks)
        # float32 corta o cache pela metade sem perda relevante de precisao
        # nas features (todas sao inteiros pequenos ou somas de inteiros).
        features = {name: arr.astype(np.float32) for name, arr in raw.items()}
        return cls(lottery=lottery, masks=masks, features=features)

    @property
    def size(self) -> int:
        return len(self.masks)

    def memory_mb(self) -> float:
        total = self.masks.nbytes + sum(a.nbytes for a in self.features.values())
        return total / 1e6


@dataclass
class WorldContext:
    """Tudo que um mundo pode enxergar ao gerar seus jogos."""

    lottery: LotteryDef
    space: SpaceCache
    analysis: HistoryAnalysis
    popularity: PopularityModel
    feature_set: FeatureSet
    seed: int = 0

    _repeat_cache: Optional[np.ndarray] = field(default=None, repr=False)
    _support_cache: Optional[np.ndarray] = field(default=None, repr=False)

    @property
    def rng(self) -> np.random.Generator:
        return np.random.default_rng(self.seed)

    @property
    def previous_mask(self) -> int:
        """Mascara do ultimo concurso visivel."""
        return cb.mask_from_numbers(self.analysis.last_draw_numbers)

    def repeats_vs_previous(self) -> np.ndarray:
        """Quantas dezenas cada jogo repete do ultimo concurso visivel.

        Unica feature que depende do concurso, entao fica fora do SpaceCache
        e e memoizada por contexto.
        """
        if self._repeat_cache is None:
            self._repeat_cache = cb.hits_against(self.space.masks, self.previous_mask)
        return self._repeat_cache

    def support_mask(self, feature_names: Sequence[str]) -> np.ndarray:
        """Jogos cujo perfil cai dentro da faixa observada em sorteios reais.

        Modelos ajustados no historico so valem onde ha historico. Sem esse
        recorte, maximizar o modelo de popularidade leva a jogos absurdos
        (uma sequencia de 15 dezenas consecutivas, por exemplo): o modelo diz
        que seriam pouco disputados, mas nenhum sorteio real jamais teve esse
        perfil, entao a afirmacao e pura extrapolacao.
        """
        if self._support_cache is None:
            mask = np.ones(self.space.size, dtype=bool)
            for name in feature_names:
                stats = self.analysis.feature_stats.get(name)
                if stats is None or name not in self.space.features:
                    continue
                values = self.space.features[name]
                mask &= (values >= np.float32(stats.p01)) & (values <= np.float32(stats.p99))
            # Se o recorte zerou o espaco (janela curta demais), desiste dele.
            if not mask.any():
                mask = np.ones(self.space.size, dtype=bool)
            self._support_cache = mask
        return self._support_cache


@dataclass
class Candidate:
    """Um jogo proposto por um mundo."""

    numbers: Tuple[int, ...]
    score: float
    world: str
    rationale: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {
            "dezenas": list(self.numbers),
            "score": round(float(self.score), 4),
            "mundo": self.world,
            "justificativa": self.rationale,
        }


# ------------------------------------------------------------------- selecao

def select_diverse(
    masks: np.ndarray,
    scores: np.ndarray,
    n_games: int,
    picks: int,
    max_overlap: Optional[int] = None,
    pool_size: int = 200_000,
) -> Tuple[np.ndarray, np.ndarray]:
    """Escolhe os melhores jogos garantindo diversidade entre eles.

    Pegar simplesmente o top-N por score produz N jogos quase identicos
    (trocando uma dezena), o que concentra risco: ou todos acertam, ou
    nenhum. Aqui pegamos um pool dos melhores e selecionamos gulosamente
    respeitando um limite de sobreposicao entre jogos ja escolhidos.

    Returns:
        (mascaras escolhidas, scores correspondentes)
    """
    if max_overlap is None:
        # Sobreposicao maxima padrao: ~60% das dezenas.
        max_overlap = max(1, int(picks * 0.6))

    pool_size = min(pool_size, len(scores))
    # argpartition e O(n) — evita ordenar 3,27 milhoes de elementos.
    pool_idx = np.argpartition(-scores, pool_size - 1)[:pool_size]
    pool_idx = pool_idx[np.argsort(-scores[pool_idx])]

    pool_masks = masks[pool_idx]
    pool_scores = scores[pool_idx]

    chosen: List[int] = []
    chosen_masks = np.empty(n_games, dtype=masks.dtype)
    n_chosen = 0

    for i in range(len(pool_masks)):
        candidate = pool_masks[i]
        if n_chosen:
            overlaps = cb.popcount(chosen_masks[:n_chosen] & candidate)
            if overlaps.max() > max_overlap:
                continue
        chosen_masks[n_chosen] = candidate
        chosen.append(i)
        n_chosen += 1
        if n_chosen >= n_games:
            break

    # Se a restricao de diversidade foi rigida demais, completa com o topo.
    if n_chosen < n_games:
        already = set(chosen)
        for i in range(len(pool_masks)):
            if i in already:
                continue
            chosen_masks[n_chosen] = pool_masks[i]
            chosen.append(i)
            n_chosen += 1
            if n_chosen >= n_games:
                break

    idx = np.array(chosen[:n_chosen], dtype=np.int64)
    return chosen_masks[:n_chosen], pool_scores[idx]


def _zscore(arr: np.ndarray) -> np.ndarray:
    """Padroniza um vetor de scores (media 0, desvio 1)."""
    std = float(arr.std())
    if std < 1e-12:
        return np.zeros_like(arr, dtype=np.float32)
    return ((arr - arr.mean()) / std).astype(np.float32)


# -------------------------------------------------------------------- mundos

class ParallelWorld(ABC):
    """Interface de um mundo paralelo."""

    slug: str = ""
    name: str = ""
    hypothesis: str = ""
    honest_note: str = ""
    category: str = "estatistico"

    # Mundos que dependem de um modelo ajustado no historico devem ficar
    # restritos ao suporte desse historico (ver WorldContext.support_mask).
    restrict_to_support: bool = False
    support_features: Sequence[str] = ("soma", "impares", "moldura", "consecutivos", "primos")

    @abstractmethod
    def score(self, ctx: WorldContext) -> np.ndarray:
        """Pontua TODO o espaco de jogos segundo a hipotese do mundo."""

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        """Justificativa especifica para um jogo escolhido."""
        return self.hypothesis

    def _final_scores(self, ctx: WorldContext) -> np.ndarray:
        """Score do mundo com recorte de suporte e desempate aleatorio."""
        scores = self.score(ctx).astype(np.float64)

        if self.restrict_to_support:
            supported = ctx.support_mask(self.support_features)
            scores = np.where(supported, scores, -np.inf)

        # Varios mundos produzem empates exatos (o mundo da repeticao, por
        # exemplo, da o mesmo score a todo jogo com o mesmo nº de repeticoes).
        # Sem desempate aleatorio, `argpartition` resolveria pela ordem do
        # indice, que corresponde as dezenas mais baixas — um vies silencioso.
        finite = scores[np.isfinite(scores)]
        scale = float(finite.std()) if finite.size else 0.0
        if scale <= 0:
            scale = 1.0
        jitter_rng = np.random.default_rng(ctx.seed + 9973)
        return scores + jitter_rng.random(len(scores)) * scale * 1e-6

    def generate(self, ctx: WorldContext, n_games: int, max_overlap: Optional[int] = None) -> List[Candidate]:
        """Gera os `n_games` melhores jogos do mundo, com diversidade."""
        scores = self._final_scores(ctx)
        masks, chosen_scores = select_diverse(
            ctx.space.masks, scores, n_games, ctx.lottery.picks, max_overlap
        )
        out: List[Candidate] = []
        for mask, score in zip(masks, chosen_scores):
            numbers = cb.numbers_from_mask(int(mask))
            out.append(Candidate(
                numbers=numbers,
                score=float(score),
                world=self.slug,
                rationale=self.explain(ctx, numbers),
            ))
        return out

    def to_dict(self) -> Dict[str, str]:
        return {
            "slug": self.slug,
            "nome": self.name,
            "hipotese": self.hypothesis,
            "ressalva": self.honest_note,
            "categoria": self.category,
        }


class UniformWorld(ParallelWorld):
    slug = "uniforme"
    name = "Mundo Uniforme (controle)"
    hypothesis = "Todo jogo tem exatamente a mesma chance; escolhe ao acaso."
    honest_note = (
        "Este é o grupo de controle. Estatisticamente, é o comportamento que "
        "todos os outros mundos deveriam igualar — se algum mundo o supera de "
        "forma consistente em acertos, isso é sinal de sorte na amostra, não de "
        "previsão."
    )
    category = "controle"

    def score(self, ctx: WorldContext) -> np.ndarray:
        return ctx.rng.random(ctx.space.size, dtype=np.float32)

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        return "Sorteado uniformemente entre todos os jogos possíveis."


class FrequencyWorld(ParallelWorld):
    slug = "frequencia"
    name = "Mundo das Dezenas Quentes"
    hypothesis = "Dezenas que saíram mais no passado continuam saindo mais."
    honest_note = (
        "Hipótese sem fundamento: os sorteios são independentes e as pequenas "
        "diferenças de frequência são flutuação amostral. Incluído para ser "
        "medido — e desmentido — pelo backtest."
    )

    def score(self, ctx: WorldContext) -> np.ndarray:
        weights = ctx.analysis.relative_frequency
        feature = cb.AdditiveFeature(weights, ctx.lottery.total_numbers)
        return _zscore(feature.evaluate(ctx.space.masks))

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        hot = set(ctx.analysis.hot_numbers(10))
        matching = sorted(hot & set(numbers))
        return f"Contém {len(matching)} das 10 dezenas mais frequentes: {matching}."


class OverdueWorld(ParallelWorld):
    slug = "atraso"
    name = "Mundo das Dezenas Atrasadas"
    hypothesis = "Dezenas que estão há muitos concursos sem sair estão 'devendo'."
    honest_note = (
        "É a falácia do apostador em forma pura: a bola não lembra há quanto "
        "tempo não é sorteada. Incluído para comparação."
    )

    def score(self, ctx: WorldContext) -> np.ndarray:
        gaps = ctx.analysis.gaps.astype(np.float64)
        # Normaliza pelo atraso medio para nao privilegiar dezenas que
        # simplesmente saem menos.
        normalized = gaps / np.maximum(ctx.analysis.mean_gap, 1e-6)
        feature = cb.AdditiveFeature(normalized, ctx.lottery.total_numbers)
        return _zscore(feature.evaluate(ctx.space.masks))

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        overdue = set(ctx.analysis.overdue_numbers(10))
        matching = sorted(overdue & set(numbers))
        return f"Contém {len(matching)} das 10 dezenas mais atrasadas: {matching}."


class BalanceWorld(ParallelWorld):
    slug = "equilibrio"
    name = "Mundo do Equilíbrio Estatístico"
    hypothesis = (
        "Sorteios reais caem nas faixas típicas de soma, paridade e distribuição "
        "no volante; jogos fora dessas faixas são desperdício."
    )
    honest_note = (
        "Parcialmente válido, mas por um motivo diferente do que se costuma "
        "dizer: as faixas típicas concentram a maioria dos jogos possíveis, "
        "então ficar nelas não aumenta a chance de acerto — apenas evita jogos "
        "extremos, que por acaso também são os mais marcados por apostadores."
    )

    FEATURES = ["soma", "impares", "moldura", "consecutivos", "primos"]

    def score(self, ctx: WorldContext) -> np.ndarray:
        penalty = np.zeros(ctx.space.size, dtype=np.float32)
        for name in self.FEATURES:
            stats = ctx.analysis.feature_stats.get(name)
            if stats is None or name not in ctx.space.features:
                continue
            spread = max(stats.std, 1e-6)
            deviation = (ctx.space.features[name] - np.float32(stats.mean)) / np.float32(spread)
            penalty += deviation ** 2
        return -penalty

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        values = ctx.feature_set.evaluate_one(numbers)
        partes = []
        for name in ("soma", "impares", "moldura", "consecutivos"):
            stats = ctx.analysis.feature_stats.get(name)
            if stats:
                partes.append(f"{name}={values[name]:.0f} (típico {stats.mean:.1f})")
        return "Dentro das faixas históricas: " + ", ".join(partes) + "."


class MarkovWorld(ParallelWorld):
    slug = "markov"
    name = "Mundo de Markov"
    hypothesis = (
        "A probabilidade de uma dezena sair depende de ela ter saído no "
        "concurso anterior."
    )
    honest_note = (
        "Testável e provavelmente nulo: se os sorteios forem independentes, as "
        "duas probabilidades condicionais convergem para a mesma coisa. O "
        "backtest mostra se a diferença observada passa do ruído."
    )

    def score(self, ctx: WorldContext) -> np.ndarray:
        prev = set(ctx.analysis.last_draw_numbers)
        weights = np.zeros(ctx.lottery.total_numbers, dtype=np.float64)
        for i in range(ctx.lottery.total_numbers):
            p = (
                ctx.analysis.markov_given_present[i]
                if (i + 1) in prev
                else ctx.analysis.markov_given_absent[i]
            )
            # Log-probabilidade para que a soma sobre o jogo seja a
            # log-verossimilhanca do jogo sob o modelo de Markov.
            weights[i] = np.log(max(p, 1e-6))
        feature = cb.AdditiveFeature(weights, ctx.lottery.total_numbers)
        return _zscore(feature.evaluate(ctx.space.masks))

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        prev = set(ctx.analysis.last_draw_numbers)
        repetidas = sorted(prev & set(numbers))
        return (
            f"Sob as transições estimadas, repete {len(repetidas)} dezenas do "
            f"concurso {ctx.analysis.last_draw}: {repetidas}."
        )


class RepeatWorld(ParallelWorld):
    slug = "repeticao"
    name = "Mundo da Repetição"
    hypothesis = (
        "A quantidade de dezenas repetidas de um concurso para o seguinte tem "
        "uma distribuição estável; vale jogar na moda dessa distribuição."
    )
    honest_note = (
        "A distribuição de repetições é de fato estável — mas é exatamente a "
        "distribuição hipergeométrica esperada ao acaso. Escolher a moda não "
        "aumenta a chance de acertar as dezenas certas, apenas concentra os "
        "jogos onde a maioria dos jogos possíveis já está."
    )

    def score(self, ctx: WorldContext) -> np.ndarray:
        repeats = ctx.repeats_vs_previous().astype(np.float32)
        target = np.float32(ctx.analysis.expected_repeats)
        spread = float(ctx.analysis.repeat_counts.std()) if ctx.analysis.repeat_counts.size else 1.0
        spread = max(spread, 1e-6)
        return -(((repeats - target) / np.float32(spread)) ** 2)

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        prev = set(ctx.analysis.last_draw_numbers)
        n_rep = len(prev & set(numbers))
        return (
            f"Repete {n_rep} dezenas do concurso anterior, contra média "
            f"histórica de {ctx.analysis.expected_repeats:.1f}."
        )


class AntiPopularWorld(ParallelWorld):
    slug = "anti_popular"
    name = "Mundo Anti-Popular"
    hypothesis = (
        "Evitar as combinações que muita gente marca não aumenta a chance de "
        "ganhar, mas aumenta quanto se leva quando ganha."
    )
    honest_note = (
        "Único mundo com ganho esperado demonstrável. O prêmio de cada faixa é "
        "rateado entre os ganhadores, então jogar onde há menos gente aumenta o "
        "valor esperado — sem mexer na probabilidade de acerto. Fica restrito ao "
        "suporte do histórico para não extrapolar o modelo."
    )
    category = "valor_esperado"
    restrict_to_support = True

    def score(self, ctx: WorldContext) -> np.ndarray:
        feature_values = {
            name: ctx.space.features[name]
            for name in ctx.popularity.feature_names
            if name in ctx.space.features
        }
        if len(feature_values) != len(ctx.popularity.feature_names):
            # Modelo pede uma feature que o espaco nao tem: cai para neutro.
            return np.zeros(ctx.space.size, dtype=np.float32)
        popularity = ctx.popularity.score(feature_values)
        return (-popularity).astype(np.float32)

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        values = ctx.feature_set.evaluate_one(numbers)
        arrays = {k: np.array([v]) for k, v in values.items()}
        try:
            multiplier = float(ctx.popularity.sharing_multiplier(arrays)[0])
        except KeyError:
            return self.hypothesis
        if multiplier < 1:
            return (
                f"Estimativa: rateio {1 / multiplier:.2f}x menor que o jogo médio "
                f"(menos apostadores nesta região do espaço)."
            )
        return f"Estimativa de rateio {multiplier:.2f}x o do jogo médio."


class HybridWorld(ParallelWorld):
    slug = "hibrido"
    name = "Mundo Híbrido (equilíbrio + anti-popular)"
    hypothesis = (
        "Ficar nas faixas típicas de sorteio, mas dentro delas escolher as "
        "regiões menos disputadas."
    )
    honest_note = (
        "Combinação das duas ideias defensáveis. O ganho continua sendo de "
        "valor esperado, não de probabilidade."
    )
    category = "valor_esperado"
    restrict_to_support = True

    def __init__(self, balance_weight: float = 0.4, popularity_weight: float = 1.0):
        self.balance_weight = balance_weight
        self.popularity_weight = popularity_weight
        self._balance = BalanceWorld()
        self._anti = AntiPopularWorld()

    def score(self, ctx: WorldContext) -> np.ndarray:
        balance = _zscore(self._balance.score(ctx))
        anti = _zscore(self._anti.score(ctx))
        return self.balance_weight * balance + self.popularity_weight * anti

    def explain(self, ctx: WorldContext, numbers: Sequence[int]) -> str:
        return (
            self._balance.explain(ctx, numbers) + " " + self._anti.explain(ctx, numbers)
        )


# Registro dos mundos disponiveis, na ordem em que aparecem no relatorio.
WORLD_REGISTRY: Dict[str, ParallelWorld] = {
    world.slug: world
    for world in (
        UniformWorld(),
        FrequencyWorld(),
        OverdueWorld(),
        BalanceWorld(),
        MarkovWorld(),
        RepeatWorld(),
        AntiPopularWorld(),
        HybridWorld(),
    )
}

DEFAULT_WORLDS = list(WORLD_REGISTRY)


def get_world(slug: str) -> ParallelWorld:
    try:
        return WORLD_REGISTRY[slug]
    except KeyError:
        disponiveis = ", ".join(WORLD_REGISTRY)
        raise ValueError(f"Mundo desconhecido: {slug!r}. Disponíveis: {disponiveis}")
