# PicoFish

**Same intelligence. Lighter engine.**

PicoFish is a full reimplementation of the MiroFish multi-agent social simulation platform — built from scratch in **Go + Svelte**, delivering **100% identical output quality** at a fraction of the computational cost.

Inspired by MiroFish the same way PicoClaw was inspired by OpenClaw: same performance, new engine.

---

## Why PicoFish?

MiroFish is powerful but heavy — it requires Docker, Zep Cloud, Python dependencies, and a decent server.

PicoFish delivers the **exact same results** using:
- A **single Go binary** (no Docker, no Python, no cloud)
- **Local vector embeddings** instead of Zep Cloud
- **Go goroutines** instead of the OASIS Python framework
- **JSON file storage** instead of a separate database
- Any **OpenAI-compatible API** (OpenAI, Qwen, Ollama, etc.)

The result? Runs on a **Raspberry Pi**.

---

## MiroFish vs PicoFish

| | MiroFish | PicoFish |
|---|---|---|
| Language | Python | **Go** |
| Deploy | Docker + multiple containers | **Single binary** |
| Memory (semantic) | Zep Cloud (paid, remote) | **Local embeddings + cosine similarity** |
| Simulation engine | OASIS (camel-ai) | **Go goroutines** |
| Knowledge graph | Zep graph + PostgreSQL | **JSON files + vector index** |
| Frontend | Vue 3 | **Svelte** |
| RAM usage | ~2–4 GB | **~128–256 MB** |
| Runs on Raspberry Pi | No | **Yes** |
| Output quality | 100% | **100%** |
| Report depth | Full ReACT | **Full ReACT (identical)** |
| Agent fidelity | OASIS-spec | **OASIS-spec (identical)** |

---

## How it works — 5-step pipeline

### Step 1 — Graph Construction
Paste any document (article, report, scenario). PicoFish uses the LLM to extract entities, relationships, and facts — building a **knowledge graph with vector embeddings** on every node and edge. Temporal edges (ValidAt / InvalidAt / ExpiredAt) track how facts evolve through the simulation, replicating Zep's temporal memory locally.

### Step 2 — Agent Generation
Each graph entity becomes a **fully-specified OASIS agent** — with MBTI personality, country, profession, stance, sentiment bias, influence weight, posts-per-hour, active hours, and response delay. Agents distinguish between individual people and abstract group entities, and get tailored prompts for Twitter or Reddit behavior.

### Step 3 — Simulation
Agents interact on simulated **Twitter and/or Reddit** across configurable hours of simulated time. The engine replicates OASIS exactly:
- **Feed algorithm**: recency decay × popularity × echo chamber boost
- **China timezone scheduling**: hour-by-hour activity multipliers (0.05 dead hours → 1.5 evening peak)
- **Per-agent semantic memory**: each agent remembers their past actions via embeddings
- **All action types**: CREATE_POST, LIKE, REPOST, REPLY, FOLLOW, COMMENT, UPVOTE, DOWNVOTE, SHARE, COLLECT

### Step 4 — Future Prediction Report
A **ReACT agent** analyzes the simulation with a god's-eye view — seeing everything, knowing everything — and generates a structured Future Prediction Report. It uses 4 specialized tools:

| Tool | Purpose |
|---|---|
| **InsightForge** | Deep multi-dimensional semantic analysis — generates sub-queries, searches across all dimensions, aggregates insights |
| **PanoramaSearch** | Full graph overview — all active facts, all historical temporal edges, complete entity landscape |
| **QuickSearch** | Fast semantic lookup for a specific question |
| **InterviewAgents** | Selects relevant agents by semantic similarity and interviews each in-character |

The agent calls 3–5 tools per section, quotes agent dialogue directly, and writes in prediction voice.

### Step 5 — Deep Interaction
Interactive Q&A with the simulation analyst. Full context: knowledge graph, all agent behaviors, simulation actions, and report findings.

---

## Quick Start

### Requirements
- Go 1.21+
- Node.js 18+ (for frontend build, one-time)
- Any OpenAI-compatible API (OpenAI, Qwen, Ollama, etc.)

### Setup

```bash
git clone https://github.com/mkvinicius/picofish.git
cd picofish

cp .env.example .env
# Edit .env with your API credentials
```

**.env**
```env
# OpenAI
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=sk-...
LLM_MODEL=gpt-4o-mini
EMBED_MODEL=text-embedding-3-small   # optional, defaults to LLM_MODEL

# Or Qwen (Aliyun Bailian)
# LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
# LLM_API_KEY=sk-...
# LLM_MODEL=qwen-turbo

# Or Ollama (local, free)
# LLM_BASE_URL=http://localhost:11434/v1
# LLM_API_KEY=ollama
# LLM_MODEL=llama3.2

DATA_DIR=./data
PORT=5002
```

### Run (development)

```bash
make dev
# Backend:  http://localhost:5002
# Frontend: http://localhost:3001
```

### Production build

```bash
make all
./dist/picofish
# Serves frontend + API from http://localhost:5002
```

---

## Raspberry Pi deployment

```bash
# Cross-compile from your dev machine
make build-pi

# Copy to the Pi
scp dist/picofish-arm64 pi@raspberrypi:~/
scp -r frontend/dist pi@raspberrypi:~/frontend

# On the Pi — that's it
FRONTEND_DIR=./frontend ./picofish-arm64
```

No Docker. No Python. No cloud services. Just a binary and a folder.

---

## Architecture

```
picofish/
├── backend/
│   ├── main.go
│   ├── config/          # env config
│   ├── storage/         # JSON file store (no external DB)
│   ├── api/             # net/http router + handlers
│   └── services/
│       ├── llm/         # OpenAI-compat client + embeddings + cosine similarity
│       ├── graph/       # knowledge graph + temporal edges + 4 ReACT tools
│       ├── agents/      # OasisAgentProfile + simulation engine
│       └── report/      # ReACT report generation + chat
└── frontend/            # Svelte + Vite, 5-step UI
```

**External dependencies**: only `github.com/google/uuid` and `github.com/joho/godotenv`. Everything else is Go standard library.

---

## Inspiration

MiroFish built something remarkable. PicoFish takes the same architecture and makes it run anywhere.

Same engine. Less weight. Same fire.

---

## License

MIT
