# MiroFish Loterias

Motor de mundos paralelos aplicado às loterias da Caixa. Mantém a estrutura do
MiroFish original (pipeline em etapas, execução em background, relatório final)
e troca o miolo: no lugar da simulação social com OASIS/Zep, entra um motor
estatístico-combinatório que roda inteiramente local.

Interface: **`/loteria`** · API: **`/api/lottery/*`**

---

## O que este sistema faz — e o que ele não faz

**Não faz:** prever resultados. Sorteios são eventos independentes e uniformes.
A chance de acertar as 15 na Lotofácil é 1 em 3.268.760 para qualquer jogo,
seja ele `01-02-03…15` ou o mais "equilibrado" dos jogos. Nenhuma análise de
frequência, atraso ou padrão altera isso, e o backtest embutido existe
justamente para mostrar essa realidade em números.

**Faz**, com fundamento verificável:

| Alavanca | Como funciona | Ganho |
|---|---|---|
| **Oportunidades (quando apostar)** | Identidade exata do prêmio rateado `EV = P·(1-(1-p)^N)/N` aplicada ao bolo anunciado do próximo concurso de cada modalidade | O único mecanismo com precedente documentado de lucro real (Cash WinFall/MIT): esperar o concurso em que a regra fica favorável |
| **Anti-popularidade** | Modelo de Poisson calibrado com a arrecadação e o nº de ganhadores publicados pela Caixa, estimando quais perfis de jogo são mais marcados | Não muda a chance de ganhar; muda **quanto se leva**, porque o prêmio é rateado entre os acertadores. Validado fora da amostra (r=0,51 em 665 concursos nunca vistos) |
| **Carteira otimizada** | Seleção gulosa submodular maximizando P(bilhete ≥ faixa alvo), com desempate anti-popular | +3,5 p.p. em P(≥11) na Lotofácil vs seleção heurística, no mesmo custo (76,4% vs 72,9%) |
| **Fechamento** | Cobertura gulosa + melhoria local por troca, com garantia **certificada por enumeração exaustiva** | A melhoria local fechou com 20 jogos a garantia que antes exigia 30 (R$ 70 vs R$ 105) |
| **Backtest walk-forward** | Cada mundo só enxerga concursos anteriores ao alvo | Mostra honestamente quais hipóteses não se sustentam |
| **Monitor de viés** | Estatística T contra distribuição nula simulada (Monte Carlo) + persistência split-half + vantagem fora da amostra | Detectou viés real na Lotofácil (p=0,0001) — e quantificou que ele é economicamente irrelevante (retorno 41%→44% contra margem de 59%) |
| **Valor esperado** | Probabilidades hipergeométricas exatas × rateio médio real | Deixa explícito o retorno estrutural (na Lotofácil, ~41%) |

---

## Arquitetura

```
backend/app/lottery/
├── catalog.py         Definição declarativa das modalidades (Lotofácil, Quina, Mega-Sena)
├── data_source.py     Cliente da API da Caixa + cache JSONL append-only
├── combinatorics.py   Núcleo: máscaras de bits (≤64 dezenas) e índices (>64), popcount, LUTs
├── analyzer.py        Estatísticas do histórico (frequência, atraso, Markov, repetição)
├── popularity.py      Regressão de Poisson (IRLS) que calibra a popularidade das apostas
├── worlds.py          Os mundos paralelos (uma hipótese cada)
├── portfolio.py       Otimizador de carteira: maximiza P(bilhete ≥ faixa alvo)
├── wheeling.py        Fechamentos (bits locais ao conjunto base) + certificação exata
├── backtest.py        Competição walk-forward + veredito estatístico
├── economics.py       Valor esperado e ajuste por rateio
├── opportunity.py     Caçador de oportunidades: EV do próximo concurso, roll-downs
├── bias.py            Monitor de viés físico (Monte Carlo)
└── engine.py          Orquestra as 5 etapas e persiste o estudo

backend/app/api/lottery.py     Rotas HTTP
backend/data/*.jsonl           Cache dos históricos (versionados): lotofacil 3.750,
                               quina 7.080, megasena 3.038 concursos
frontend/src/views/LotteryView.vue
frontend/src/api/lottery.js
```

### O truque que torna tudo viável

Para universos de até 64 dezenas, um jogo é uma máscara de bits (bit *i-1*
ligado = dezena *i* marcada). Daí:

```python
acertos = popcount(jogo & sorteio)
```

Com 25 dezenas, o espaço **completo** da Lotofácil — 3.268.760 jogos — ocupa
13 MB como `uint32`; os 50.063.860 da Mega-Sena, 400 MB como `uint64`.
Enumerar leva de 0,35 s (Lotofácil) a 17 s (Mega). Por isso "escolher os
melhores jogos" não é heurística aqui: é **busca exata sobre o espaço inteiro**.

A Quina tem 80 dezenas — não cabe em máscara de 64 bits. Para ela o motor usa
a segunda representação: cada jogo é uma linha `(picks,)` de dezenas `uint8`
(24.040.016 × 5 = 120 MB), e as mesmas operações saem por *gather*:

```python
acertos = membro[jogo].sum(axis=1)     # membro = bool por dezena
feature = pesos[jogo].sum(axis=1)
```

Nos fechamentos, as dezenas do conjunto base são remapeadas para bits locais
0..b-1 — então a construção funciona em qualquer universo, e a certificação
continua exaustiva (a garantia depende só da interseção do sorteio com o base).

Features aditivas sobre máscaras (soma, ímpares, primos, moldura, dezenas por
linha, aniversário ≤31) são avaliadas por tabelas de lookup de 16 bits — dois
`gather` por bloco, independentemente de quantos jogos existam.

### Os mundos paralelos

| Mundo | Categoria | Hipótese |
|---|---|---|
| `uniforme` | **controle** | Escolhe ao acaso. É a régua contra a qual todos os outros são medidos |
| `frequencia` | a testar | Dezenas quentes continuam quentes |
| `atraso` | a testar | Dezenas atrasadas estão "devendo" |
| `equilibrio` | a testar | Ficar nas faixas típicas de soma/paridade/distribuição |
| `markov` | a testar | A dezena depende de ter saído no concurso anterior |
| `repeticao` | a testar | Jogar na moda da distribuição de repetições |
| `anti_popular` | **valor esperado** | Evitar as regiões mais marcadas do espaço |
| `hibrido` | **valor esperado** | Faixas típicas + região menos disputada |

Os mundos "a testar" estão ali para serem medidos, não porque funcionem. Cada
um carrega um campo `ressalva` que adianta o que a teoria prevê, e o backtest
confirma.

---

## Instalação na VPS

O modo loteria **não usa LLM nem Zep** e dispensa `camel-oasis`/`torch` — o que
elimina ~6-8 GB de disco e ~3 GB de RAM em relação ao MiroFish completo.

```bash
# Backend
cd backend
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-loteria.txt      # em vez de requirements.txt

export MIROFISH_MODE=loteria                 # não exige LLM_API_KEY nem ZEP_API_KEY
python run.py

# Frontend (produção: build estático, não o dev server)
cd ../frontend
npm ci && npm run build
# sirva frontend/dist com nginx, com /api/ apontando para :5001
```

**Requisitos reais no modo loteria:** 2 vCPU / 4 GB / 40 GB dão conta.
Com 4 vCPU / 8 GB fica confortável, e com 16 GB de RAM cabe o espaço completo
da Mega-Sena (50.063.860 combinações, ~300 MB) em memória.

**Segurança:** a API não tem autenticação — não exponha a porta 5001 na
internet. Use Tailscale ou nginx com senha e firewall restrito ao seu IP.

---

## Uso da API

```bash
BASE=http://localhost:5001/api/lottery

# Catálogo
curl $BASE/modalidades
curl $BASE/mundos

# Dados (o cache já vem com 3.750 concursos; isto busca só o que falta)
curl -X POST $BASE/sync -H 'Content-Type: application/json' -d '{"modalidade":"lotofacil"}'
curl "$BASE/historico/lotofacil?limit=5"

# Análise (estatísticas + popularidade + valor esperado)
curl $BASE/analise/lotofacil

# Estudo completo (roda em background)
curl -X POST $BASE/estudo -H 'Content-Type: application/json' -d '{
  "modalidade": "lotofacil",
  "n_jogos": 8,
  "modo": "fechamento",
  "rodar_backtest": true,
  "backtest_concursos": 60
}'
curl $BASE/task/<task_id>       # progresso
curl $BASE/estudo/<run_id>      # boletim completo

# Ferramentas avulsas
curl -X POST $BASE/fechamento -H 'Content-Type: application/json' \
  -d '{"dezenas":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18],"n_jogos":20}'
curl -X POST $BASE/conferir -H 'Content-Type: application/json' \
  -d '{"jogos":[[1,2,5,6,7,11,12,13,15,18,19,20,22,24,25]]}'
```

### Modos de carteira

- `valor_esperado` — todos os jogos vêm do mundo híbrido, nas regiões menos disputadas
- `fechamento` — cobertura sobre um conjunto base, com garantia certificada
- `diversificado` — divide o orçamento entre os mundos, para comparação prática

---

## Resultados observados

Backtest de 60 concursos (3691–3750), 5 jogos por concurso por mundo:

| Mundo | Média de acertos | ROI |
|---|---|---|
| frequencia | 9,033 | −68,0% |
| anti_popular | 9,010 | −70,7% |
| markov | 9,033 | −72,0% |
| equilibrio | 9,090 | −74,0% |
| **uniforme (controle)** | **8,870** | **−82,0%** |

Todos os mundos convergem para ~9,0 acertos — exatamente o valor teórico
(15 × 15 ÷ 25 = 9). As diferenças ficam dentro do ruído (|t| < 2 em quase
todos), e o único caso com t > 2 é o que se espera ao testar oito hipóteses
simultaneamente. O ROI é negativo em todos os cenários, como tem que ser.

### Validação fora da amostra (out-of-sample)

O modelo de popularidade é treinado em 80% do histórico e testado nos 20%
finais — concursos que ele nunca viu. Correlação entre o score do modelo e o
rateio realmente observado:

| Modalidade | Faixa | Correlação | Ruído esperado | Veredito |
|---|---|---|---|---|
| Lotofácil | 15 acertos | +0,246 | ±0,039 | sinal real |
| Lotofácil | 14 acertos | +0,505 | ±0,039 | sinal real |
| Mega-Sena | 5 acertos (quina) | +0,505 | ±0,050 | sinal real |
| Mega-Sena | 6 acertos (sena) | +0,065 | ±0,050 | inconclusivo* |
| Quina | 4 acertos (quadra) | +0,509 | ±0,032 | sinal real |
| Quina | 5 acertos | +0,017 | ±0,032 | inconclusivo* |

\* Nas faixas principais de Mega e Quina quase todo concurso tem 0 ganhadores —
não há estatística para validar. O sinal forte está nas faixas secundárias,
que são justamente as rateadas com frequência.

Na Mega, os coeficientes confirmam o viés de aniversário da literatura
(Ziemba; Clotfelter & Cook): `soma −0,30` e `metade_baixa +0,24` — jogos de
dezenas baixas (datas) têm sistematicamente mais ganhadores, logo rateiam pior.

### O que o modelo de popularidade encontrou

Calibrado em 3.322 concursos com arrecadação publicada, o coeficiente mais
forte foi **`consecutivos` = −0,352 por desvio-padrão**: sorteios com mais
pares de dezenas consecutivas tiveram *menos* ganhadores, ou seja, jogos com
dezenas consecutivas são **menos** marcados pelos apostadores. Isso contraria a
intuição comum (a de que as pessoas gostam de sequências) — e é justamente por
isso que o modelo é ajustado com dados em vez de suposto.

O modelo fica restrito ao **suporte empírico** do histórico (percentis 1–99 de
cada feature, ~94,7% do espaço). Sem esse recorte, maximizar a
anti-popularidade levava a jogos absurdos como 15 dezenas consecutivas: o
modelo diz que seriam pouco disputados, mas nenhum sorteio real jamais teve
esse perfil, então a afirmação seria pura extrapolação.

### Fechamento: ganho real e medível

18 dezenas no conjunto base, 20 jogos (R$ 70,00), comparado com 20 jogos
sorteados ao acaso dentro das mesmas 18 dezenas:

| Dezenas do conjunto sorteadas | Chance | Fechamento garante | Aleatório garante |
|---|---|---|---|
| 13 | 5,50% | **12 pontos** | 11 pontos |
| 12 | 19,88% | **11 pontos** | 10 pontos |
| 11 | 34,08% | **10 pontos** | 9 pontos |

Mesmo custo, garantia estritamente melhor — verificado por enumeração dos
3.268.760 sorteios possíveis, não por estimativa.

---

## Notas técnicas

- **Preço da aposta** (`base_price` em `catalog.py`) é parâmetro: atualize se a
  Caixa reajustar. O backtest usa o preço atual para toda a janela, o que é uma
  aproximação declarada.
- **Prêmio no backtest** usa o valor histórico real da faixa. Isso ignora que,
  se você tivesse ganhado, o rateio teria mais um acertador — desprezível nas
  faixas baixas, relevante na principal.
- **Superdispersão** no modelo de popularidade (≈3,9) indica que a direção dos
  coeficientes é confiável, mas a magnitude deve ser lida com cautela. Está
  registrado na resposta da API.
- **Lotomania** não é suportada: C(100,50) é grande demais para enumerar; exigiria
  amostragem em vez de busca exaustiva.
- **Requisitos por modalidade** (pico medido num estudo completo com backtest):
  Lotofácil ~1 GB; Quina ~6,5 GB; Mega-Sena ~11,3 GB. Uma VPS de 16 GB roda
  qualquer uma delas, mas **não rode dois estudos de Mega/Quina em paralelo**.
  Tempos no hardware de referência (4 vCPU): Lotofácil ~35 s, Quina ~3,5 min,
  Mega ~6,5 min.
- **API nova**: `GET /api/lottery/oportunidades` (EV do próximo concurso de cada
  modalidade), `GET /api/lottery/vies/<slug>` (monitor de viés, cacheado) e o
  modo de carteira `"otimizado"` no `POST /api/lottery/estudo` (parâmetro
  opcional `alvo_otimizacao`: faixa de pontos a maximizar).

---

## Aviso

Loteria é jogo de retorno esperado negativo por desenho — na Lotofácil, cada
R$ 3,50 apostados devolvem em média R$ 1,43. Este sistema não muda isso e não
aumenta a chance de ninguém acertar. Aposte apenas o que puder perder
integralmente.
