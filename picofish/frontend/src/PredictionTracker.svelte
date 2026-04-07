<script>
  import { onMount } from 'svelte'
  import { api } from './lib/api.js'

  export let projectId

  let predictions = []
  let calibration = null
  let loading = false
  let extracting = false
  let error = ''

  async function load() {
    loading = true
    error = ''
    try {
      const data = await api.getPredictions(projectId)
      predictions = data.predictions || []
      calibration = data.calibration || null
    } catch (e) {
      error = e.message
    } finally {
      loading = false
    }
  }

  async function extract() {
    extracting = true
    error = ''
    try {
      const data = await api.extractPredictions(projectId)
      await load()
    } catch (e) {
      error = e.message
    } finally {
      extracting = false
    }
  }

  async function markOutcome(pred, correct) {
    try {
      await api.markPredictionOutcome(projectId, pred.id, correct)
      await load()
    } catch (e) {
      error = e.message
    }
  }

  function confidenceColor(c) {
    if (c >= 0.7) return '#22c55e'
    if (c >= 0.4) return '#f59e0b'
    return '#ef4444'
  }

  function timeframeIcon(t) {
    return { short: '⚡', medium: '📅', long: '🔭' }[t] || '❓'
  }

  onMount(load)
</script>

<div class="predictions-panel">
  <div class="panel-header">
    <h3>Prediction Tracker</h3>
    <div class="header-actions">
      <button on:click={load} disabled={loading}>Refresh</button>
      <button class="extract-btn" on:click={extract} disabled={extracting || loading}>
        {extracting ? 'Extracting…' : '✦ Extract from report'}
      </button>
    </div>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if calibration && calibration.resolved > 0}
    <div class="calibration-bar">
      <div class="cal-stat">
        <span>Accuracy</span>
        <strong>{(calibration.accuracy * 100).toFixed(0)}%</strong>
      </div>
      <div class="cal-stat">
        <span>Brier score</span>
        <strong>{calibration.brier_score}</strong>
      </div>
      <div class="cal-stat">
        <span>Avg confidence</span>
        <strong>{(calibration.avg_confidence * 100).toFixed(0)}%</strong>
      </div>
      <div class="cal-stat">
        <span>Resolved</span>
        <strong>{calibration.resolved}/{calibration.total_predictions}</strong>
      </div>
    </div>
  {/if}

  {#if loading}
    <p class="muted">Loading predictions…</p>
  {:else if predictions.length === 0}
    <p class="muted">No predictions yet. Generate a report and click "Extract from report".</p>
  {:else}
    <div class="pred-list">
      {#each predictions as pred}
        <div class="pred-card" class:resolved={pred.outcome !== null}>
          <div class="pred-meta">
            <span class="timeframe">{timeframeIcon(pred.timeframe)} {pred.timeframe}</span>
            <span class="category">{pred.category}</span>
            {#if pred.outcome !== null}
              <span class="outcome" class:correct={pred.outcome} class:incorrect={!pred.outcome}>
                {pred.outcome ? '✓ Correct' : '✗ Incorrect'}
              </span>
            {/if}
          </div>

          <p class="pred-claim">{pred.claim}</p>

          <!-- Confidence bar -->
          <div class="conf-row">
            <span class="conf-label">Confidence</span>
            <div class="conf-bar-wrap">
              <div
                class="conf-bar"
                style="width:{pred.confidence * 100}%; background:{confidenceColor(pred.confidence)}"
              ></div>
            </div>
            <span class="conf-pct" style="color:{confidenceColor(pred.confidence)}">
              {(pred.confidence * 100).toFixed(0)}%
            </span>
          </div>

          {#if pred.outcome === null}
            <div class="outcome-btns">
              <button class="correct-btn" on:click={() => markOutcome(pred, true)}>👍 Correct</button>
              <button class="incorrect-btn" on:click={() => markOutcome(pred, false)}>👎 Incorrect</button>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .predictions-panel { display: flex; flex-direction: column; gap: 1rem; }
  .panel-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 0.5rem; }
  h3 { margin: 0; font-size: 1rem; color: #e2e8f0; }
  .header-actions { display: flex; gap: 0.4rem; }
  button {
    padding: 0.3rem 0.7rem; background: #334155; color: #e2e8f0;
    border: none; border-radius: 6px; cursor: pointer; font-size: 0.8rem;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  .extract-btn { background: #4f46e5; }
  .muted { color: #64748b; font-size: 0.9rem; }
  .error { color: #ef4444; font-size: 0.85rem; }

  .calibration-bar {
    display: flex; gap: 1rem; flex-wrap: wrap;
    background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 0.75rem;
  }
  .cal-stat { display: flex; flex-direction: column; align-items: center; gap: 0.1rem; min-width: 80px; }
  .cal-stat span { font-size: 0.72rem; color: #64748b; }
  .cal-stat strong { font-size: 1rem; color: #e2e8f0; }

  .pred-list { display: flex; flex-direction: column; gap: 0.6rem; }
  .pred-card {
    background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 0.75rem;
    display: flex; flex-direction: column; gap: 0.5rem;
  }
  .pred-card.resolved { border-color: #1e3a5f; }

  .pred-meta { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  .timeframe { font-size: 0.75rem; color: #94a3b8; }
  .category {
    font-size: 0.72rem; background: #0f172a; color: #64748b;
    padding: 0.1rem 0.5rem; border-radius: 999px;
  }
  .outcome { font-size: 0.75rem; font-weight: 700; margin-left: auto; }
  .outcome.correct { color: #22c55e; }
  .outcome.incorrect { color: #ef4444; }

  .pred-claim { margin: 0; color: #cbd5e1; font-size: 0.88rem; line-height: 1.5; }

  .conf-row { display: flex; align-items: center; gap: 0.5rem; }
  .conf-label { font-size: 0.75rem; color: #64748b; white-space: nowrap; }
  .conf-bar-wrap {
    flex: 1; height: 6px; background: #334155; border-radius: 3px; overflow: hidden;
  }
  .conf-bar { height: 100%; border-radius: 3px; transition: width 0.3s; }
  .conf-pct { font-size: 0.75rem; font-weight: 700; width: 36px; text-align: right; }

  .outcome-btns { display: flex; gap: 0.5rem; }
  .correct-btn { background: #14532d; color: #86efac; }
  .incorrect-btn { background: #7f1d1d; color: #fca5a5; }
</style>
