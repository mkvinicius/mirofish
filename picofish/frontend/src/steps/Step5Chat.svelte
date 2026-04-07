<script>
  import { api } from '../lib/api.js'
  import { currentProject, simRequirement } from '../stores/project.js'
  import NetworkGraph from '../NetworkGraph.svelte'
  import ReplayTimeline from '../ReplayTimeline.svelte'

  let activeTab = 'chat' // 'chat' | 'network' | 'replay'

  let messages = []
  let input = ''
  let loading = false
  let error = ''
  let chatEl

  async function send() {
    const msg = input.trim()
    if (!msg || loading) return
    input = ''
    error = ''
    messages = [...messages, { role: 'user', content: msg }]

    loading = true
    try {
      const history = messages.slice(0, -1).map(m => ({ role: m.role, content: m.content }))
      const req = $simRequirement || 'Análise de simulação social'
      const res = await api.chat($currentProject.id, req, msg, history)
      messages = [...messages, { role: 'assistant', content: res.response }]
    } catch(e) {
      error = e.message
      messages = messages.slice(0, -1)
    }
    loading = false

    setTimeout(() => { if (chatEl) chatEl.scrollTop = chatEl.scrollHeight }, 50)
  }

  const suggestions = [
    'Resuma as principais previsões da simulação',
    'Quais agentes foram mais influentes e por quê?',
    'Quais foram os sentimentos dominantes nas plataformas?',
    'Que riscos e oportunidades a simulação revela?',
    'Como diferentes grupos de agentes reagiram ao cenário?',
    'Quais efeitos em cascata emergiram da simulação?'
  ]
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 16px; }

  .tabs {
    display: flex; gap: 0; border-bottom: 1px solid #334155; margin-bottom: 20px;
  }
  .tab-btn {
    padding: 8px 18px; background: transparent; border: none;
    color: #64748b; cursor: pointer; font-size: 0.85rem; font-weight: 500;
    border-bottom: 2px solid transparent; margin-bottom: -1px;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab-btn.active { color: #38bdf8; border-bottom-color: #38bdf8; }
  .tab-btn:hover:not(.active) { color: #94a3b8; }

  .scenario-badge {
    background: #1e293b; border: 1px solid #334155;
    border-radius: 8px; padding: 8px 12px; font-size: 0.78rem;
    color: #64748b; margin-bottom: 16px;
  }
  .scenario-badge strong { color: #38bdf8; }

  .suggestions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; }
  .suggestion {
    background: #1e293b; border: 1px solid #334155; border-radius: 20px;
    padding: 6px 14px; font-size: 0.78rem; color: #94a3b8;
    cursor: pointer; transition: border-color 0.15s, color 0.15s;
  }
  .suggestion:hover { border-color: #38bdf8; color: #38bdf8; }

  .chat-window {
    background: #1e293b; border: 1px solid #334155; border-radius: 12px;
    height: 380px; overflow-y: auto; padding: 16px;
    display: flex; flex-direction: column; gap: 12px;
    margin-bottom: 12px; scroll-behavior: smooth;
  }
  .message { display: flex; gap: 10px; }
  .message.user { flex-direction: row-reverse; }
  .avatar {
    width: 32px; height: 32px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 0.9rem; flex-shrink: 0;
  }
  .user .avatar { background: #0c4a6e; }
  .assistant .avatar { background: #1a1a2e; border: 1px solid #334155; }
  .bubble {
    max-width: 80%; padding: 10px 14px; border-radius: 10px;
    font-size: 0.875rem; line-height: 1.6; white-space: pre-wrap;
  }
  .user .bubble { background: #0c4a6e; color: #e0f2fe; border-radius: 10px 10px 2px 10px; }
  .assistant .bubble { background: #0f172a; border: 1px solid #334155; color: #cbd5e1; border-radius: 10px 10px 10px 2px; }
  .empty { text-align: center; color: #475569; font-size: 0.85rem; margin: auto; }

  .input-row { display: flex; gap: 10px; }
  input {
    flex: 1; background: #1e293b; border: 1px solid #334155; color: #e2e8f0;
    padding: 10px 14px; border-radius: 8px; font-size: 0.9rem; outline: none;
  }
  input:focus { border-color: #38bdf8; }
  button {
    background: #38bdf8; color: #0f172a; border: none;
    padding: 10px 20px; border-radius: 8px; font-weight: 600;
    cursor: pointer; font-size: 0.9rem;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .error { color: #ef4444; font-size: 0.8rem; margin-top: 6px; }
  .typing { color: #64748b; font-size: 0.8rem; padding: 8px 0; }

  .tab-panel { padding: 0 2px; }
</style>

<h2>Passo 5 — Interação Profunda</h2>
<p class="desc">Análise completa: chat com o analista IA, grafo de influências, e replay da simulação hora a hora.</p>

<!-- Tab bar -->
<div class="tabs">
  <button class="tab-btn" class:active={activeTab === 'chat'} on:click={() => activeTab = 'chat'}>
    💬 Chat
  </button>
  <button class="tab-btn" class:active={activeTab === 'network'} on:click={() => activeTab = 'network'}>
    🕸 Rede de Influências
  </button>
  <button class="tab-btn" class:active={activeTab === 'replay'} on:click={() => activeTab = 'replay'}>
    ⏱ Replay
  </button>
</div>

<!-- Chat tab -->
{#if activeTab === 'chat'}
  <div class="tab-panel">
    {#if $simRequirement}
      <div class="scenario-badge">Cenário: <strong>{$simRequirement}</strong></div>
    {/if}

    <div class="suggestions">
      {#each suggestions as s}
        <button class="suggestion" on:click={() => { input = s; send() }}>{s}</button>
      {/each}
    </div>

    <div class="chat-window" bind:this={chatEl}>
      {#if messages.length === 0}
        <div class="empty">
          Pergunte qualquer coisa sobre os resultados da simulação...<br>
          <span style="font-size: 0.75rem; margin-top: 6px; display: block">Use as sugestões acima ou digite sua própria pergunta</span>
        </div>
      {/if}
      {#each messages as m}
        <div class="message {m.role}">
          <div class="avatar">{m.role === 'user' ? '👤' : '🐟'}</div>
          <div class="bubble">{m.content}</div>
        </div>
      {/each}
      {#if loading}
        <p class="typing">Analisando...</p>
      {/if}
    </div>

    <div class="input-row">
      <input
        bind:value={input}
        placeholder="Pergunte sobre a simulação..."
        on:keydown={e => e.key === 'Enter' && send()}
        disabled={loading}
      />
      <button on:click={send} disabled={loading || !input.trim()}>Enviar</button>
    </div>
    {#if error}<p class="error">{error}</p>{/if}
  </div>

<!-- Network tab -->
{:else if activeTab === 'network'}
  <div class="tab-panel">
    <NetworkGraph projectId={$currentProject?.id} />
  </div>

<!-- Replay tab -->
{:else if activeTab === 'replay'}
  <div class="tab-panel">
    <ReplayTimeline projectId={$currentProject?.id} />
  </div>
{/if}
