<script>
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep } from '../stores/project.js'

  let topic = ''
  let rounds = 5
  let status = null
  let actions = []
  let error = ''
  let interval = null

  onMount(async () => {
    await refresh()
  })
  onDestroy(() => { if (interval) clearInterval(interval) })

  async function refresh() {
    try {
      status = await api.getSimulationStatus($currentProject.id)
      if (status?.status === 'running') {
        actions = await api.getSimulationActions($currentProject.id)
      }
    } catch {}
  }

  async function start() {
    if (!topic.trim()) return
    error = ''
    try {
      await api.startSimulation($currentProject.id, rounds, topic)
      status = { status: 'running', current_round: 0, total_rounds: rounds }
      actions = []
      if (!interval) interval = setInterval(refresh, 2000)
    } catch(e) { error = e.message }
  }

  async function stop() {
    try {
      await api.stopSimulation($currentProject.id)
      if (interval) { clearInterval(interval); interval = null }
      await refresh()
    } catch(e) { error = e.message }
  }

  $: if (status?.status === 'completed' || status?.status === 'stopped') {
    if (interval) { clearInterval(interval); interval = null }
    if (status.status === 'completed') {
      api.getSimulationActions($currentProject.id).then(a => actions = a).catch(() => {})
    }
  }

  $: if (status?.status === 'running' && !interval) {
    interval = setInterval(refresh, 2000)
  }

  function actionColor(type) {
    return type === 'CREATE_POST' ? '#38bdf8' : type === 'COMMENT' ? '#a78bfa' : '#22c55e'
  }

  function platformIcon(p) {
    return p === 'twitter' ? '🐦' : '🤖'
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
  input:focus { border-color: #38bdf8; }
  input[type=number] { max-width: 80px; }

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
</style>

<h2>Step 3 — Run Simulation</h2>
<p class="desc">Agents will interact on simulated Twitter and Reddit, producing organic social dynamics.</p>

{#if error}<p class="error">⚠ {error}</p>{/if}

<div class="config-row">
  <div class="field" style="flex: 3">
    <label>Topic / Initial Event *</label>
    <input bind:value={topic} placeholder="e.g. The government announces a new energy policy..." />
  </div>
  <div class="field" style="flex: 0">
    <label>Rounds</label>
    <input type="number" bind:value={rounds} min="1" max="20" />
  </div>
  {#if status?.status !== 'running'}
    <button on:click={start} disabled={!topic.trim()}>▶ Start</button>
  {:else}
    <button class="stop" on:click={stop}>⏹ Stop</button>
  {/if}
</div>

{#if status && status.status !== 'not_started'}
  <div class="status-bar">
    <div class="status-header">
      <span class="status-label {status.status}">{status.status.toUpperCase()}</span>
      {#if status.status === 'running'}
        <span style="font-size: 0.8rem; color: #64748b">auto-refreshing...</span>
      {/if}
    </div>
    {#if status.total_rounds > 0}
      <div class="progress-track">
        <div class="progress-fill" style="width:{Math.round((status.current_round / status.total_rounds) * 100)}%"></div>
      </div>
      <p class="progress-text">Round {status.current_round} / {status.total_rounds}</p>
    {/if}
    <div class="stats">
      <div class="stat">Agents: <span>{status.agent_count || 0}</span></div>
      <div class="stat">Actions: <span>{status.action_count || 0}</span></div>
    </div>
  </div>
{/if}

{#if actions.length > 0}
  <div class="actions-feed">
    <h3>Live Feed ({actions.length} actions)</h3>
    <div class="action-list">
      {#each actions as a}
        <div class="action-item">
          <div class="action-header">
            <span class="action-platform">{platformIcon(a.platform)}</span>
            <span class="action-agent">{a.agent_name}</span>
            <span class="action-type" style="color:{actionColor(a.action_type)}">{a.action_type}</span>
            <span style="font-size: 0.7rem; color: #475569; margin-left: auto">Round {a.round}</span>
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
    <button on:click={() => $currentStep = 4}>Next: Generate Report →</button>
  </div>
{/if}
