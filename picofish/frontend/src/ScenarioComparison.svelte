<script>
  import { api } from './lib/api.js'

  export let projectId
  export let baseTopic = ''

  let scenarios = [
    { id: 's1', label: 'Baseline', seed: 42, total_hours: 12, platform: 'twitter', topic_suffix: '', patches: [] },
    { id: 's2', label: 'Alt Scenario', seed: 123, total_hours: 12, platform: 'twitter', topic_suffix: '', patches: [] }
  ]
  let report = null
  let loading = false
  let error = ''

  function addScenario() {
    if (scenarios.length >= 4) return
    const id = 's' + (scenarios.length + 1)
    scenarios = [...scenarios, {
      id,
      label: 'Scenario ' + (scenarios.length + 1),
      seed: Math.floor(Math.random() * 9999),
      total_hours: 12,
      platform: 'twitter',
      topic_suffix: '',
      patches: []
    }]
  }

  function removeScenario(i) {
    if (scenarios.length <= 2) return
    scenarios = scenarios.filter((_, idx) => idx !== i)
  }

  async function run() {
    loading = true
    error = ''
    report = null
    try {
      report = await api.compareScenarios(projectId, baseTopic || 'Simulation topic', scenarios)
    } catch (e) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function polarizationColor(idx) {
    if (idx < 0.2) return '#22c55e'
    if (idx < 0.5) return '#f59e0b'
    return '#ef4444'
  }
</script>

<div class="scenarios-panel">
  <div class="panel-header">
    <h3>Multi-Scenario Comparison</h3>
    <button on:click={addScenario} disabled={scenarios.length >= 4 || loading}>+ Add Scenario</button>
  </div>

  <div class="base-topic">
    <label>Base topic</label>
    <input bind:value={baseTopic} placeholder="e.g. AI regulation debate" />
  </div>

  <div class="scenario-list">
    {#each scenarios as sc, i}
      <div class="scenario-card">
        <div class="sc-row">
          <input class="label-input" bind:value={sc.label} placeholder="Scenario label" />
          {#if scenarios.length > 2}
            <button class="remove" on:click={() => removeScenario(i)}>✕</button>
          {/if}
        </div>
        <div class="sc-row">
          <label>Seed <input type="number" bind:value={sc.seed} /></label>
          <label>Hours <input type="number" bind:value={sc.total_hours} min="4" max="48" /></label>
          <label>Topic suffix <input bind:value={sc.topic_suffix} placeholder="optional" /></label>
        </div>
      </div>
    {/each}
  </div>

  <button class="run-btn" on:click={run} disabled={loading}>
    {loading ? 'Running comparison…' : 'Run Comparison'}
  </button>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if report}
    <div class="results">
      <h4>Results</h4>

      <div class="result-grid">
        {#each report.results as r, i}
          <div class="result-card">
            <div class="rc-title">{r.label}</div>
            <div class="rc-stat">
              <span>Actions</span><strong>{r.action_count}</strong>
            </div>
            <div class="rc-stat">
              <span>Posts</span><strong>{r.post_count}</strong>
            </div>
            <div class="rc-stat">
              <span>Avg engagement</span><strong>{r.avg_engagement}</strong>
            </div>
            <div class="rc-stat">
              <span>Polarization</span>
              <strong style="color:{polarizationColor(r.polarization_index)}">
                {(r.polarization_index * 100).toFixed(1)}%
              </strong>
            </div>
            <div class="stance-breakdown">
              {#each Object.entries(r.stance_breakdown || {}) as [stance, count]}
                <span class="stance-pill stance-{stance}">{stance}: {count}</span>
              {/each}
            </div>
          </div>
        {/each}
      </div>

      <div class="divergence">
        <strong>Divergence Analysis</strong>
        <p>Max polarization delta: <strong>{(report.divergence.max_polarization_delta * 100).toFixed(2)}%</strong></p>
        <p>Most divergent agent: <strong>{report.divergence.most_divergent_agent || '—'}</strong></p>
        <p>Action range: <strong>{report.divergence.action_count_range?.[0]} – {report.divergence.action_count_range?.[1]}</strong></p>
      </div>
    </div>
  {/if}
</div>

<style>
  .scenarios-panel { display: flex; flex-direction: column; gap: 1rem; }
  .panel-header { display: flex; align-items: center; justify-content: space-between; }
  h3 { margin: 0; font-size: 1rem; color: #e2e8f0; }
  h4 { margin: 0 0 0.5rem; color: #cbd5e1; }
  .base-topic { display: flex; align-items: center; gap: 0.75rem; }
  .base-topic label { color: #94a3b8; font-size: 0.85rem; white-space: nowrap; }
  .base-topic input { flex: 1; }
  .scenario-list { display: flex; flex-direction: column; gap: 0.5rem; }
  .scenario-card {
    background: #1e293b; border: 1px solid #334155;
    border-radius: 8px; padding: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;
  }
  .sc-row { display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; }
  .label-input { flex: 1; font-weight: 600; }
  .remove {
    background: #7f1d1d; color: #fca5a5; border: none; border-radius: 4px;
    padding: 0.2rem 0.5rem; cursor: pointer; font-size: 0.8rem;
  }
  label { display: flex; align-items: center; gap: 0.4rem; font-size: 0.8rem; color: #94a3b8; }
  input { background: #0f172a; color: #e2e8f0; border: 1px solid #334155; border-radius: 6px; padding: 0.3rem 0.6rem; }
  input[type=number] { width: 70px; }
  button {
    padding: 0.4rem 1rem; background: #3b82f6; color: white;
    border: none; border-radius: 6px; cursor: pointer; font-size: 0.85rem;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  .run-btn { align-self: flex-start; }
  .error { color: #ef4444; font-size: 0.85rem; }
  .results { border-top: 1px solid #334155; padding-top: 1rem; }
  .result-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 0.75rem; }
  .result-card {
    background: #1e293b; border: 1px solid #334155; border-radius: 8px;
    padding: 0.75rem; display: flex; flex-direction: column; gap: 0.4rem;
  }
  .rc-title { font-weight: 700; color: #e2e8f0; font-size: 0.9rem; border-bottom: 1px solid #334155; padding-bottom: 0.3rem; }
  .rc-stat { display: flex; justify-content: space-between; font-size: 0.8rem; color: #94a3b8; }
  .rc-stat strong { color: #e2e8f0; }
  .stance-breakdown { display: flex; gap: 0.4rem; flex-wrap: wrap; margin-top: 0.3rem; }
  .stance-pill {
    font-size: 0.7rem; padding: 0.1rem 0.4rem; border-radius: 999px; background: #334155; color: #94a3b8;
  }
  .stance-supportive { background: #14532d; color: #86efac; }
  .stance-opposing { background: #7f1d1d; color: #fca5a5; }
  .stance-neutral { background: #1e1b4b; color: #a5b4fc; }
  .divergence {
    background: #1e293b; border: 1px solid #334155; border-radius: 8px;
    padding: 0.75rem; margin-top: 0.75rem; font-size: 0.85rem; color: #94a3b8;
  }
  .divergence strong { color: #e2e8f0; }
  .divergence p { margin: 0.3rem 0; }
</style>
