<script>
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let document = ''
  let loading = false
  let result = null
  let error = ''
  let nodes = []

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

  import { onMount } from 'svelte'
  onMount(loadExisting)
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
  button.secondary {
    background: transparent;
    border: 1px solid #334155;
    color: #94a3b8;
  }
  button.secondary:hover { border-color: #38bdf8; color: #38bdf8; }

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

  .next-btn { margin-top: 24px; display: flex; justify-content: flex-end; }
</style>

<h2>Step 1 — Build Knowledge Graph</h2>
<p class="desc">Paste any document, article, or text. PicoFish will extract entities and build a graph.</p>

<textarea
  bind:value={document}
  placeholder="Paste your document here...&#10;&#10;Example: A news article, policy document, research paper, or any text describing a scenario you want to simulate."
/>

{#if error}<p class="error">⚠ {error}</p>{/if}

<div class="actions">
  <button on:click={buildGraph} disabled={loading || !document.trim()}>
    {#if loading}⏳ Building graph...{:else}🔨 Build Graph{/if}
  </button>
  {#if loading}<span class="loading">Extracting entities via LLM...</span>{/if}
</div>

{#if result}
  <div class="result-card">
    <div class="stat-row">
      <div class="stat">
        <div class="stat-num">{result.node_count}</div>
        <div class="stat-label">Entities</div>
      </div>
      <div class="stat">
        <div class="stat-num">{result.edge_count}</div>
        <div class="stat-label">Relations</div>
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
    <h3>Graph Entities ({nodes.length})</h3>
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
    <button on:click={() => $currentStep = 2}>Next: Generate Agents →</button>
  </div>
{/if}
