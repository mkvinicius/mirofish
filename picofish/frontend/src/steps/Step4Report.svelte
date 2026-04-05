<script>
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../lib/api.js'
  import { currentProject, currentStep, simRequirement } from '../stores/project.js'

  let status = null
  let error = ''
  let interval = null
  let scenarioInput = $simRequirement || ''

  onMount(async () => {
    status = await api.getReport($currentProject.id).catch(() => null)
  })
  onDestroy(() => { if (interval) clearInterval(interval) })

  async function generate() {
    error = ''
    const req = scenarioInput.trim() || $simRequirement || 'Social simulation analysis'
    simRequirement.set(req)
    try {
      status = await api.generateReport($currentProject.id, req)
      if (interval) clearInterval(interval)
      interval = setInterval(poll, 3000)
    } catch(e) { error = e.message }
  }

  async function poll() {
    try {
      status = await api.getReport($currentProject.id)
      if (status?.status === 'completed' || status?.status === 'error') {
        clearInterval(interval); interval = null
      }
    } catch {}
  }

  // Minimal markdown-to-HTML renderer
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
      .replace(/<\/li>\n<li>/g, '</li><li>')
      .replace(/(<li>.*<\/li>)/gs, '<ul>$1</ul>')
      .replace(/\n\n/g, '</p><p>')
      .replace(/^/, '<p>')
      .replace(/$/, '</p>')
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
  button.regen {
    background: transparent;
    border: 1px solid #334155;
    color: #94a3b8;
    font-size: 0.8rem;
    padding: 4px 12px;
    margin-left: 12px;
  }
  button.regen:hover { border-color: #38bdf8; color: #38bdf8; }

  .error { color: #ef4444; font-size: 0.85rem; margin-bottom: 12px; }

  .status-pill {
    display: inline-block;
    padding: 3px 10px;
    border-radius: 20px;
    font-size: 0.8rem;
    font-weight: 600;
    margin-bottom: 16px;
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
    margin-bottom: 16px;
  }
  .outline h3 { font-size: 0.9rem; color: #94a3b8; margin-bottom: 8px; }
  .outline-title { font-weight: 600; color: #f1f5f9; margin-bottom: 4px; }
  .section-list { list-style: none; padding: 0; margin: 0; }
  .section-list li { font-size: 0.82rem; color: #64748b; padding: 2px 0; }
  .section-list li::before { content: "• "; color: #38bdf8; }

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
</style>

<h2>Step 4 — Generate Report</h2>
<p class="desc">The ReACT agent analyzes simulation results using InsightForge, PanoramaSearch, QuickSearch, and InterviewAgents tools to produce a Future Prediction Report.</p>

{#if error}<p class="error">{error}</p>{/if}

{#if !status || status.status === 'not_found' || (!status.status)}
  <label>Scenario (sim requirement)</label>
  <input bind:value={scenarioInput} placeholder="Describe the scenario — e.g. Government announces AI regulation..." />
  <button on:click={generate}>Generate Report</button>

{:else if status.status === 'planning'}
  <span class="status-pill planning">Planning outline...</span>
  <p style="color: #64748b; font-size: 0.85rem">Analyzing the simulation scenario and structuring the report.</p>

{:else if status.status === 'generating'}
  <span class="status-pill generating">Generating report...</span>
  {#if status.outline}
    <div class="outline">
      <h3>Report outline</h3>
      <div class="outline-title">{status.outline.title}</div>
      <ul class="section-list">
        {#each status.outline.sections || [] as sec}
          <li>{sec.title}</li>
        {/each}
      </ul>
    </div>
  {/if}
  <p style="color: #64748b; font-size: 0.85rem">The ReACT agent is using tools to gather evidence. This may take a few minutes.</p>

{:else if status.status === 'completed'}
  <span class="status-pill completed">Report ready</span>
  <button class="regen" on:click={generate}>Regenerate</button>
  <div class="report-body">{@html renderMarkdown(status.content)}</div>
  <div class="next-btn">
    <button on:click={() => $currentStep = 5}>Next: Deep Interaction →</button>
  </div>

{:else if status.status === 'error'}
  <span class="status-pill err">Error</span>
  <p style="color: #ef4444; font-size: 0.85rem; margin-top: 8px">{status.error || status.content}</p>
  <label style="margin-top: 16px">Scenario</label>
  <input bind:value={scenarioInput} placeholder="Describe the scenario..." />
  <button style="margin-top: 8px" on:click={generate}>Retry</button>

{:else}
  <label>Scenario</label>
  <input bind:value={scenarioInput} placeholder="Describe the scenario..." />
  <button on:click={generate}>Generate Report</button>
{/if}
