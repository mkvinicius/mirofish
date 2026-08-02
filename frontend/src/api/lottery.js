import service, { requestWithRetry } from './index'

/** Modalidades disponíveis (Lotofácil, Quina, Mega-Sena). */
export function listLotteries() {
  return requestWithRetry(() => service({ url: '/api/lottery/modalidades', method: 'get' }))
}

/** Mundos paralelos disponíveis, com hipótese e ressalva de cada um. */
export function listWorlds() {
  return requestWithRetry(() => service({ url: '/api/lottery/mundos', method: 'get' }))
}

/** Resumo do histórico em cache + últimos concursos. */
export function getHistory(slug, limit = 10) {
  return requestWithRetry(() =>
    service({ url: `/api/lottery/historico/${slug}`, method: 'get', params: { limit } })
  )
}

/** Dispara o download dos concursos que faltam no cache. */
export function syncHistory(modalidade) {
  return service({ url: '/api/lottery/sync', method: 'post', data: { modalidade } })
}

/** Estatísticas, modelo de popularidade e valor esperado. */
export function getAnalysis(slug, janela) {
  return requestWithRetry(() =>
    service({ url: `/api/lottery/analise/${slug}`, method: 'get', params: janela ? { janela } : {} })
  )
}

/** Inicia um estudo completo (roda em background). */
export function startStudy(config) {
  return service({ url: '/api/lottery/estudo', method: 'post', data: config })
}

/** Progresso de uma tarefa (sync ou estudo). */
export function getTask(taskId) {
  return service({ url: `/api/lottery/task/${taskId}`, method: 'get' })
}

/** Boletim completo de um estudo concluído. */
export function getStudy(runId) {
  return requestWithRetry(() => service({ url: `/api/lottery/estudo/${runId}`, method: 'get' }))
}

/** Estudos já executados. */
export function listStudies(limit = 50) {
  return requestWithRetry(() =>
    service({ url: '/api/lottery/estudos', method: 'get', params: { limit } })
  )
}

export function deleteStudy(runId) {
  return service({ url: `/api/lottery/estudo/${runId}`, method: 'delete' })
}

/** Fechamento sob medida, com garantia certificada por enumeração exata. */
export function buildWheel(data) {
  return requestWithRetry(() => service({ url: '/api/lottery/fechamento', method: 'post', data }))
}

/** Confere jogos contra um concurso já realizado. */
export function checkGames(data) {
  return requestWithRetry(() => service({ url: '/api/lottery/conferir', method: 'post', data }))
}

/** Avaliação econômica do próximo concurso de cada modalidade. */
export function getOpportunities() {
  return requestWithRetry(() => service({ url: '/api/lottery/oportunidades', method: 'get' }))
}

/** Teste formal de viés físico da modalidade (Monte Carlo, cacheado). */
export function getBias(slug) {
  return requestWithRetry(() =>
    service({ url: `/api/lottery/vies/${slug}`, method: 'get', timeout: 600000 })
  )
}

/** Caderneta: lista apostas conferidas. atualizar=true baixa sorteios novos antes. */
export function listMyBets(atualizar = false) {
  return service({
    url: '/api/lottery/apostas', method: 'get',
    params: atualizar ? { atualizar: 1 } : {}
  })
}

/** Registra na caderneta os jogos apostados na Caixa. */
export function addMyBet(data) {
  return service({ url: '/api/lottery/apostas', method: 'post', data })
}

export function deleteMyBet(betId) {
  return service({ url: `/api/lottery/apostas/${betId}`, method: 'delete' })
}
