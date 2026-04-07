<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let projects = []
  let enriched = []   // projects with agent/sim stats
  let name = '', description = ''
  let loading = false, error = ''
  let search = ''
  let cloneTarget = null, cloneName = ''

  onMount(loadProjects)

  async function loadProjects() {
    try {
      projects = await api.listProjects()
      enriched = projects.map(p => ({ ...p, _agents: 0, _polarization: null }))
      // Load agent count + sim history for each project asynchronously
      projects.forEach((p, i) => {
        api.listAgents(p.id).then(a => {
          enriched[i] = { ...enriched[i], _agents: a?.length || 0 }
          enriched = enriched
        }).catch(() => {})
        api.getSimHistory(p.id).then(h => {
          const last = h?.[0]
          if (last) {
            enriched[i] = {
              ...enriched[i],
              _lastSim: last.started_at,
              _actions: last.action_count,
              _platform: last.platform,
            }
            enriched = enriched
          }
        }).catch(() => {})
        api.getPredictions(p.id).then(d => {
          const cal = d?.calibration
          if (cal?.total_predictions > 0) {
            enriched[i] = { ...enriched[i], _predictions: cal.total_predictions }
            enriched = enriched
          }
        }).catch(() => {})
      })
    } catch(e) { error = e.message }
  }

  async function createProject() {
    if (!name.trim()) return
    loading = true; error = ''
    try {
      const p = await api.createProject(name.trim(), description.trim())
      projects = [p, ...projects]
      enriched = [{ ...p, _agents: 0 }, ...enriched]
      name = ''; description = ''
    } catch(e) { error = e.message }
    loading = false
  }

  async function deleteProject(id, ev) {
    ev.stopPropagation()
    if (!confirm('Excluir este projeto e todos os seus dados?')) return
    try {
      await api.deleteProject(id)
      projects = projects.filter(p => p.id !== id)
      enriched = enriched.filter(p => p.id !== id)
    } catch(e) { error = e.message }
  }

  async function cloneProject(p, ev) {
    ev.stopPropagation()
    cloneTarget = p
    cloneName = p.name + ' (clone)'
  }

  async function confirmClone() {
    if (!cloneTarget) return
    try {
      const result = await api.cloneProject(cloneTarget.id, cloneName)
      await loadProjects()
      cloneTarget = null
    } catch(e) { error = e.message }
  }

  function openProject(p) {
    $currentProject = p
    $currentStep = 1
  }

  function polarizationBadge(pol) {
    if (pol === null || pol === undefined) return null
    if (pol < 0.3) return { label: `${(pol*100).toFixed(0)}%`, cls: 'pol-low' }
    if (pol < 0.6) return { label: `${(pol*100).toFixed(0)}%`, cls: 'pol-med' }
    return { label: `${(pol*100).toFixed(0)}%`, cls: 'pol-high' }
  }

  $: filtered = enriched.filter(p =>
    !search || p.name?.toLowerCase().includes(search.toLowerCase()) ||
    p.description?.toLowerCase().includes(search.toLowerCase())
  )
</script>

<style>
  h1 { font-size: 1.5rem; font-weight: 700; margin-bottom: 4px; color: #f1f5f9; }
  .subtitle { color: #64748b; margin-bottom: 28px; font-size: 0.9rem; }

  .top-bar { display: flex; gap: 12px; align-items: center; margin-bottom: 20px; flex-wrap: wrap; }
  .search-input {
    flex: 1; min-width: 200px;
    background: #1e293b; border: 1px solid #334155; color: #e2e8f0;
    padding: 8px 14px; border-radius: 8px; font-size: 0.9rem; outline: none;
  }
  .search-input:focus { border-color: #38bdf8; }

  .create-form {
    background: #1e293b; border: 1px solid #334155;
    border-radius: 12px; padding: 20px; margin-bottom: 28px;
  }
  .create-form h2 { font-size: 0.95rem; margin-bottom: 14px; color: #94a3b8; }
  .row { display: flex; gap: 10px; align-items: flex-end; flex-wrap: wrap; }
  .field { flex: 1; min-width: 160px; }
  label { display: block; margin-bottom: 4px; font-size: 0.78rem; color: #64748b; }
  input {
    background: #0f172a; border: 1px solid #334155; color: #e2e8f0;
    padding: 8px 12px; border-radius: 8px; font-size: 0.9rem; width: 100%; outline: none;
  }
  input:focus { border-color: #38bdf8; }

  button {
    background: #38bdf8; color: #0f172a; border: none;
    padding: 9px 20px; border-radius: 8px; font-weight: 600;
    cursor: pointer; font-size: 0.875rem; white-space: nowrap;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }

  /* Project card grid */
  .projects-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 14px;
  }
  .project-card {
    background: #1e293b; border: 1px solid #334155; border-radius: 12px;
    padding: 16px; cursor: pointer;
    transition: border-color 0.15s, transform 0.1s;
    display: flex; flex-direction: column; gap: 10px;
  }
  .project-card:hover { border-color: #38bdf8; transform: translateY(-2px); }

  .card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
  .card-name { font-size: 1rem; color: #f1f5f9; font-weight: 600; }
  .card-desc { font-size: 0.78rem; color: #64748b; }
  .card-date { font-size: 0.72rem; color: #475569; }

  .card-stats { display: flex; gap: 8px; flex-wrap: wrap; }
  .stat-pill {
    font-size: 0.72rem; padding: 0.15rem 0.5rem; border-radius: 999px;
    background: #0f172a; color: #64748b; border: 1px solid #334155;
  }
  .stat-pill.agents { color: #38bdf8; border-color: #1e3a5f; }
  .stat-pill.actions { color: #a78bfa; border-color: #2e1b5f; }
  .pol-low { color: #22c55e; border-color: #14532d; }
  .pol-med { color: #f59e0b; border-color: #451a03; }
  .pol-high { color: #ef4444; border-color: #7f1d1d; }

  .card-actions { display: flex; gap: 6px; }
  .action-btn {
    background: #0f172a; color: #94a3b8; border: 1px solid #334155;
    padding: 4px 10px; border-radius: 6px; font-size: 0.75rem;
    cursor: pointer; transition: all 0.15s;
  }
  .action-btn:hover { border-color: #38bdf8; color: #38bdf8; }
  .action-btn.del { color: #ef4444; }
  .action-btn.del:hover { background: #7f1d1d; border-color: #ef4444; color: white; }
  .action-btn.open { background: #38bdf8; color: #0f172a; border-color: #38bdf8; font-weight: 600; }
  .action-btn.open:hover { background: #7dd3fc; }

  /* Clone modal */
  .modal-overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.7);
    display: flex; align-items: center; justify-content: center; z-index: 100;
  }
  .modal {
    background: #1e293b; border: 1px solid #334155; border-radius: 12px;
    padding: 24px; width: 400px; max-width: 90vw; display: flex; flex-direction: column; gap: 16px;
  }
  .modal h3 { font-size: 1rem; color: #e2e8f0; margin: 0; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; }
  .btn-cancel { background: #334155; color: #94a3b8; }
  .btn-cancel:hover { background: #475569; }

  .error { color: #ef4444; margin-bottom: 10px; font-size: 0.85rem; }
  .empty {
    color: #475569; text-align: center; padding: 60px 20px;
    border: 2px dashed #334155; border-radius: 12px;
  }
  .empty p { margin: 0 0 12px; }
  .empty .cta { background: #38bdf8; color: #0f172a; border: none; cursor: pointer; padding: 10px 24px; border-radius: 8px; font-weight: 600; font-size: 0.9rem; }
  .empty .cta:hover { background: #7dd3fc; }
</style>

<h1>🐟 PicoFish</h1>
<p class="subtitle">Simulação social multi-agente — roda até no Raspberry Pi</p>

{#if error}<p class="error">⚠ {error}</p>{/if}

<!-- Create form -->
<div class="create-form">
  <h2>+ Novo Projeto</h2>
  <div class="row">
    <div class="field">
      <label>Nome *</label>
      <input bind:value={name} placeholder="ex: Regulação de IA no Brasil" on:keydown={e => e.key === 'Enter' && createProject()} />
    </div>
    <div class="field">
      <label>Descrição</label>
      <input bind:value={description} placeholder="Descrição opcional" />
    </div>
    <button on:click={createProject} disabled={loading || !name.trim()}>
      {loading ? '...' : 'Criar'}
    </button>
  </div>
</div>

<!-- Search bar -->
{#if projects.length > 0}
  <div class="top-bar">
    <input class="search-input" bind:value={search} placeholder="🔍 Buscar projetos..." />
    <span style="color: #475569; font-size: 0.8rem">{filtered.length} projeto{filtered.length !== 1 ? 's' : ''}</span>
  </div>
{/if}

{#if filtered.length === 0 && projects.length === 0}
  <div class="empty">
    <p>Nenhum projeto ainda.</p>
    <p style="font-size: 0.85rem; color: #475569; margin-bottom: 20px">Crie seu primeiro projeto acima ou use um dos cenários de exemplo.</p>
  </div>
{:else if filtered.length === 0}
  <div class="empty">
    <p>Nenhum projeto encontrado para "{search}"</p>
  </div>
{:else}
  <div class="projects-grid">
    {#each filtered as p}
      <div class="project-card" on:click={() => openProject(p)}>
        <div class="card-header">
          <div>
            <div class="card-name">{p.name}</div>
            {#if p.description}<div class="card-desc">{p.description}</div>{/if}
          </div>
        </div>

        <!-- Stats row -->
        <div class="card-stats">
          {#if p._agents > 0}
            <span class="stat-pill agents">👤 {p._agents} agentes</span>
          {/if}
          {#if p._actions > 0}
            <span class="stat-pill actions">⚡ {p._actions} ações</span>
          {/if}
          {#if p._predictions > 0}
            <span class="stat-pill">🎯 {p._predictions} previsões</span>
          {/if}
          {#if p._lastSim}
            <span class="stat-pill" title={p._lastSim}>
              🕐 {new Date(p._lastSim).toLocaleDateString('pt-BR')}
            </span>
          {/if}
        </div>

        <div class="card-date">{p.created_at?.slice(0,10) || ''}</div>

        <!-- Quick actions -->
        <div class="card-actions" on:click|stopPropagation>
          <button class="action-btn open" on:click={() => openProject(p)}>Abrir →</button>
          <button class="action-btn" on:click={ev => cloneProject(p, ev)}>⎘ Clonar</button>
          <button class="action-btn del" on:click={ev => deleteProject(p.id, ev)}>✕</button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Clone modal -->
{#if cloneTarget}
  <div class="modal-overlay" on:click={() => cloneTarget = null}>
    <div class="modal" on:click|stopPropagation>
      <h3>Clonar "{cloneTarget.name}"</h3>
      <div>
        <label>Nome do clone</label>
        <input bind:value={cloneName} />
      </div>
      <p style="font-size: 0.78rem; color: #64748b">Copia grafo + agentes. Não copia simulação ou relatórios.</p>
      <div class="modal-actions">
        <button class="action-btn btn-cancel" on:click={() => cloneTarget = null}>Cancelar</button>
        <button on:click={confirmClone}>Clonar</button>
      </div>
    </div>
  </div>
{/if}
