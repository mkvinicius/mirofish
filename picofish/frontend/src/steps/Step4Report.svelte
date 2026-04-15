<script>
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep, simRequirement } from '../stores/project.js'
  import PredictionTracker from '../PredictionTracker.svelte'

  let status = null
  let error = ''
  let scenarioInput = $simRequirement || ''
  let eventSource = null
  let showProgress = true

  onMount(async () => {
    status = await api.getReport($currentProject.id).catch(() => null)
    if (status?.status === 'generating' || status?.status === 'planning') {
      startSSE()
    }
  })

  onDestroy(() => closeSSE())

  function closeSSE() {
    if (eventSource) { eventSource.close(); eventSource = null }
  }

  function startSSE() {
    closeSSE()
    eventSource = new EventSource(api.reportStreamUrl($currentProject.id))
    eventSource.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data)
        status = data
        if (data.status === 'completed' || data.status === 'error') {
          closeSSE()
        }
      } catch {}
    }
    eventSource.onerror = () => closeSSE()
  }

  async function generate() {
    error = ''
    const req = scenarioInput.trim() || $simRequirement || 'Análise de simulação social'
    simRequirement.set(req)
    try {
      status = await api.generateReport($currentProject.id, req)
      startSSE()
    } catch(e) { error = e.message }
  }

  function exportReport() {
    window.open(api.exportReportUrl($currentProject.id), '_blank')
  }

  function renderMarkdown(md) {
    if (!md) return ''
    return md
      .replace(/^# (.+)$/gm, '<h1>$1</h1>')
      .replace(/^## (.+)$/gm, '<h2>$1</h2>')
      .replace(/^### (.+)$/gm, '<h3>$1</h3>')
      .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
      .replace(/\*(.+?)\*/g, '<em>$1</em>')
      .replace(/^---$/gm, '<hr>')
      .replace(/^> (.+)$/gm, '<blockquote>$1</blockquote>')
      .replace(/^- (.+)$/gm, '<li>$1</li>')
      .replace(/(<li>.*<\/li>)/gs, '<ul>$1</ul>')
      .replace(/\n\n/g, '</p><p>')
      .replace(/^/, '<p>').replace(/$/, '</p>')
  }
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 20px; }

  label { display: block; font-size: 0.8rem; color: #64748b; margin-bottom: 4px; }
  input {
    width: 100%;
    background: #0f172a;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 9px 12px;
    border-radius: 8px;
    font-size: 0.9rem;
    outline: none;
    margin-bottom: 16px;
  }
  input:focus { border-color: #38bdf8; }

  .btn-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

  button {
    background: #38bdf8;
    color: #0f172a;
    border: none;
    padding: 10px 24px;
    border-radius: 8px;
    font-weight: 600;
    cursor: pointer;
    font-size: 0.9rem;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  button.secondary {
    background: transparent;
    border: 1px solid #334155;
    color: #94a3b8;
    font-size: 0.8rem;
    padding: 7px 14px;
  }
  button.secondary:hover { border-color: #38bdf8; color: #38bdf8; }
  button.export {
    background: #14532d;
    color: #22c55e;
    font-size: 0.8rem;
    padding: 7px 14px;
  }
  button.export:hover { background: #166534; }

  .error { color: #ef4444; font-size: 0.85rem; margin-bottom: 12px; }

  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: 20px;
    font-size: 0.8rem;
    font-weight: 600;
  }
  .planning { background: #2d1b69; color: #a78bfa; }
  .generating { background: #0c4a6e; color: #38bdf8; }
  .completed { background: #14532d; color: #22c55e; }
  .err { background: #431407; color: #f97316; }

  .outline {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 10px;
    padding: 14px;
    margin: 12px 0;
  }
  .outline-title { font-weight: 600; color: #f1f5f9; margin-bottom: 6px; font-size: 0.9rem; }
  .section-list { list-style: none; padding: 0; margin: 0; }
  .section-list li { font-size: 0.82rem; color: #64748b; padding: 2px 0; }
  .section-list li::before { content: "• "; color: #38bdf8; }

  .spinner {
    display: inline-block;
    width: 10px; height: 10px;
    border: 2px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .report-body {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 12px;
    padding: 24px;
    margin-top: 16px;
    line-height: 1.7;
    font-size: 0.875rem;
    color: #cbd5e1;
  }
  :global(.report-body h1) { font-size: 1.3rem; color: #f1f5f9; margin: 0 0 8px; }
  :global(.report-body h2) { font-size: 1.1rem; color: #38bdf8; margin: 24px 0 8px; }
  :global(.report-body h3) { font-size: 0.95rem; color: #7dd3fc; margin: 16px 0 6px; }
  :global(.report-body p) { margin-bottom: 12px; }
  :global(.report-body strong) { color: #f1f5f9; }
  :global(.report-body em) { color: #94a3b8; }
  :global(.report-body hr) { border: none; border-top: 1px solid #334155; margin: 20px 0; }
  :global(.report-body blockquote) { border-left: 3px solid #38bdf8; padding-left: 12px; color: #94a3b8; margin: 8px 0; }
  :global(.report-body ul) { padding-left: 20px; margin-bottom: 12px; }
  :global(.report-body li) { margin-bottom: 4px; }

  .next-btn { margin-top: 24px; display: flex; justify-content: flex-end; }

  .prediction-section {
    margin-top: 2rem;
    padding-top: 1.5rem;
    border-top: 1px solid #334155;
  }

  .progress-log {
    background: #0f172a; border: 1px solid #334155; border-radius: 10px;
    margin: 14px 0; overflow: hidden;
  }
  .progress-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 8px 14px; cursor: pointer; border-bottom: 1px solid #334155;
  }
  .progress-header:hover { background: #1e293b; }
  .progress-title { font-size: 0.8rem; color: #64748b; font-weight: 600; }
  .progress-entries { padding: 10px 14px; max-height: 220px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
  .progress-entry { font-size: 0.78rem; color: #94a3b8; line-height: 1.4; }
  .progress-entry.last { color: #38bdf8; }

  .step-badge {
    font-size: 0.72rem; padding: 1px 8px; border-radius: 10px;
    background: #1e293b; color: #64748b; margin-left: 8px;
  }
</style>

<h2>Passo 4 — Gerar Relatório</h2>
<p class="desc">O agente ReACT analisa a simulação usando InsightForge, PanoramaSearch, QuickSearch e InterviewAgents — transmitindo o progresso em tempo real.</p>

{#if error}<p class="error">{error}</p>{/if}

{#if !status || status.status === 'not_found' || !status.status}
  <label>Cenário (requisito da simulação)</label>
  <input bind:value={scenarioInput} placeholder="ex: Governo anuncia regulação de IA..." />
  <button on:click={generate}>Gerar Relatório</button>

{:else if status.status === 'planning'}
  <div class="btn-row">
    <span class="status-pill planning"><span class="spinner"></span> Planejando estrutura...</span>
    {#if status.current_step}<span class="step-badge">{status.current_step}</span>{/if}
  </div>
  {#if status.progress?.length > 0}
    <div class="progress-log">
      <div class="progress-header" on:click={() => showProgress = !showProgress}>
        <span class="progress-title">📋 Log ({status.progress.length})</span>
        <span style="font-size: 0.75rem; color: #475569">{showProgress ? '▲' : '▼'}</span>
      </div>
      {#if showProgress}
        <div class="progress-entries">
          {#each status.progress as entry, i}
            <div class="progress-entry" class:last={i === status.progress.length - 1}>{entry}</div>
          {/each}
        </div>
      {/if}
    </div>
  {:else}
    <p style="color: #64748b; font-size: 0.85rem; margin-top: 12px">Analisando cenário e estruturando as seções do relatório.</p>
  {/if}

{:else if status.status === 'generating'}
  <div class="btn-row">
    <span class="status-pill generating"><span class="spinner"></span> Gerando...</span>
    {#if status.current_step}<span class="step-badge">{status.current_step}</span>{/if}
  </div>
  {#if status.progress?.length > 0}
    <div class="progress-log">
      <div class="progress-header" on:click={() => showProgress = !showProgress}>
        <span class="progress-title">📋 Log ({status.progress.length})</span>
        <span style="font-size: 0.75rem; color: #475569">{showProgress ? '▲' : '▼'}</span>
      </div>
      {#if showProgress}
        <div class="progress-entries">
          {#each status.progress as entry, i}
            <div class="progress-entry" class:last={i === status.progress.length - 1}>{entry}</div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
  {#if status.outline}
    <div class="outline">
      <div class="outline-title">{status.outline.title}</div>
      <ul class="section-list">
        {#each status.outline.sections || [] as sec}
          <li>{sec.title}</li>
        {/each}
      </ul>
    </div>
  {/if}
  {#if !status.progress?.length}
    <p style="color: #64748b; font-size: 0.85rem">Agente ReACT coletando evidências — atualizações em tempo real.</p>
  {/if}

{:else if status.status === 'completed'}
  <div class="btn-row">
    <span class="status-pill completed">✓ Relatório pronto</span>
    <button class="secondary" on:click={generate}>Regenerar</button>
    <button class="export" on:click={exportReport}>↓ Exportar .md</button>
  </div>
  {#if status.progress?.length > 0}
    <div class="progress-log">
      <div class="progress-header" on:click={() => showProgress = !showProgress}>
        <span class="progress-title">📋 Log de geração ({status.progress.length} passos)</span>
        <span style="font-size: 0.75rem; color: #475569">{showProgress ? '▲' : '▼'}</span>
      </div>
      {#if showProgress}
        <div class="progress-entries">
          {#each status.progress as entry}
            <div class="progress-entry">{entry}</div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
  <div class="report-body">{@html renderMarkdown(status.content)}</div>

  <!-- Phase 2: Prediction Tracker -->
  <div class="prediction-section">
    <PredictionTracker projectId={$currentProject.id} />
  </div>

  <div class="next-btn">
    <button on:click={() => $currentStep = 5}>Próximo: Interação Profunda →</button>
  </div>

{:else if status.status === 'error'}
  <span class="status-pill err">Erro</span>
  <p style="color: #ef4444; font-size: 0.85rem; margin-top: 8px">{status.error || status.content}</p>
  <label style="margin-top: 16px">Cenário</label>
  <input bind:value={scenarioInput} placeholder="Descreva o cenário..." />
  <button style="margin-top: 8px" on:click={generate}>Tentar novamente</button>

{:else}
  <label>Cenário</label>
  <input bind:value={scenarioInput} placeholder="Descreva o cenário..." />
  <button on:click={generate}>Gerar Relatório</button>
{/if}
