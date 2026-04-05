# 🐟 PicoFish

> MiroFish, lighter. Runs on a Raspberry Pi.

PicoFish is a lightweight rewrite of MiroFish — same 5-step multi-agent simulation workflow, but built in **Go + Svelte** with **SQLite** and **Ollama**, consuming a fraction of the resources.

## MiroFish vs PicoFish

| Feature | MiroFish | PicoFish |
|--------|---------|---------|
| Backend | Python / Flask | **Go / Gin** |
| Graph DB | Zep Cloud (remote) | **SQLite (local)** |
| Simulation | OASIS (camel-ai) | **Custom Go goroutines** |
| Frontend | Vue 3 + Vite | **Svelte + Vite** |
| LLM | OpenAI SDK | **Ollama / any OpenAI-compat API** |
| Deployment | Docker required | **Single binary** |
| RAM usage | ~1–2GB | **~50–150MB** |
| Runs on Pi | No | **Yes** |

## Same 5-Step Workflow

1. **Graph Construction** — Paste a document → LLM extracts entities → SQLite knowledge graph
2. **Agent Generation** — Graph nodes → LLM creates agent personas with personality & behavior
3. **Simulation** — Go goroutines simulate agent interactions on Twitter/Reddit
4. **Report Generation** — ReACT pattern: LLM plans → queries graph → synthesizes report
5. **Deep Interaction** — Chat with the AI analyst about simulation results

## Quick Start

```bash
# 1. Clone and enter picofish
cp .env.example .env
# Edit .env with your LLM settings

# 2. Install Ollama (for local LLM on Raspberry Pi)
curl -fsSL https://ollama.ai/install.sh | sh
ollama pull llama3

# 3. Run
make dev
# Backend: http://localhost:5002
# Frontend: http://localhost:3001
```

## Raspberry Pi Deployment

```bash
# Build ARM64 binary on your dev machine
make build-pi

# Copy to Raspberry Pi
scp dist/picofish-arm64 pi@raspberrypi:~/picofish
scp -r frontend/dist pi@raspberrypi:~/picofish-frontend

# On the Pi
FRONTEND_DIR=./picofish-frontend ./picofish-arm64
```

## Configuration

```env
# Use Ollama (local, free)
LLM_BASE_URL=http://localhost:11434/v1
LLM_API_KEY=ollama
LLM_MODEL=llama3

# Or use any OpenAI-compatible API
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=sk-...
LLM_MODEL=gpt-4o-mini
```

## Requirements

- Go 1.21+
- Node.js 18+
- Ollama (or any OpenAI-compatible LLM API)
- No cloud services required
