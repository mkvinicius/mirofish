<script>
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep, simRequirement } from '../stores/project.js'

  let topic = $simRequirement || ''
  let totalHours = 24
  let platform = 'both'
  let status = null
  let actions = []
  let liveActions = []  // SSE live feed
  let history = []
  let error = ''
  let interval = null
  let showHistory = false
  let eventSource = null
  let liveActionList = null  // bind for auto-scroll

  onMount(async () => {
    await refresh()
    if (status?.status === 'running') {
      openFeed()
    } else if (status?.status === 'completed') {
      actions = await api.getSimulationActions($currentProject.id).catch(() => [])
    }
    history = await api.getSimHistory($currentProject.id).catch(() => [])
  })

  onDestroy(() => {
    if (interval) clearInterval(interval)
    closeFeed()
  })

  function openFeed() {
    closeFeed()
    eventSource = new EventSource(api.simFeedUrl($currentProject.id))
    eventSource.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data)
        if (ev.type === 'action' && ev.action) {
          liveActions = [ev.action, ...liveActions].slice(0, 200)
          if (status) status = { ...status, action_count: (status.action_count || 0) + 1 }
        } else if (ev.type === 'hour') {
          if (status) status = { ...status, current_hour: ev.hour ?? status.current_hour }
        } else if (ev.type === 'done') {
          closeFeed()
          status = { ...status, status: 'completed' }
          api.getSimulationActions($currentProject.id).then(a => actions = a).catch(() => {})
        } else if (ev.type === 'error') {
          closeFeed()
        }
      } catch {}
    }
    eventSource.onerror = () => closeFeed()
  }

  function closeFeed() {
    if (eventSource) { eventSource.close(); eventSource = null }
  }

  async function refresh() {
    try {
      status = await api.getSimulationStatus($currentProject.id)
    } catch {}
  }

  async function start() {
    if (!topic.trim()) return
    error = ''
    simRequirement.set(topic)
    try {
      await api.startSimulation($currentProject.id, totalHours, platform, topic)
      status = { status: 'running', current_hour: 0, total_hours: totalHours, agent_count: 0, action_count: 0 }
      liveActions = []
      actions = []
      openFeed()
      if (!interval) interval = setInterval(refresh, 10000)
    } catch(e) { error = e.message }
  }

  async function stop() {
    try {
      await api.stopSimulation($currentProject.id)
      if (interval) { clearInterval(interval); interval = null }
      closeFeed()
      await refresh()
    } catch(e) { error = e.message }
  }

  $: if (status?.status === 'completed' || status?.status === 'stopped') {
    if (interval) { clearInterval(interval); interval = null }
  }

  // Auto-scroll live feed to top when new actions arrive
  $: if (liveActions.length && liveActionList) {
    liveActionList.scrollTop = 0
  }

  function actionColor(type) {
    const colors = {
      'CREATE_POST': '#38bdf8',
      'COMMENT': '#a78bfa',
      'REPLY_TO_POST': '#a78bfa',
      'LIKE_POST': '#22c55e',
      'UPVOTE': '#22c55e',
      'REPOST': '#f59e0b',
      'SHARE': '#f59e0b',
    }
    return colors[type] || '#64748b'
  }

  function platformIcon(p) {
    const icons = {
      twitter: '𝕏', reddit: '🤖', instagram: '📸',
      tiktok: '🎵', whatsapp: '💬', facebook: '👥'
    }
    return icons[p] || '🌐'
  }

  function progressPct(s) {
    if (!s || !s.total_hours || s.total_hours === 0) return 0
    return Math.round((s.current_hour / s.total_hours) * 100)
  }
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 24px; }

  .config-row {
    display: flex;
    gap: 12px;
    align-items: flex-end;
    margin-bottom: 20px;
    flex-wrap: wrap;
  }
  .field { flex: 1; min-width: 200px; }
  label { display: block; font-size: 0.8rem; color: #64748b; margin-bottom: 4px; }
  input, select {
    width: 100%;
    background: #0f172a;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 9px 12px;
    border-radius: 8px;
    font-size: 0.9rem;
    outline: none;
  }
  input:focus, select:focus { border-color: #38bdf8; }
  .narrow { flex: 0 0 auto; min-width: 0; }
  .narrow input, .narrow select { min-width: 80px; }

  button {
    background: #38bdf8;
    color: #0f172a;
    border: none;
    padding: 10px 20px;
    border-radius: 8px;
    font-weight: 600;
    cursor: pointer;
    font-size: 0.9rem;
    white-space: nowrap;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  button.stop { background: #ef4444; color: white; }
  button.stop:hover { background: #f87171; }

  .error { color: #ef4444; font-size: 0.85rem; margin-bottom: 12px; }

  .status-bar {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 10px;
    padding: 16px;
    margin-bottom: 20px;
  }
  .status-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
  .status-label {
    font-size: 0.85rem;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 4px;
  }
  .running { background: #0c4a6e; color: #38bdf8; }
  .completed { background: #14532d; color: #22c55e; }
  .stopped { background: #431407; color: #f97316; }

  .progress-track {
    height: 6px;
    background: #0f172a;
    border-radius: 3px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: #38bdf8;
    border-radius: 3px;
    transition: width 0.5s;
  }
  .progress-text { font-size: 0.75rem; color: #64748b; margin-top: 4px; }

  .stats { display: flex; gap: 20px; margin-top: 12px; }
  .stat { font-size: 0.8rem; color: #64748b; }
  .stat span { color: #38bdf8; font-weight: 600; }

  .actions-feed h3 { font-size: 0.9rem; color: #64748b; margin-bottom: 12px; }
  .action-list { display: flex; flex-direction: column; gap: 8px; max-height: 400px; overflow-y: auto; }
  .live-dot {
    display: inline-block; width: 7px; height: 7px;
    background: #22c55e; border-radius: 50%; margin-right: 4px;
    animation: pulse 1.2s ease-in-out infinite;
  }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
  .action-item {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 8px;
    padding: 10px 12px;
  }
  .action-header { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .action-type {
    font-size: 0.7rem;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 3px;
    background: #0f172a;
  }
  .action-agent { font-size: 0.82rem; font-weight: 600; color: #f1f5f9; }
  .action-platform { font-size: 0.75rem; }
  .action-content { font-size: 0.82rem; color: #94a3b8; line-height: 1.4; }

  .next-btn { margin-top: 24px; display: flex; justify-content: flex-end; }

  button.history-toggle {
    background: transparent;
    border: 1px solid #334155;
    color: #64748b;
    font-size: 0.8rem;
    padding: 6px 12px;
    margin-bottom: 10px;
  }
  button.history-toggle:hover { border-color: #38bdf8; color: #38bdf8; background: transparent; }

  .history-list { display: flex; flex-direction: column; gap: 6px; }
  .history-item {
    background: #1e293b;
    border: 1px solid #1e3a4c;
    border-radius: 8px;
    padding: 10px 14px;
  }
  .history-header { display: flex; align-items: center; gap: 8px; }
  .history-platform { font-size: 0.85rem; }
  .history-topic { font-size: 0.82rem; color: #cbd5e1; flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .history-meta { font-size: 0.75rem; color: #38bdf8; white-space: nowrap; }
  .history-time { font-size: 0.72rem; color: #475569; margin-top: 3px; }
</style>

<h2>Passo 3 — Rodar Simulação</h2>
<p class="desc">Agentes interagem no Twitter/Reddit simulado ao longo de horas — produzindo dinâmicas sociais orgânicas com algoritmos de feed, câmaras de eco e memória comportamental.</p>

{#if error}<p class="error">{error}</p>{/if}

<div class="config-row">
  <div class="field" style="flex: 3">
    <label>Cenário / Requisito da Simulação *</label>
    <input bind:value={topic} placeholder="ex: Governo anuncia regulação severa de IA no Brasil..." />
  </div>
  <div class="field narrow">
    <label>Horas</label>
    <input type="number" bind:value={totalHours} min="1" max="168" style="min-width: 72px" />
  </div>
  <div class="field narrow">
    <label>Plataforma</label>
    <select bind:value={platform}>
      <option value="both">X (Twitter) + Reddit</option>
      <option value="all">Todas as plataformas</option>
      <option value="twitter">X (Twitter)</option>
      <option value="reddit">Reddit</option>
      <option value="instagram">Instagram</option>
      <option value="tiktok">TikTok</option>
      <option value="whatsapp">WhatsApp</option>
      <option value="facebook">Facebook</option>
    </select>
  </div>
  {#if status?.status !== 'running'}
    <button on:click={start} disabled={!topic.trim()}>▶ Iniciar</button>
  {:else}
    <button class="stop" on:click={stop}>⏹ Parar</button>
  {/if}
</div>

{#if status && status.status !== 'not_started'}
  <div class="status-bar">
    <div class="status-header">
      <span class="status-label {status.status}">
        {status.status === 'running' ? 'RODANDO' : status.status === 'completed' ? 'CONCLUÍDO' : 'PARADO'}
      </span>
      {#if status.status === 'running'}
        <span style="font-size: 0.8rem; color: #64748b">atualizando...</span>
      {/if}
    </div>
    {#if status.total_hours > 0}
      <div class="progress-track">
        <div class="progress-fill" style="width:{progressPct(status)}%"></div>
      </div>
      <p class="progress-text">Hora {status.current_hour} / {status.total_hours}</p>
    {/if}
    <div class="stats">
      <div class="stat">Agentes: <span>{status.agent_count || 0}</span></div>
      <div class="stat">Ações: <span>{status.action_count || 0}</span></div>
      {#if status.platform}<div class="stat">Plataforma: <span>{status.platform}</span></div>{/if}
    </div>
  </div>
{/if}

{#if status?.status === 'running' && liveActions.length > 0}
  <div class="actions-feed">
    <h3><span class="live-dot"></span>Feed ao Vivo ({liveActions.length} ações)</h3>
    <div class="action-list" bind:this={liveActionList}>
      {#each liveActions as a}
        <div class="action-item">
          <div class="action-header">
            <span class="action-platform">{platformIcon(a.platform)}</span>
            <span class="action-agent">{a.agent_name}</span>
            <span class="action-type" style="color:{actionColor(a.action_type)}">{a.action_type}</span>
            <span style="font-size: 0.7rem; color: #475569; margin-left: auto">H{a.sim_hour}</span>
          </div>
          {#if a.content}
            <p class="action-content">{a.content}</p>
          {/if}
        </div>
      {/each}
    </div>
  </div>
{:else if actions.length > 0}
  <div class="actions-feed">
    <h3>Ações da Simulação ({actions.length})</h3>
    <div class="action-list">
      {#each actions as a}
        <div class="action-item">
          <div class="action-header">
            <span class="action-platform">{platformIcon(a.platform)}</span>
            <span class="action-agent">{a.agent_name}</span>
            <span class="action-type" style="color:{actionColor(a.action_type)}">{a.action_type}</span>
            <span style="font-size: 0.7rem; color: #475569; margin-left: auto">H{a.sim_hour}</span>
          </div>
          {#if a.content}
            <p class="action-content">{a.content}</p>
          {/if}
        </div>
      {/each}
    </div>
  </div>
{/if}

{#if status?.status === 'completed'}
  <div class="next-btn">
    <button on:click={() => $currentStep = 4}>Próximo: Gerar Relatório →</button>
  </div>
{/if}

{#if history.length > 0}
  <div style="margin-top: 32px">
    <button class="history-toggle" on:click={() => showHistory = !showHistory}>
      {showHistory ? '▲' : '▼'} Simulações Anteriores ({history.length})
    </button>
    {#if showHistory}
      <div class="history-list">
        {#each history as h}
          <div class="history-item">
            <div class="history-header">
              <span class="history-platform">{platformIcon(h.platform)}</span>
              <span class="history-topic">{h.topic}</span>
              <span class="history-meta">{h.total_hours}h · {h.action_count} ações</span>
            </div>
            <div class="history-time">{h.started_at ? new Date(h.started_at).toLocaleString('pt-BR') : ''}</div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}
