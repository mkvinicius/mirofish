<template>
  <div class="lottery-view">
    <header class="app-header">
      <div class="brand" @click="router.push('/')">MIROFISH<span class="brand-tag">loterias</span></div>
      <div class="header-right">
        <span class="status-pill" :class="statusClass">
          <span class="dot"></span>{{ statusText }}
        </span>
      </div>
    </header>

    <main class="content">
      <!-- ======================= COLUNA DE CONFIGURAÇÃO ======================= -->
      <aside class="config-panel">
        <h2 class="panel-title">Configurar estudo</h2>

        <label class="field">
          <span class="field-label">Modalidade</span>
          <select v-model="config.modalidade" :disabled="running">
            <option v-for="m in lotteries" :key="m.slug" :value="m.slug">
              {{ m.nome }} — {{ formatInt(m.combinacoes) }} combinações
            </option>
          </select>
        </label>

        <label class="field">
          <span class="field-label">Quantos jogos<em>{{ config.n_jogos }} · R$ {{ estimatedCost }}</em></span>
          <input type="range" min="1" max="30" v-model.number="config.n_jogos" :disabled="running" />
        </label>

        <div class="field">
          <span class="field-label">Como montar o bilhete</span>
          <div class="mode-options">
            <button
              v-for="m in modes" :key="m.value"
              class="mode-btn" :class="{ active: config.modo === m.value }"
              :disabled="running"
              @click="config.modo = m.value"
            >
              <strong>{{ m.label }}</strong>
              <small>{{ m.hint }}</small>
            </button>
          </div>
        </div>

        <label class="field" v-if="config.modo === 'fechamento'">
          <span class="field-label">Dezenas no conjunto base<em>{{ config.fechamento_dezenas }}</em></span>
          <input type="range" min="16" max="20" v-model.number="config.fechamento_dezenas" :disabled="running" />
        </label>

        <div class="field">
          <span class="field-label">Mundos paralelos</span>
          <div class="world-list">
            <label v-for="w in worlds" :key="w.slug" class="world-item" :title="w.ressalva">
              <input type="checkbox" :value="w.slug" v-model="config.mundos" :disabled="running" />
              <span class="world-name">{{ w.nome }}</span>
              <span class="world-tag" :class="w.categoria">{{ tagLabel(w.categoria) }}</span>
            </label>
          </div>
        </div>

        <label class="field checkbox-field">
          <input type="checkbox" v-model="config.rodar_backtest" :disabled="running" />
          <span>Rodar backtest walk-forward</span>
        </label>

        <label class="field" v-if="config.rodar_backtest">
          <span class="field-label">Concursos de teste<em>{{ config.backtest_concursos }}</em></span>
          <input type="range" min="10" max="200" step="10" v-model.number="config.backtest_concursos" :disabled="running" />
        </label>

        <button class="run-btn" :disabled="running || !config.mundos.length" @click="runStudy">
          {{ running ? 'Rodando…' : 'Rodar estudo' }}
        </button>

        <div v-if="running" class="progress">
          <div class="progress-bar"><div class="progress-fill" :style="{ width: progress + '%' }"></div></div>
          <span class="progress-msg">{{ progressMsg }}</span>
        </div>

        <p v-if="error" class="error-box">{{ error }}</p>

        <div v-if="history" class="history-box">
          <span class="history-line">
            Histórico: <strong>{{ formatInt(history.concursos) }}</strong> concursos
          </span>
          <span class="history-line" v-if="history.ultimo">
            Último: <strong>{{ history.ultimo }}</strong> ({{ history.data_ultimo }})
          </span>
        </div>
      </aside>

      <!-- =========================== RESULTADOS =========================== -->
      <section class="results">
        <div v-if="!study" class="empty-state">
          <h1>Mundos paralelos para loterias</h1>
          <p>
            Cada mundo é uma hipótese de aposta. Todos competem em backtest
            walk-forward sobre o histórico real da Caixa — nenhum deles enxerga
            o concurso que está tentando prever.
          </p>
          <p class="disclaimer-inline">
            Este sistema <strong>não prevê resultados</strong>. Sorteios são
            independentes e a chance de acerto é idêntica para qualquer jogo. O
            que ele otimiza é o <strong>rateio do prêmio</strong> (jogar onde há
            menos apostadores) e a <strong>garantia de prêmios secundários</strong>
            via fechamento.
          </p>
        </div>

        <template v-else>
          <!-- BOLETIM -->
          <div class="card">
            <div class="card-head">
              <h2>Boletim de apostas</h2>
              <span class="card-meta">
                {{ study.boletim.n_jogos }} jogos · R$ {{ study.boletim.custo_total.toFixed(2) }}
              </span>
            </div>

            <div v-for="(jogo, i) in study.boletim.jogos" :key="i" class="bet">
              <div class="bet-balls">
                <span v-for="n in jogo.dezenas" :key="n" class="ball">{{ pad(n) }}</span>
              </div>
              <div class="bet-meta">
                <span :class="jogo.rateio_estimado < 1 ? 'good' : 'bad'">
                  rateio estimado {{ jogo.rateio_estimado }}×
                </span>
                <span>retorno esperado R$ {{ jogo.valor_esperado.retorno_esperado_ajustado.toFixed(2) }}</span>
              </div>
            </div>

            <ul class="notes">
              <li v-for="(n, i) in study.boletim.observacoes" :key="i">{{ n }}</li>
            </ul>
          </div>

          <!-- CERTIFICADO -->
          <div class="card">
            <div class="card-head">
              <h2>Garantia certificada</h2>
              <span class="card-meta">
                verificado contra {{ formatInt(study.certificado.sorteios_verificados) }} sorteios
              </span>
            </div>
            <p class="card-sub">
              Enumeração exaustiva de todos os sorteios possíveis — são teoremas
              sobre este bilhete, não estimativas.
            </p>
            <ul class="statements">
              <li v-for="(f, i) in study.certificado.frases_bilhete" :key="'t' + i">{{ f }}</li>
            </ul>
            <template v-if="study.boletim.modo === 'fechamento'">
              <h3 class="sub-title">Garantia condicional do fechamento</h3>
              <ul class="statements">
                <li v-for="(f, i) in study.certificado.frases" :key="'c' + i">{{ f }}</li>
              </ul>
            </template>
          </div>

          <!-- BACKTEST -->
          <div class="card" v-if="study.backtest">
            <div class="card-head">
              <h2>Competição dos mundos</h2>
              <span class="card-meta">
                concursos {{ study.backtest.concurso_inicial }}–{{ study.backtest.concurso_final }}
              </span>
            </div>
            <table class="rank-table">
              <thead>
                <tr>
                  <th>Mundo</th>
                  <th>Média de acertos</th>
                  <th>11+</th>
                  <th>12+</th>
                  <th>13+</th>
                  <th>Prêmio</th>
                  <th>ROI</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in study.backtest.ranking" :key="r.mundo" :class="{ control: r.mundo === 'uniforme' }">
                  <td>{{ r.nome }}</td>
                  <td class="num">{{ r.media_acertos.toFixed(3) }}</td>
                  <td class="num">{{ r.premios_por_faixa['11'] || 0 }}</td>
                  <td class="num">{{ r.premios_por_faixa['12'] || 0 }}</td>
                  <td class="num">{{ r.premios_por_faixa['13'] || 0 }}</td>
                  <td class="num">R$ {{ r.premio_total.toFixed(2) }}</td>
                  <td class="num" :class="r.roi >= 0 ? 'good' : 'bad'">{{ (r.roi * 100).toFixed(1) }}%</td>
                </tr>
              </tbody>
            </table>
            <p class="table-note">
              Valor teórico de acertos por jogo: {{ study.backtest.media_acertos_teorica.toFixed(2) }}
            </p>
            <ul class="verdicts">
              <li v-for="(v, i) in study.backtest.veredito" :key="i">{{ v }}</li>
            </ul>
          </div>

          <!-- POPULARIDADE -->
          <div class="card">
            <div class="card-head">
              <h2>Modelo de popularidade</h2>
              <span class="card-meta">
                {{ study.popularidade.heuristico ? 'heurístico' : `calibrado em ${study.popularidade.concursos_usados} concursos` }}
              </span>
            </div>
            <p class="card-sub">
              Ajustado por regressão de Poisson sobre arrecadação e nº de
              ganhadores publicados pela Caixa. É o que sustenta o mundo
              anti-popular.
            </p>
            <ul class="statements">
              <li v-for="(t, i) in study.popularidade.interpretacao" :key="i">{{ t }}</li>
            </ul>
            <ul class="notes">
              <li v-for="(t, i) in study.popularidade.observacoes" :key="i">{{ t }}</li>
            </ul>
          </div>

          <!-- VALOR ESPERADO -->
          <div class="card">
            <div class="card-head">
              <h2>Valor esperado</h2>
              <span class="card-meta">
                retorno de {{ (study.valor_esperado.taxa_de_retorno * 100).toFixed(1) }}%
              </span>
            </div>
            <table class="rank-table">
              <thead>
                <tr><th>Faixa</th><th>Probabilidade</th><th>Prêmio médio</th><th>Contribuição</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in expectedValueRows" :key="row.faixa">
                  <td>{{ row.faixa }} acertos</td>
                  <td class="num">1 em {{ formatInt(row.um_em) }}</td>
                  <td class="num">R$ {{ formatInt(Math.round(row.premio_medio)) }}</td>
                  <td class="num">R$ {{ row.contribuicao.toFixed(4) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- AVISOS -->
          <div class="card warning-card">
            <h2>Leia antes de apostar</h2>
            <ul class="statements">
              <li v-for="(a, i) in study.avisos" :key="i">{{ a }}</li>
            </ul>
          </div>
        </template>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import {
  listLotteries, listWorlds, getHistory,
  startStudy, getTask, getStudy
} from '../api/lottery'

const router = useRouter()

const lotteries = ref([])
const worlds = ref([])
const history = ref(null)
const study = ref(null)

const running = ref(false)
const progress = ref(0)
const progressMsg = ref('')
const error = ref('')

let pollTimer = null

const modes = [
  { value: 'valor_esperado', label: 'Valor esperado', hint: 'jogos nas regiões menos disputadas' },
  { value: 'fechamento', label: 'Fechamento', hint: 'garante prêmios secundários' },
  { value: 'diversificado', label: 'Diversificado', hint: 'divide entre os mundos' }
]

const config = ref({
  modalidade: 'lotofacil',
  n_jogos: 8,
  modo: 'valor_esperado',
  mundos: [],
  rodar_backtest: true,
  backtest_concursos: 60,
  fechamento_dezenas: 18,
  sincronizar: true
})

const currentLottery = computed(() =>
  lotteries.value.find(l => l.slug === config.value.modalidade)
)

const estimatedCost = computed(() => {
  const price = currentLottery.value?.preco_aposta || 0
  return (price * config.value.n_jogos).toFixed(2)
})

// As chaves do objeto vêm do backend em ordem decrescente, mas o JS reordena
// chaves numéricas em ordem crescente — daí a normalização explícita aqui.
const expectedValueRows = computed(() => {
  const tiers = study.value?.valor_esperado?.por_faixa || {}
  return Object.entries(tiers)
    .map(([faixa, info]) => ({ faixa: Number(faixa), ...info }))
    .sort((a, b) => b.faixa - a.faixa)
})

const statusClass = computed(() => {
  if (error.value) return 'error'
  if (running.value) return 'running'
  return study.value ? 'done' : 'idle'
})

const statusText = computed(() => {
  if (error.value) return 'Erro'
  if (running.value) return 'Processando'
  return study.value ? 'Concluído' : 'Pronto'
})

const pad = (n) => String(n).padStart(2, '0')
const formatInt = (n) => (n ?? 0).toLocaleString('pt-BR')
const tagLabel = (c) => ({
  controle: 'controle',
  valor_esperado: 'valor esperado',
  estatistico: 'a testar'
}[c] || c)

async function loadReference() {
  try {
    const [lots, ws] = await Promise.all([listLotteries(), listWorlds()])
    lotteries.value = lots.modalidades
    worlds.value = ws.mundos
    // Por padrão todos os mundos entram: o backtest só faz sentido com o
    // grupo de controle presente para comparação.
    config.value.mundos = ws.mundos.map(w => w.slug)
    await loadHistory()
  } catch (e) {
    error.value = `Falha ao carregar dados de referência: ${e.message}`
  }
}

async function loadHistory() {
  try {
    history.value = await getHistory(config.value.modalidade, 5)
  } catch (e) {
    history.value = null
  }
}

async function runStudy() {
  error.value = ''
  study.value = null
  running.value = true
  progress.value = 0
  progressMsg.value = 'iniciando…'

  try {
    const { task_id, run_id } = await startStudy(config.value)
    pollTimer = setInterval(async () => {
      try {
        const { task } = await getTask(task_id)
        progress.value = task.progress
        progressMsg.value = task.progress_detail?.mensagem || task.message

        if (task.status === 'completed') {
          stopPolling()
          const res = await getStudy(run_id)
          study.value = res.estudo
          running.value = false
          progressMsg.value = ''
        } else if (task.status === 'failed') {
          stopPolling()
          error.value = task.error || 'O estudo falhou'
          running.value = false
        }
      } catch (e) {
        stopPolling()
        error.value = `Falha ao acompanhar o estudo: ${e.message}`
        running.value = false
      }
    }, 2000)
  } catch (e) {
    error.value = e.message
    running.value = false
  }
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

onMounted(loadReference)
onUnmounted(stopPolling)
</script>

<style scoped>
.lottery-view {
  --black: #000;
  --white: #fff;
  --orange: #FF4500;
  --gray-light: #F5F5F5;
  --gray-text: #666;
  --border: #E5E5E5;
  --green: #0a7d34;
  min-height: 100vh;
  background: var(--white);
  color: var(--black);
  font-family: 'Space Grotesk', system-ui, sans-serif;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 28px;
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  background: var(--white);
  z-index: 10;
}

.brand {
  font-weight: 700;
  letter-spacing: 0.08em;
  cursor: pointer;
}

.brand-tag {
  margin-left: 8px;
  padding: 2px 8px;
  font-size: 11px;
  border-radius: 10px;
  background: var(--orange);
  color: var(--white);
  letter-spacing: 0.04em;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--gray-text);
}

.status-pill .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #bbb;
}

.status-pill.running .dot { background: var(--orange); animation: pulse 1.2s infinite; }
.status-pill.done .dot { background: var(--green); }
.status-pill.error .dot { background: #c0392b; }

@keyframes pulse { 50% { opacity: 0.3; } }

.content {
  display: grid;
  grid-template-columns: 330px 1fr;
  gap: 28px;
  padding: 28px;
  align-items: start;
}

/* ------------------------------- config ------------------------------- */
.config-panel {
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 20px;
  position: sticky;
  top: 88px;
}

.panel-title {
  font-size: 15px;
  margin: 0 0 18px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.field { display: block; margin-bottom: 18px; }

.field-label {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--gray-text);
  margin-bottom: 7px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.field-label em { font-style: normal; color: var(--orange); font-weight: 600; }

.field select,
.field input[type='range'] { width: 100%; }

.field select {
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 7px;
  font-family: inherit;
  background: var(--white);
}

.checkbox-field {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 13px;
}

.checkbox-field input { margin: 0; }

.mode-options { display: flex; flex-direction: column; gap: 7px; }

.mode-btn {
  text-align: left;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--white);
  cursor: pointer;
  font-family: inherit;
}

.mode-btn strong { display: block; font-size: 13px; }
.mode-btn small { color: var(--gray-text); font-size: 11px; }
.mode-btn.active { border-color: var(--orange); background: rgba(255, 69, 0, 0.05); }

.world-list { display: flex; flex-direction: column; gap: 5px; }

.world-item {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  cursor: pointer;
}

.world-name { flex: 1; }

.world-tag {
  font-size: 9.5px;
  padding: 2px 6px;
  border-radius: 8px;
  background: var(--gray-light);
  color: var(--gray-text);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  white-space: nowrap;
}

.world-tag.valor_esperado { background: rgba(10, 125, 52, 0.12); color: var(--green); }
.world-tag.controle { background: rgba(0, 0, 0, 0.08); }

.run-btn {
  width: 100%;
  padding: 12px;
  border: none;
  border-radius: 8px;
  background: var(--orange);
  color: var(--white);
  font-weight: 600;
  font-size: 14px;
  cursor: pointer;
  font-family: inherit;
}

.run-btn:disabled { background: #ccc; cursor: not-allowed; }

.progress { margin-top: 14px; }

.progress-bar {
  height: 4px;
  background: var(--gray-light);
  border-radius: 2px;
  overflow: hidden;
}

.progress-fill { height: 100%; background: var(--orange); transition: width 0.4s; }

.progress-msg {
  display: block;
  margin-top: 7px;
  font-size: 11px;
  color: var(--gray-text);
}

.error-box {
  margin-top: 14px;
  padding: 10px;
  border-radius: 7px;
  background: rgba(192, 57, 43, 0.08);
  color: #c0392b;
  font-size: 12px;
}

.history-box {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
  font-size: 11.5px;
  color: var(--gray-text);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* ------------------------------ resultados ------------------------------ */
.results { display: flex; flex-direction: column; gap: 20px; min-width: 0; }

.empty-state { padding: 50px 10px; max-width: 640px; }
.empty-state h1 { font-size: 30px; margin: 0 0 16px; }
.empty-state p { color: var(--gray-text); line-height: 1.65; margin-bottom: 14px; }

.disclaimer-inline {
  padding: 14px 16px;
  border-left: 3px solid var(--orange);
  background: var(--gray-light);
  border-radius: 0 7px 7px 0;
}

.card {
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 22px;
}

.card-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.card h2 { font-size: 17px; margin: 0; }
.card-meta { font-size: 12px; color: var(--gray-text); }

.card-sub {
  font-size: 12.5px;
  color: var(--gray-text);
  margin: 0 0 16px;
  line-height: 1.55;
}

.sub-title {
  font-size: 13px;
  margin: 20px 0 8px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--gray-text);
}

.bet {
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}

.bet-balls { display: flex; flex-wrap: wrap; gap: 5px; }

.ball {
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--black);
  color: var(--white);
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.bet-meta {
  display: flex;
  gap: 18px;
  margin-top: 8px;
  font-size: 11.5px;
  color: var(--gray-text);
  flex-wrap: wrap;
}

.good { color: var(--green); }
.bad { color: #c0392b; }

.notes, .statements, .verdicts {
  margin: 14px 0 0;
  padding-left: 18px;
  font-size: 12.5px;
  color: var(--gray-text);
  line-height: 1.65;
}

.statements { color: var(--black); }
.verdicts li { margin-bottom: 6px; }

.rank-table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 14px;
  font-size: 12.5px;
}

.rank-table th, .rank-table td {
  padding: 8px 6px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

.rank-table th {
  font-size: 10.5px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--gray-text);
  font-weight: 600;
}

.rank-table .num { text-align: right; font-variant-numeric: tabular-nums; }
.rank-table tr.control { background: var(--gray-light); font-style: italic; }

.table-note { font-size: 11.5px; color: var(--gray-text); margin: 10px 0 0; }

.warning-card { border-color: var(--orange); background: rgba(255, 69, 0, 0.035); }
.warning-card h2 { margin-bottom: 6px; }

@media (max-width: 900px) {
  .content { grid-template-columns: 1fr; }
  .config-panel { position: static; }
}
</style>
