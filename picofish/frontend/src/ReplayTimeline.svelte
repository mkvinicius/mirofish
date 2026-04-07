<script>
  import { onMount } from 'svelte'
  import { api } from './lib/api.js'

  export let projectId

  let frames = []
  let loading = false
  let error = ''
  let currentHour = 0
  let frame = null

  $: frame = frames[currentHour] || null
  $: maxHour = frames.length - 1

  async function load() {
    loading = true
    error = ''
    try {
      const data = await api.getReplay(projectId)
      frames = data.frames || []
      currentHour = 0
    } catch (e) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function exportData(format) {
    window.open(api.replayExportUrl(projectId, format), '_blank')
  }

  onMount(load)
</script>

<div class="replay-panel">
  <div class="panel-header">
    <h3>Simulation Replay</h3>
    <div class="header-actions">
      <button on:click={load} disabled={loading}>{loading ? 'Loading…' : 'Refresh'}</button>
      {#if frames.length > 0}
        <button on:click={() => exportData('json')}>↓ JSON</button>
        <button on:click={() => exportData('csv')}>↓ CSV</button>
        <button on:click={() => exportData('md')}>↓ Markdown</button>
      {/if}
    </div>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if loading}
    <p class="muted">Loading replay frames…</p>
  {:else if frames.length === 0}
    <p class="muted">No replay data yet. Start a simulation to record frames.</p>
  {:else}
    <!-- Timeline slider -->
    <div class="slider-row">
      <span class="hour-label">Hour {frame?.hour ?? 0}</span>
      <input
        type="range" min="0" max={maxHour} step="1"
        bind:value={currentHour}
        class="timeline-slider"
      />
      <span class="hour-label">Sim {frame?.sim_hour ?? 0}:00</span>
    </div>

    {#if frame}
      <div class="frame-stats">
        <div class="stat"><span>Actions</span><strong>{frame.action_count}</strong></div>
        <div class="stat"><span>Agents active</span><strong>{frame.agent_count}</strong></div>
        <div class="stat"><span>Posts visible</span><strong>{frame.posts?.length ?? 0}</strong></div>
      </div>

      <!-- Recent posts at this hour -->
      {#if frame.posts?.length > 0}
        <div class="section-title">Posts at this hour</div>
        <div class="post-list">
          {#each frame.posts.slice(0, 6) as p}
            <div class="post-item">
              <span class="post-author">@{p.author_name}</span>
              <span class="post-hour">[{p.sim_hour}:00]</span>
              <span class="post-likes">👍{p.like_count}</span>
              <span class="post-content">{p.content}</span>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Agent states -->
      {#if frame.agents?.length > 0}
        <div class="section-title">Agent states</div>
        <div class="agent-table-wrap">
          <table>
            <thead>
              <tr><th>Agent</th><th>Stance</th><th>Sentiment</th><th>Actions</th></tr>
            </thead>
            <tbody>
              {#each frame.agents as ag}
                <tr>
                  <td>{ag.agent_name}</td>
                  <td>
                    <span class="stance-badge stance-{ag.stance}">{ag.stance}</span>
                  </td>
                  <td>
                    <div class="sentiment-bar-wrap">
                      <div
                        class="sentiment-bar"
                        style="width:{Math.abs(ag.sentiment_bias) * 50}%;
                               left:{ag.sentiment_bias >= 0 ? '50%' : (50 + ag.sentiment_bias * 50) + '%'};
                               background:{ag.sentiment_bias >= 0 ? '#22c55e' : '#ef4444'}"
                      ></div>
                    </div>
                    <span class="sentiment-val">{ag.sentiment_bias.toFixed(2)}</span>
                  </td>
                  <td>{ag.action_count}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .replay-panel { display: flex; flex-direction: column; gap: 1rem; }
  .panel-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 0.5rem; }
  h3 { margin: 0; font-size: 1rem; color: #e2e8f0; }
  .header-actions { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  button {
    padding: 0.3rem 0.7rem; background: #334155; color: #e2e8f0;
    border: none; border-radius: 6px; cursor: pointer; font-size: 0.8rem;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  .muted { color: #64748b; font-size: 0.9rem; }
  .error { color: #ef4444; font-size: 0.85rem; }

  .slider-row { display: flex; align-items: center; gap: 0.75rem; }
  .hour-label { color: #94a3b8; font-size: 0.8rem; white-space: nowrap; }
  .timeline-slider { flex: 1; accent-color: #3b82f6; }

  .frame-stats { display: flex; gap: 1rem; flex-wrap: wrap; }
  .stat {
    background: #1e293b; border: 1px solid #334155; border-radius: 8px;
    padding: 0.5rem 1rem; display: flex; flex-direction: column; align-items: center;
  }
  .stat span { font-size: 0.75rem; color: #64748b; }
  .stat strong { font-size: 1.1rem; color: #e2e8f0; }

  .section-title { font-size: 0.8rem; color: #64748b; text-transform: uppercase; letter-spacing: 0.05em; margin-top: 0.25rem; }

  .post-list { display: flex; flex-direction: column; gap: 0.3rem; }
  .post-item {
    background: #1e293b; border-radius: 6px; padding: 0.4rem 0.6rem;
    font-size: 0.8rem; display: flex; gap: 0.5rem; align-items: baseline; flex-wrap: wrap;
  }
  .post-author { color: #60a5fa; font-weight: 600; }
  .post-hour { color: #475569; }
  .post-likes { color: #94a3b8; }
  .post-content { color: #cbd5e1; flex: 1; }

  .agent-table-wrap { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.8rem; }
  th { color: #64748b; padding: 0.3rem 0.5rem; text-align: left; }
  td { color: #cbd5e1; padding: 0.3rem 0.5rem; border-top: 1px solid #1e293b; }

  .stance-badge {
    font-size: 0.7rem; padding: 0.1rem 0.5rem; border-radius: 999px; background: #334155; color: #94a3b8;
  }
  .stance-supportive { background: #14532d; color: #86efac; }
  .stance-opposing { background: #7f1d1d; color: #fca5a5; }
  .stance-neutral { background: #1e1b4b; color: #a5b4fc; }

  .sentiment-bar-wrap {
    display: inline-block; width: 80px; height: 6px; background: #334155;
    border-radius: 3px; position: relative; vertical-align: middle; margin-right: 0.4rem;
  }
  .sentiment-bar {
    position: absolute; height: 100%; border-radius: 3px;
  }
  .sentiment-val { font-size: 0.75rem; color: #94a3b8; }
</style>
