"""
Modelo de popularidade de apostas — o unico fator com ganho esperado real.

A chance de acertar e a mesma para qualquer jogo. O que NAO e igual e quanto
voce leva quando acerta: premio de faixa e rateado entre os ganhadores, entao
um jogo que pouca gente marca vale mais do que um jogo que todo mundo marca.

Como estimar quem marca o que sem ter acesso ao volume de apostas por jogo?
Usando dois campos que a Caixa publica em cada concurso:

    valorArrecadado         -> quantos jogos simples foram apostados
    numeroDeGanhadores      -> quantos deles eram exatamente o sorteio

Se as apostas fossem uniformes, o numero esperado de ganhadores da faixa
principal seria `jogos_apostados / C(n, k)`. O desvio entre o observado e o
esperado, concurso a concurso, e justamente o efeito de popularidade daquela
combinacao. Regredindo esse desvio sobre as features do sorteio obtemos um
modelo de popularidade **calibrado com dados reais**, e nao com achismo.

Detalhe importante que torna a conta valida: o preco de uma aposta e
proporcional a quantos jogos simples ela cobre (uma aposta de 18 dezenas
custa C(18,15) vezes a aposta minima e cobre C(18,15) jogos). Logo
`arrecadacao / preco_base` da o total de jogos simples apostados
independentemente da mistura de tamanhos de aposta. O preco base mudou ao
longo dos anos, e essa variacao e absorvida por efeitos fixos de ano.
"""

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Sequence

import numpy as np

from .analyzer import FeatureSet, build_masks
from .catalog import LotteryDef
from .data_source import Draw

# Features usadas no modelo. Sao as dimensoes em que o comportamento humano
# de marcacao de volante costuma se desviar do aleatorio.
MODEL_FEATURES = [
    "soma",
    "impares",
    "moldura",
    "consecutivos",
    "primos",
    "metade_baixa",
    "multiplos_3",
]


@dataclass
class PopularityModel:
    """Modelo log-linear de popularidade ajustado por regressao de Poisson."""

    lottery: LotteryDef
    feature_names: List[str]
    coefficients: np.ndarray          # coeficiente por feature (escala padronizada)
    intercept: float
    feature_mean: np.ndarray
    feature_std: np.ndarray
    n_draws_used: int
    converged: bool
    tier_used: int
    dispersion: float                 # razao qui-quadrado/gl (>1 = superdispersao)
    fallback: bool = False            # True se caiu no modelo heuristico
    notes: List[str] = field(default_factory=list)

    # ------------------------------------------------------------------ scoring

    def score(self, feature_values: Dict[str, np.ndarray]) -> np.ndarray:
        """Log-popularidade relativa de cada jogo (0 = popularidade media).

        Valores positivos indicam jogos mais disputados que a media; valores
        negativos, jogos menos disputados (melhores para valor esperado).
        """
        stacked = np.column_stack([feature_values[name] for name in self.feature_names])
        standardized = (stacked - self.feature_mean) / self.feature_std
        return standardized @ self.coefficients

    def sharing_multiplier(self, feature_values: Dict[str, np.ndarray]) -> np.ndarray:
        """Quantas vezes mais (ou menos) o premio seria dividido, vs. a media."""
        return np.exp(self.score(feature_values))

    def to_dict(self) -> Dict[str, Any]:
        return {
            "faixa_usada": f"{self.tier_used} acertos",
            "concursos_usados": self.n_draws_used,
            "convergiu": self.converged,
            "dispersao": round(self.dispersion, 3),
            "heuristico": self.fallback,
            "coeficientes": {
                name: round(float(coef), 4)
                for name, coef in zip(self.feature_names, self.coefficients)
            },
            "interpretacao": self._interpret(),
            "observacoes": self.notes,
        }

    def _interpret(self) -> List[str]:
        """Traduz os coeficientes em frases legiveis."""
        out: List[str] = []
        order = np.argsort(-np.abs(self.coefficients))
        for idx in order[:4]:
            name = self.feature_names[idx]
            coef = float(self.coefficients[idx])
            if abs(coef) < 0.01:
                continue
            direcao = "mais disputados" if coef > 0 else "menos disputados"
            out.append(
                f"jogos com '{name}' acima da média tendem a ser {direcao} "
                f"(coef. {coef:+.3f} por desvio-padrão)"
            )
        if not out:
            out.append("nenhuma feature apresentou efeito relevante de popularidade")
        return out


def _poisson_irls(
    design: np.ndarray,
    counts: np.ndarray,
    offset: np.ndarray,
    max_iter: int = 50,
    tol: float = 1e-8,
) -> tuple:
    """Regressao de Poisson por minimos quadrados reponderados (IRLS).

    Implementada na mao para nao adicionar statsmodels/sklearn como
    dependencia — sao ~30 linhas e o problema e pequeno (poucas colunas).

    Returns:
        (beta, convergiu, dispersao)
    """
    beta = np.zeros(design.shape[1], dtype=np.float64)
    # Chute inicial sensato para o intercepto: log da razao media observada.
    total_expected = float(np.exp(offset).sum())
    if total_expected > 0 and counts.sum() > 0:
        beta[0] = np.log(counts.sum() / total_expected)

    converged = False
    for _ in range(max_iter):
        eta = offset + design @ beta
        eta = np.clip(eta, -30.0, 30.0)
        mu = np.exp(eta)
        mu = np.maximum(mu, 1e-9)

        # Passo de Newton-Raphson na forma de minimos quadrados ponderados.
        working = eta - offset + (counts - mu) / mu
        weights = mu

        weighted_design = design * weights[:, None]
        hessian = design.T @ weighted_design
        gradient = weighted_design.T @ working

        # Ridge minimo para estabilidade numerica quando ha colinearidade.
        hessian += np.eye(hessian.shape[0]) * 1e-8

        try:
            new_beta = np.linalg.solve(hessian, gradient)
        except np.linalg.LinAlgError:
            new_beta = np.linalg.lstsq(hessian, gradient, rcond=None)[0]

        if not np.all(np.isfinite(new_beta)):
            break

        delta = float(np.max(np.abs(new_beta - beta)))
        beta = new_beta
        if delta < tol:
            converged = True
            break

    # Dispersao de Pearson: sinaliza se o modelo de Poisson e otimista demais.
    eta = np.clip(offset + design @ beta, -30.0, 30.0)
    mu = np.maximum(np.exp(eta), 1e-9)
    dof = max(len(counts) - design.shape[1], 1)
    dispersion = float(((counts - mu) ** 2 / mu).sum() / dof)

    return beta, converged, dispersion


def _heuristic_model(lottery: LotteryDef, feature_names: List[str], reason: str) -> PopularityModel:
    """Modelo de reserva quando nao ha dados suficientes para ajustar.

    Os sinais vem de padroes de marcacao amplamente documentados (preferencia
    por sequencias, por dezenas baixas e por marcar a moldura do volante).
    Fica explicitamente marcado como heuristico para nao ser confundido com
    o modelo calibrado.
    """
    priors = {
        "soma": 0.0,
        "impares": 0.05,
        "moldura": 0.15,
        "consecutivos": 0.30,
        "primos": 0.05,
        "metade_baixa": 0.20,
        "multiplos_3": 0.0,
    }
    coefficients = np.array([priors.get(name, 0.0) for name in feature_names], dtype=np.float64)
    return PopularityModel(
        lottery=lottery,
        feature_names=feature_names,
        coefficients=coefficients,
        intercept=0.0,
        feature_mean=np.zeros(len(feature_names)),
        feature_std=np.ones(len(feature_names)),
        n_draws_used=0,
        converged=False,
        tier_used=lottery.prize_tiers[0],
        dispersion=float("nan"),
        fallback=True,
        notes=[f"modelo heurístico (não calibrado): {reason}"],
    )


def fit_popularity(
    draws: Sequence[Draw],
    lottery: LotteryDef,
    feature_set: Optional[FeatureSet] = None,
    tier: Optional[int] = None,
    min_draws: int = 200,
) -> PopularityModel:
    """Ajusta o modelo de popularidade sobre o historico.

    Args:
        draws: historico (apenas concursos visiveis ao mundo, no backtest)
        tier: faixa de acertos usada como alvo. O padrao e a faixa principal,
              que mede a popularidade da combinacao exata. Faixas menores tem
              muito mais ganhadores (menos ruido) mas medem a popularidade da
              vizinhanca do sorteio, nao dele mesmo.
    """
    features = feature_set or FeatureSet(lottery)
    feature_names = [name for name in MODEL_FEATURES if name in features.names]
    target_tier = tier or lottery.prize_tiers[0]

    # So servem concursos com arrecadacao publicada e rateio da faixa alvo.
    usable = [
        d for d in draws
        if d.revenue > 0 and target_tier in d.prizes and d.date
    ]
    if len(usable) < min_draws:
        return _heuristic_model(
            lottery, feature_names,
            f"apenas {len(usable)} concursos com arrecadação publicada "
            f"(mínimo {min_draws})",
        )

    masks = build_masks(usable, lottery)
    feature_values = features.evaluate(masks)
    raw = np.column_stack([feature_values[name] for name in feature_names])

    feature_mean = raw.mean(axis=0)
    feature_std = raw.std(axis=0)
    feature_std[feature_std < 1e-9] = 1.0  # feature constante nao informa nada
    standardized = (raw - feature_mean) / feature_std

    counts = np.array([usable[i].prizes[target_tier].winners for i in range(len(usable))], dtype=np.float64)

    # Offset: log do numero esperado de ganhadores se as apostas fossem
    # uniformes. O preco base (desconhecido e variavel no tempo) vira um
    # fator constante dentro de cada ano, capturado pelos dummies de ano.
    total_combinations = float(lottery.total_combinations)
    revenue = np.array([d.revenue for d in usable], dtype=np.float64)
    offset = np.log(revenue / total_combinations)

    # Efeitos fixos de ano: absorvem mudanca de preco e de volume de apostas.
    years = np.array([int(d.date[:4]) for d in usable])
    unique_years = np.unique(years)
    year_dummies = np.zeros((len(usable), len(unique_years) - 1), dtype=np.float64)
    for col, year in enumerate(unique_years[1:]):  # primeiro ano = referencia
        year_dummies[:, col] = (years == year).astype(np.float64)

    intercept_col = np.ones((len(usable), 1), dtype=np.float64)
    design = np.hstack([intercept_col, standardized, year_dummies])

    beta, converged, dispersion = _poisson_irls(design, counts, offset)

    n_feat = len(feature_names)
    coefficients = beta[1:1 + n_feat]

    notes = [
        f"calibrado em {len(usable)} concursos com {len(unique_years)} efeitos fixos de ano",
        f"alvo: nº de ganhadores da faixa de {target_tier} acertos",
    ]
    if dispersion > 2.0:
        notes.append(
            f"superdispersão alta ({dispersion:.1f}): os coeficientes indicam direção "
            f"confiável, mas a magnitude deve ser lida com cautela"
        )
    if not converged:
        notes.append("IRLS não convergiu totalmente; coeficientes podem ser instáveis")

    return PopularityModel(
        lottery=lottery,
        feature_names=feature_names,
        coefficients=coefficients,
        intercept=float(beta[0]),
        feature_mean=feature_mean,
        feature_std=feature_std,
        n_draws_used=len(usable),
        converged=converged,
        tier_used=target_tier,
        dispersion=dispersion,
        notes=notes,
    )
