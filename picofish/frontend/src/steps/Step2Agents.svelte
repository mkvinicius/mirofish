<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let agents = []
  let loading = false
  let error = ''

  onMount(async () => {
    try { agents = await api.listAgents($currentProject.id) } catch {}
  })

  async function generate() {
    loading = true; error = ''
    try {
      const res = await api.generateProfiles($currentProject.id, [])
      agents = res.agents || []
    } catch(e) { error = e.message }
    loading = false
  }

  function sentimentColor(v) {
    if (v > 0.3) return '#22c55e'
    if (v < -0.3) return '#ef4444'
    return '#f59e0b'
  }

  function activityBar(v) {
    return Math.round((v || 0) * 100)
  }
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 24px; }

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
    margin-left: 12px;
  }
  button.secondary:hover { border-color: #38bdf8; color: #38bdf8; }

  .actions { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
  .loading { color: #64748b; font-size: 0.85rem; }
  .error { color: #ef4444; font-size: 0.85rem; }

  .agents-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 14px;
  }
  .agent-card {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 10px;
    padding: 14px;
  }

  .agent-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 8px; }
  .agent-name { font-weight: 600; font-size: 0.95rem; color: #f1f5f9; }
  .agent-meta { font-size: 0.75rem; color: #64748b; }

  .agent-tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 6px; }
  .tag {
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 1px 6px;
    font-size: 0.7rem;
    color: #94a3b8;
  }

  .bars { margin-top: 10px; display: flex; flex-direction: column; gap: 4px; }
  .bar-row { display: flex; align-items: center; gap: 8px; font-size: 0.72rem; color: #64748b; }
  .bar-label { width: 64px; }
  .bar-track { flex: 1; height: 4px; background: #0f172a; border-radius: 2px; }
  .bar-fill { height: 100%; border-radius: 2px; }

  .agent-bg { font-size: 0.75rem; color: #64748b; margin-top: 8px; line-height: 1.4; }

  .next-btn { margin-top: 24px; display: flex; justify-content: flex-end; }
  .count { color: #38bdf8; font-weight: 600; }
</style>

<h2>Step 2 — Generate Agents</h2>
<p class="desc">PicoFish creates agent profiles from your graph entities. Each agent has unique personality, behavior, and background.</p>

<div class="actions">
  <button on:click={generate} disabled={loading}>
    {#if loading}⏳ Generating...{:else}🤖 Generate Agent Profiles{/if}
  </button>
  {#if loading}<span class="loading">Creating profiles via LLM (may take a moment)...</span>{/if}
  {#if error}<span class="error">⚠ {error}</span>{/if}
</div>

{#if agents.length > 0}
  <p style="margin-bottom: 16px; font-size: 0.85rem; color: #64748b;">
    <span class="count">{agents.length}</span> agents ready
  </p>
  <div class="agents-grid">
    {#each agents as a}
      <div class="agent-card">
        <div class="agent-header">
          <div>
            <div class="agent-name">{a.name}</div>
            <div class="agent-meta">{a.age}y · {a.profession}</div>
          </div>
        </div>

        <div class="agent-tags">
          <span class="tag">🧠 {a.personality}</span>
          {#each (a.interests || '').split(',').slice(0, 2) as i}
            {#if i.trim()}<span class="tag">{i.trim()}</span>{/if}
          {/each}
        </div>

        <div class="bars">
          <div class="bar-row">
            <span class="bar-label">Activity</span>
            <div class="bar-track">
              <div class="bar-fill" style="width:{activityBar(a.activity_level)}%; background:#38bdf8"></div>
            </div>
            <span>{activityBar(a.activity_level)}%</span>
          </div>
          <div class="bar-row">
            <span class="bar-label">Sentiment</span>
            <div class="bar-track">
              <div class="bar-fill" style="width:{activityBar((a.sentiment_bias+1)/2)}%; background:{sentimentColor(a.sentiment_bias)}"></div>
            </div>
            <span style="color:{sentimentColor(a.sentiment_bias)}">{a.sentiment_bias > 0 ? '+' : ''}{(a.sentiment_bias || 0).toFixed(1)}</span>
          </div>
        </div>

        {#if a.background}
          <p class="agent-bg">{a.background.slice(0, 100)}{a.background.length > 100 ? '...' : ''}</p>
        {/if}
      </div>
    {/each}
  </div>

  <div class="next-btn">
    <button on:click={() => $currentStep = 3}>Next: Run Simulation →</button>
  </div>
{/if}
