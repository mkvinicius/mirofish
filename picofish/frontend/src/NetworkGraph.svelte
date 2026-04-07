<script>
  import { onMount, onDestroy } from 'svelte'
  import { api } from './lib/api.js'

  export let projectId

  let networkData = null
  let loading = false
  let error = ''
  let svgEl
  let animFrame
  let nodes = []
  let links = []

  // Cluster colour palette
  const clusterColors = {
    supportive: '#22c55e',
    opposing: '#ef4444',
    neutral: '#6366f1',
    observer: '#f59e0b',
    default: '#94a3b8'
  }

  function clusterColor(cluster) {
    return clusterColors[cluster] || clusterColors.default
  }

  async function load() {
    if (!projectId) return
    loading = true
    error = ''
    try {
      const data = await api.getNetwork(projectId)
      networkData = data
      nodes = (data.d3?.nodes || []).map(n => ({
        ...n,
        x: Math.random() * 600 + 50,
        y: Math.random() * 400 + 50,
        vx: 0,
        vy: 0
      }))
      links = data.d3?.links || []
      simulate()
    } catch (e) {
      error = e.message
    } finally {
      loading = false
    }
  }

  // Very simple force simulation (no d3 dependency)
  function simulate() {
    cancelAnimationFrame(animFrame)
    const W = 700, H = 500
    const nodeMap = Object.fromEntries(nodes.map(n => [n.id, n]))
    let iter = 0

    function step() {
      if (iter++ > 200) return
      const alpha = Math.max(0.01, 1 - iter / 150)

      // Repulsion
      for (let i = 0; i < nodes.length; i++) {
        for (let j = i + 1; j < nodes.length; j++) {
          const a = nodes[i], b = nodes[j]
          const dx = b.x - a.x || 0.1
          const dy = b.y - a.y || 0.1
          const dist = Math.sqrt(dx * dx + dy * dy) || 1
          const force = (80 * 80) / (dist * dist) * alpha
          a.vx -= dx / dist * force
          a.vy -= dy / dist * force
          b.vx += dx / dist * force
          b.vy += dy / dist * force
        }
      }

      // Link attraction
      for (const link of links) {
        const s = nodeMap[link.source]
        const t = nodeMap[link.target]
        if (!s || !t) continue
        const dx = t.x - s.x
        const dy = t.y - s.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const force = (dist - 120) * 0.03 * alpha
        s.vx += dx / dist * force
        s.vy += dy / dist * force
        t.vx -= dx / dist * force
        t.vy -= dy / dist * force
      }

      // Centre gravity
      for (const n of nodes) {
        n.vx += (W / 2 - n.x) * 0.003 * alpha
        n.vy += (H / 2 - n.y) * 0.003 * alpha
        n.vx *= 0.85
        n.vy *= 0.85
        n.x = Math.max(20, Math.min(W - 20, n.x + n.vx))
        n.y = Math.max(20, Math.min(H - 20, n.y + n.vy))
      }

      nodes = nodes
      animFrame = requestAnimationFrame(step)
    }
    animFrame = requestAnimationFrame(step)
  }

  onMount(load)
  onDestroy(() => cancelAnimationFrame(animFrame))
</script>

<div class="network-panel">
  <div class="panel-header">
    <h3>Agent Influence Network</h3>
    <button on:click={load} disabled={loading}>
      {loading ? 'Loading…' : 'Refresh'}
    </button>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {:else if loading}
    <p class="muted">Building influence network…</p>
  {:else if !networkData}
    <p class="muted">Run a simulation first, then load the network.</p>
  {:else}
    <!-- SVG Force Graph -->
    <svg bind:this={svgEl} viewBox="0 0 700 500" class="graph-svg">
      <!-- Links -->
      {#each links as link}
        {@const s = nodes.find(n => n.id === link.source)}
        {@const t = nodes.find(n => n.id === link.target)}
        {#if s && t}
          <line
            x1={s.x} y1={s.y}
            x2={t.x} y2={t.y}
            stroke="#475569"
            stroke-width={Math.max(0.5, link.weight * 3)}
            stroke-opacity="0.5"
          />
        {/if}
      {/each}

      <!-- Nodes -->
      {#each nodes as node}
        <g transform="translate({node.x},{node.y})">
          <circle
            r={node.radius}
            fill={clusterColor(node.cluster)}
            stroke="#1e293b"
            stroke-width="1.5"
            opacity="0.9"
          />
          <text
            dy="0.35em"
            text-anchor="middle"
            font-size="9"
            fill="#f1f5f9"
            pointer-events="none"
          >{node.name.split(' ')[0]}</text>
        </g>
      {/each}
    </svg>

    <!-- Legend + top agents -->
    <div class="network-info">
      <div class="legend">
        {#each Object.entries(clusterColors).filter(([k]) => k !== 'default') as [stance, color]}
          <span class="legend-dot" style="background:{color}"></span>
          <span>{stance}</span>
        {/each}
      </div>

      <div class="top-agents">
        <strong>Top Influencers</strong>
        <table>
          <thead><tr><th>Agent</th><th>PageRank</th><th>In-degree</th></tr></thead>
          <tbody>
            {#each (networkData.agents || []).slice(0, 8) as a}
              <tr>
                <td>{a.agent_name}</td>
                <td>{(a.page_rank * 100).toFixed(1)}%</td>
                <td>{a.in_degree}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>

<style>
  .network-panel { display: flex; flex-direction: column; gap: 1rem; }
  .panel-header { display: flex; align-items: center; justify-content: space-between; }
  h3 { margin: 0; font-size: 1rem; color: #e2e8f0; }
  button {
    padding: 0.3rem 0.8rem; background: #3b82f6; color: white;
    border: none; border-radius: 6px; cursor: pointer; font-size: 0.8rem;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  .graph-svg {
    width: 100%; border: 1px solid #334155; border-radius: 8px;
    background: #0f172a;
  }
  .muted { color: #64748b; font-size: 0.9rem; }
  .error { color: #ef4444; font-size: 0.85rem; }
  .network-info { display: flex; gap: 2rem; flex-wrap: wrap; }
  .legend { display: flex; gap: 0.5rem; align-items: center; font-size: 0.8rem; color: #94a3b8; flex-wrap: wrap; }
  .legend-dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  .top-agents { flex: 1; min-width: 260px; }
  .top-agents strong { font-size: 0.85rem; color: #cbd5e1; }
  table { width: 100%; border-collapse: collapse; margin-top: 0.4rem; font-size: 0.8rem; }
  th { text-align: left; color: #64748b; padding: 0.2rem 0.4rem; }
  td { color: #cbd5e1; padding: 0.2rem 0.4rem; border-top: 1px solid #1e293b; }
</style>
