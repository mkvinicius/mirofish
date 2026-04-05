<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let projects = []
  let name = '', description = ''
  let loading = false, error = ''

  onMount(loadProjects)

  async function loadProjects() {
    try { projects = await api.listProjects() }
    catch(e) { error = e.message }
  }

  async function createProject() {
    if (!name.trim()) return
    loading = true; error = ''
    try {
      const p = await api.createProject(name.trim(), description.trim())
      projects = [p, ...projects]
      name = ''; description = ''
    } catch(e) { error = e.message }
    loading = false
  }

  async function deleteProject(id, e) {
    e.stopPropagation()
    if (!confirm('Excluir este projeto?')) return
    try {
      await api.deleteProject(id)
      projects = projects.filter(p => p.id !== id)
    } catch(e) { error = e.message }
  }

  function openProject(p) {
    $currentProject = p
    $currentStep = 1
  }
</script>

<style>
  h1 { font-size: 1.5rem; font-weight: 700; margin-bottom: 8px; color: #f1f5f9; }
  .subtitle { color: #64748b; margin-bottom: 32px; font-size: 0.9rem; }

  .create-form {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 12px;
    padding: 20px;
    margin-bottom: 32px;
  }
  .create-form h2 { font-size: 1rem; margin-bottom: 16px; color: #94a3b8; }
  .row { display: flex; gap: 12px; align-items: flex-end; }
  input, textarea {
    background: #0f172a;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 0.9rem;
    width: 100%;
    outline: none;
  }
  input:focus, textarea:focus { border-color: #38bdf8; }
  label { display: block; margin-bottom: 4px; font-size: 0.8rem; color: #64748b; }
  .field { flex: 1; }

  button {
    background: #38bdf8;
    color: #0f172a;
    border: none;
    padding: 9px 20px;
    border-radius: 8px;
    font-weight: 600;
    cursor: pointer;
    font-size: 0.9rem;
    white-space: nowrap;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }

  .projects-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 16px;
  }
  .project-card {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 12px;
    padding: 16px;
    cursor: pointer;
    transition: border-color 0.15s, transform 0.1s;
    position: relative;
  }
  .project-card:hover { border-color: #38bdf8; transform: translateY(-2px); }
  .project-card h3 { font-size: 1rem; color: #f1f5f9; margin-bottom: 4px; }
  .project-card p { font-size: 0.8rem; color: #64748b; }
  .project-card .date { font-size: 0.75rem; color: #475569; margin-top: 8px; }
  .del-btn {
    position: absolute; top: 12px; right: 12px;
    background: transparent; color: #ef4444; padding: 2px 6px;
    font-size: 0.75rem; border-radius: 4px;
  }
  .del-btn:hover { background: #ef4444; color: white; }

  .error { color: #ef4444; margin-bottom: 12px; font-size: 0.85rem; }
  .empty { color: #475569; text-align: center; padding: 40px; }
</style>

<h1>🐟 PicoFish</h1>
<p class="subtitle">Simulação social multi-agente leve — roda até no Raspberry Pi</p>

{#if error}<p class="error">⚠ {error}</p>{/if}

<div class="create-form">
  <h2>Novo Projeto</h2>
  <div class="row">
    <div class="field">
      <label>Nome do Projeto *</label>
      <input bind:value={name} placeholder="ex: Impacto da Regulação de IA" on:keydown={e => e.key === 'Enter' && createProject()} />
    </div>
    <div class="field">
      <label>Descrição</label>
      <input bind:value={description} placeholder="Descrição opcional" />
    </div>
    <button on:click={createProject} disabled={loading || !name.trim()}>
      {loading ? '...' : '+ Criar'}
    </button>
  </div>
</div>

{#if projects.length === 0}
  <p class="empty">Nenhum projeto ainda. Crie um acima para começar.</p>
{:else}
  <div class="projects-grid">
    {#each projects as p}
      <div class="project-card" on:click={() => openProject(p)}>
        <button class="del-btn" on:click={e => deleteProject(p.id, e)}>✕</button>
        <h3>{p.name}</h3>
        {#if p.description}<p>{p.description}</p>{/if}
        <p class="date">{p.created_at?.slice(0,10)}</p>
      </div>
    {/each}
  </div>
{/if}
