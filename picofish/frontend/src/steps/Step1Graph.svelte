<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let document = ''
  let loading = false
  let result = null
  let error = ''
  let nodes = []
  let seeds = []
  let selectedSeedId = ''
  let seedHints = null  // { suggestedHours, suggestedAgentCount }

  onMount(async () => {
    loadExisting()
    seeds = await api.listSeeds().catch(() => [])
  })

  async function buildGraph() {
    if (!document.trim()) return
    loading = true; error = ''; result = null
    try {
      result = await api.buildGraph($currentProject.id, document)
      nodes = await api.getNodes($currentProject.id)
    } catch(e) { error = e.message }
    loading = false
  }

  async function loadExisting() {
    try { nodes = await api.getNodes($currentProject.id) } catch {}
  }

  function onSeedChange() {
    if (!selectedSeedId) { seedHints = null; return }
    const seed = seeds.find(s => s.id === selectedSeedId)
    if (!seed) return
    document = seed.text
    seedHints = {
      suggestedHours: seed.suggested_hours,
      suggestedAgentCount: seed.suggested_agent_count,
    }
  }
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 24px; }

  textarea {
    width: 100%;
    min-height: 200px;
    background: #0f172a;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 12px;
    border-radius: 8px;
    font-size: 0.875rem;
    line-height: 1.6;
    resize: vertical;
    outline: none;
    font-family: inherit;
  }
  textarea:focus { border-color: #38bdf8; }

  .actions { display: flex; gap: 12px; margin-top: 12px; align-items: center; }

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

  .result-card {
    background: #1e293b;
    border: 1px solid #22c55e;
    border-radius: 12px;
    padding: 20px;
    margin-top: 20px;
  }
  .stat-row { display: flex; gap: 24px; margin-bottom: 12px; }
  .stat { text-align: center; }
  .stat-num { font-size: 2rem; font-weight: 700; color: #22c55e; }
  .stat-label { font-size: 0.75rem; color: #64748b; }

  .types { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .tag {
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 2px 8px;
    font-size: 0.75rem;
    color: #94a3b8;
  }

  .nodes-section { margin-top: 24px; }
  .nodes-section h3 { font-size: 0.9rem; color: #64748b; margin-bottom: 12px; }
  .node-list { display: grid; gap: 8px; }
  .node-item {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 8px;
    padding: 10px 14px;
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .node-type {
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 2px 6px;
    font-size: 0.7rem;
    color: #38bdf8;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .node-name { font-size: 0.875rem; font-weight: 600; color: #f1f5f9; }
  .node-desc { font-size: 0.75rem; color: #64748b; margin-top: 2px; }

  .error { color: #ef4444; font-size: 0.85rem; margin-top: 8px; }
  .loading { color: #64748b; font-size: 0.85rem; }

  .seed-row { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
  .seed-label { font-size: 0.8rem; color: #64748b; white-space: nowrap; }
  select.seed-select {
    background: #0f172a; border: 1px solid #334155; color: #e2e8f0;
    padding: 7px 10px; border-radius: 8px; font-size: 0.85rem; outline: none; flex: 1; min-width: 200px;
  }
  select.seed-select:focus { border-color: #38bdf8; }
  .seed-hints {
    display: flex; gap: 10px; flex-wrap: wrap;
    background: #1e293b; border: 1px solid #334155; border-radius: 8px;
    padding: 8px 14px; margin-bottom: 12px; font-size: 0.78rem;
  }
  .hint-item { color: #64748b; }
  .hint-item span { color: #38bdf8; font-weight: 600; }

  .next-btn { margin-top: 24px; display: flex; justify-content: flex-end; }
</style>

<h2>Passo 1 — Construir Grafo de Conhecimento</h2>
<p class="desc">Cole qualquer documento, artigo ou texto. O PicoFish extrai entidades e constrói um grafo.</p>

{#if seeds.length > 0}
  <div class="seed-row">
    <span class="seed-label">Carregar exemplo:</span>
    <select class="seed-select" bind:value={selectedSeedId} on:change={onSeedChange}>
      <option value="">— Escolha um cenário de exemplo —</option>
      {#each seeds as s}
        <option value={s.id}>{s.name}</option>
      {/each}
    </select>
  </div>
{/if}

{#if seedHints}
  <div class="seed-hints">
    <span class="hint-item">Horas sugeridas: <span>{seedHints.suggestedHours}h</span></span>
    <span class="hint-item">Agentes sugeridos: <span>~{seedHints.suggestedAgentCount}</span></span>
    <span class="hint-item" style="color: #475569">← use esses valores nos passos seguintes</span>
  </div>
{/if}

<textarea
  bind:value={document}
  placeholder="Cole seu documento aqui...&#10;&#10;Exemplo: Uma notícia, documento de política, artigo científico ou qualquer texto descrevendo o cenário que deseja simular."
/>

{#if error}<p class="error">⚠ {error}</p>{/if}

<div class="actions">
  <button on:click={buildGraph} disabled={loading || !document.trim()}>
    {#if loading}⏳ Construindo grafo...{:else}🔨 Construir Grafo{/if}
  </button>
  {#if loading}<span class="loading">Extraindo entidades via LLM...</span>{/if}
</div>

{#if result}
  <div class="result-card">
    <div class="stat-row">
      <div class="stat">
        <div class="stat-num">{result.node_count}</div>
        <div class="stat-label">Entidades</div>
      </div>
      <div class="stat">
        <div class="stat-num">{result.edge_count}</div>
        <div class="stat-label">Relações</div>
      </div>
    </div>
    <div class="types">
      {#each (result.entity_types || []) as t}
        <span class="tag">{t}</span>
      {/each}
    </div>
  </div>
{/if}

{#if nodes.length > 0}
  <div class="nodes-section">
    <h3>Entidades do Grafo ({nodes.length})</h3>
    <div class="node-list">
      {#each nodes as n}
        <div class="node-item">
          <span class="node-type">{n.type}</span>
          <div>
            <div class="node-name">{n.name}</div>
            {#if n.properties?.description}
              <div class="node-desc">{n.properties.description}</div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </div>

  <div class="next-btn">
    <button on:click={() => $currentStep = 2}>Próximo: Gerar Agentes →</button>
  </div>
{/if}
