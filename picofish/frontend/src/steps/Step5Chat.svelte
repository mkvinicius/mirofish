<script>
  import { api } from '../lib/api.js'
  import { currentProject, simRequirement } from '../stores/project.js'

  let messages = [] // { role: 'user'|'assistant', content: string }
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
      const req = $simRequirement || 'Social simulation analysis'
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
    'Summarize the main predictions of the simulation',
    'Which agents were most influential and why?',
    'What were the dominant sentiments across platforms?',
    'What risks and opportunities does the simulation reveal?',
    'How did different agent groups react to the scenario?',
    'What cascade effects emerged from the simulation?'
  ]
</script>

<style>
  h2 { font-size: 1.2rem; font-weight: 700; margin-bottom: 4px; }
  .desc { color: #64748b; font-size: 0.85rem; margin-bottom: 20px; }

  .scenario-badge {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 8px;
    padding: 8px 12px;
    font-size: 0.78rem;
    color: #64748b;
    margin-bottom: 16px;
  }
  .scenario-badge strong { color: #38bdf8; }

  .suggestions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; }
  .suggestion {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 20px;
    padding: 6px 14px;
    font-size: 0.78rem;
    color: #94a3b8;
    cursor: pointer;
    transition: border-color 0.15s, color 0.15s;
  }
  .suggestion:hover { border-color: #38bdf8; color: #38bdf8; }

  .chat-window {
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 12px;
    height: 420px;
    overflow-y: auto;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 12px;
    scroll-behavior: smooth;
  }

  .message { display: flex; gap: 10px; }
  .message.user { flex-direction: row-reverse; }

  .avatar {
    width: 32px; height: 32px;
    border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 0.9rem;
    flex-shrink: 0;
  }
  .user .avatar { background: #0c4a6e; }
  .assistant .avatar { background: #1a1a2e; border: 1px solid #334155; }

  .bubble {
    max-width: 80%;
    padding: 10px 14px;
    border-radius: 10px;
    font-size: 0.875rem;
    line-height: 1.6;
    white-space: pre-wrap;
  }
  .user .bubble { background: #0c4a6e; color: #e0f2fe; border-radius: 10px 10px 2px 10px; }
  .assistant .bubble { background: #0f172a; border: 1px solid #334155; color: #cbd5e1; border-radius: 10px 10px 10px 2px; }

  .empty { text-align: center; color: #475569; font-size: 0.85rem; margin: auto; }

  .input-row { display: flex; gap: 10px; }
  input {
    flex: 1;
    background: #1e293b;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 0.9rem;
    outline: none;
  }
  input:focus { border-color: #38bdf8; }

  button {
    background: #38bdf8;
    color: #0f172a;
    border: none;
    padding: 10px 20px;
    border-radius: 8px;
    font-weight: 600;
    cursor: pointer;
    font-size: 0.9rem;
  }
  button:hover { background: #7dd3fc; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }

  .error { color: #ef4444; font-size: 0.8rem; margin-top: 6px; }
  .typing { color: #64748b; font-size: 0.8rem; padding: 8px 0; }
</style>

<h2>Step 5 — Deep Interaction</h2>
<p class="desc">Ask anything about the simulation. The AI analyst has god's-eye view of the knowledge graph, all agent behaviors, and temporal dynamics.</p>

{#if $simRequirement}
  <div class="scenario-badge">Scenario: <strong>{$simRequirement}</strong></div>
{/if}

<div class="suggestions">
  {#each suggestions as s}
    <button class="suggestion" on:click={() => { input = s; send() }}>{s}</button>
  {/each}
</div>

<div class="chat-window" bind:this={chatEl}>
  {#if messages.length === 0}
    <div class="empty">
      Ask anything about the simulation results...<br>
      <span style="font-size: 0.75rem; margin-top: 6px; display: block">Use the suggestions above or type your own question</span>
    </div>
  {/if}
  {#each messages as m}
    <div class="message {m.role}">
      <div class="avatar">{m.role === 'user' ? '👤' : '🐟'}</div>
      <div class="bubble">{m.content}</div>
    </div>
  {/each}
  {#if loading}
    <p class="typing">Analyzing...</p>
  {/if}
</div>

<div class="input-row">
  <input
    bind:value={input}
    placeholder="Ask about the simulation..."
    on:keydown={e => e.key === 'Enter' && send()}
    disabled={loading}
  />
  <button on:click={send} disabled={loading || !input.trim()}>Send</button>
</div>
{#if error}<p class="error">{error}</p>{/if}
