<script>
  import { currentProject, currentStep } from './stores/project.js'
  import ProjectSelect from './steps/ProjectSelect.svelte'
  import Step1Graph from './steps/Step1Graph.svelte'
  import Step2Agents from './steps/Step2Agents.svelte'
  import Step3Simulation from './steps/Step3Simulation.svelte'
  import Step4Report from './steps/Step4Report.svelte'
  import Step5Chat from './steps/Step5Chat.svelte'

  const steps = [
    { n: 1, label: 'Graph' },
    { n: 2, label: 'Agents' },
    { n: 3, label: 'Simulation' },
    { n: 4, label: 'Report' },
    { n: 5, label: 'Interact' }
  ]
</script>

<style>
  :global(*) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background: #0f172a;
    color: #e2e8f0;
    min-height: 100vh;
  }

  header {
    background: #1e293b;
    border-bottom: 1px solid #334155;
    padding: 12px 24px;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .logo { font-size: 1.4rem; font-weight: 700; color: #38bdf8; }
  .tagline { font-size: 0.8rem; color: #64748b; }

  .project-badge {
    margin-left: auto;
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 4px 12px;
    font-size: 0.8rem;
    color: #94a3b8;
    cursor: pointer;
  }
  .project-badge:hover { border-color: #38bdf8; color: #38bdf8; }

  .stepper {
    display: flex;
    gap: 0;
    padding: 16px 24px;
    background: #1e293b;
    border-bottom: 1px solid #334155;
  }

  .step {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    padding: 6px 12px;
    border-radius: 6px;
    transition: background 0.15s;
    font-size: 0.85rem;
    color: #64748b;
  }
  .step:hover { background: #0f172a; }
  .step.active { color: #38bdf8; }
  .step.done { color: #22c55e; }

  .step-num {
    width: 24px; height: 24px;
    border-radius: 50%;
    border: 2px solid currentColor;
    display: flex; align-items: center; justify-content: center;
    font-size: 0.75rem; font-weight: 700;
    flex-shrink: 0;
  }
  .step.active .step-num { background: #38bdf8; color: #0f172a; border-color: #38bdf8; }
  .step.done .step-num { background: #22c55e; color: #0f172a; border-color: #22c55e; }

  .step-connector {
    flex: 0 0 20px;
    height: 2px;
    background: #334155;
    align-self: center;
    margin: 0 -4px;
  }

  main { padding: 24px; max-width: 960px; margin: 0 auto; }
</style>

<header>
  <span class="logo">🐟 PicoFish</span>
  <span class="tagline">Lightweight Multi-Agent Simulation</span>
  {#if $currentProject}
    <span class="project-badge" on:click={() => { $currentProject = null; $currentStep = 1 }}>
      📁 {$currentProject.name} ✕
    </span>
  {/if}
</header>

{#if !$currentProject}
  <main><ProjectSelect /></main>
{:else}
  <div class="stepper">
    {#each steps as s, i}
      {#if i > 0}<div class="step-connector"></div>{/if}
      <div
        class="step {$currentStep === s.n ? 'active' : ''} {$currentStep > s.n ? 'done' : ''}"
        on:click={() => $currentStep = s.n}
      >
        <span class="step-num">{$currentStep > s.n ? '✓' : s.n}</span>
        {s.label}
      </div>
    {/each}
  </div>

  <main>
    {#if $currentStep === 1}<Step1Graph />
    {:else if $currentStep === 2}<Step2Agents />
    {:else if $currentStep === 3}<Step3Simulation />
    {:else if $currentStep === 4}<Step4Report />
    {:else if $currentStep === 5}<Step5Chat />
    {/if}
  </main>
{/if}
