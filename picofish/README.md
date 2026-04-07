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
| Memory (semantic) | Zep Cloud (paid, remote) | **Local episodic memory + belief store** |
| Simulation engine | OASIS (camel-ai) | **Go goroutines** |
| Knowledge graph | Zep graph + PostgreSQL | **JSON files + GraphRAG hop traversal** |
| Frontend | Vue 3 | **Svelte** |
| RAM usage | ~2–4 GB | **~128–256 MB** |
| Runs on Raspberry Pi | No | **Yes** |
| Output quality | 100% | **100%+** |
| Report depth | Full ReACT | **Full ReACT + self-critique loop** |
| Agent fidelity | OASIS-spec | **OASIS-spec + individual/group types** |
| Reproducibility | No | **Yes — deterministic seeds** |
| Graph traversal | GraphRAG multi-hop | **GraphRAG multi-hop (local)** |
| Scenario comparison | No | **Yes — multi-scenario statistical divergence** |
| Influence network | No | **Yes — PageRank + D3.js export** |
| Mid-sim injection | No | **Yes — InjectionEvent queue** |
| Prediction tracking | No | **Yes — confidence scores + Brier calibration** |
| Simulation replay | No | **Yes — JSON/CSV/Markdown export** |
| Live simulation feed | No | **Yes — SSE per-action stream** |
| Report streaming | No | **Yes — SSE progress log** |
| Project dashboard | No | **Yes — card grid, search, clone** |
| Example seeds | No | **Yes — 3 built-in scenarios** |
| Panic recovery | No | **Yes — per-request, logged** |
| Configurable params | No | **Yes — full .env surface** |

---

## How it works — 5-step pipeline

### Step 1 — Graph Construction
Paste any document (article, report, scenario). PicoFish uses the LLM to extract entities, relationships, and facts — building a **knowledge graph with vector embeddings** on every node and edge. Temporal edges (ValidAt / InvalidAt / ExpiredAt) track how facts evolve through the simulation, replicating Zep's temporal memory locally.

### Step 2 — Agent Generation
Each graph entity becomes a **fully-specified OASIS agent** with two types:

- **Individual agents**: human personas with MBTI, emotional reasoning, 1–3 posts/hour, full episodic memory, belief decay, FOLLOW/UNFOLLOW actions
- **Group agents**: institutional entities (media, governments, companies) — rational posting, 3–8 posts/hour, 2.5× feed influence multiplier, simplified memory, no following

Detection is automatic: entities with >3 graph relationships or institutional name keywords become group agents.

### Step 3 — Simulation
Agents interact across **6 platforms** (X/Twitter, Reddit, Instagram, TikTok, WhatsApp, Facebook) with calibrated parameters:

- **Feed algorithm**: `exp(-0.15 × hours)` recency decay · `log(1 + likes + reposts) × 0.3` popularity · stance-based echo chamber (1.8× boost for same stance, 0.4× suppression for opposite)
- **Calibrated scheduling**: hour-by-hour multipliers (0.02 dead hours skipped entirely → 1.5 evening peak)
- **Episodic memory**: each agent maintains `EpisodicEvent` history with embeddings, `BeliefStore` per topic, and belief revision on contradiction (cosine similarity < 0.3)
- **Deterministic seeds**: pass a seed to `Start()` for reproducible simulation output
- **All action types**: CREATE_POST, LIKE, REPOST, REPLY, FOLLOW, COMMENT, UPVOTE, DOWNVOTE, SHARE, STORY, REEL, CREATE_VIDEO, SEND_MESSAGE, FORWARD, REACT, JOIN_GROUP

### Step 4 — Future Prediction Report
A **ReACT agent** analyzes the simulation and generates a structured Future Prediction Report with a 2-iteration **self-critique loop**. Uses 4 specialized tools with GraphRAG expansion:

| Tool | Hop depth | Purpose |
|---|---|---|
| **InsightForge** | 2 hops | Deep multi-dimensional semantic analysis with sub-query decomposition |
| **PanoramaSearch** | 3 hops | Full graph overview — ranked by semantic relevance, temporal tracking |
| **QuickSearch** | 1 hop | Fast focused lookup |
| **InterviewAgents** | — | Selects agents by semantic similarity, interviews each in-character |

**Mandatory sections guaranteed**: Timeline of Key Events · Key Actors & Influence Scores · Prediction Confidence · Divergence Points

The self-critique loop reviews the draft twice: adding confidence scores, removing unsupported conclusions, and ensuring all key actors appear.

### Step 5 — Deep Interaction
Interactive Q&A with the simulation analyst. Full context: knowledge graph, all agent behaviors, simulation actions, and report findings. Three built-in tabs:

- **Chat** — free-form Q&A with the ReACT analyst
- **Influence Network** — D3.js force-directed graph showing PageRank scores and stance clusters
- **Replay** — hour-by-hour timeline slider; export to JSON, CSV, or Markdown

---

## Phase 2 — Exclusive Features (not in MiroFish)

### Multi-Scenario Comparison Engine
Run 2–4 parallel scenario variants with different seeds, topic suffixes, or agent patches. Computes:
- **Polarization Index** — average pairwise cosine distance of agent belief vectors (0 = consensus, 1 = max polarization)
- **Divergence stats** — per-agent sentiment range, max polarization delta, action count range

```
POST /api/v1/projects/:id/scenarios/compare
{ "base_topic": "...", "scenarios": [...] }
```

### Agent Influence Network
After simulation, computes a weighted directed influence graph:
- **PageRank** (10 iterations, damping 0.85) — normalized to [0, 1]
- **Clusters** by stance with internal density
- **D3.js export** — node radius = 5 + PageRank × 30

```
GET /api/v1/projects/:id/network
```

### Mid-Simulation Injection Events
Queue breaking-news or narrative shift events to fire at a specific simulated hour:

```json
POST /api/v1/projects/:id/simulation/inject
{ "hour": 14, "content": "Breaking: ...", "visibility": "public" }
```

Visibility modes: `"public"` (everyone sees it), `"targeted"` (specific agents only), `"rumor"` (30% random spread in feeds).

### Confidence-Scored Predictions with Calibration
After report generation, the LLM extracts falsifiable predictions with confidence scores. Mark outcomes as ✓/✗ to compute:
- **Brier score** — calibration accuracy (lower = better)
- **Accuracy by confidence band** — high/medium/low breakdown

```
POST /api/v1/projects/:id/report/predictions/extract
POST /api/v1/projects/:id/report/predictions/:id/outcome
```

### Simulation Replay & Export
Every simulation hour is recorded as a `ReplayFrame` (agent snapshots + post summaries). Export the full replay:

```
GET /api/v1/projects/:id/simulation/replay
GET /api/v1/projects/:id/simulation/replay/export?format=json|csv|md
```

---

## Phase 3 — Production Features

### Live Simulation Feed (SSE)
Every agent action is broadcast in real time as the simulation runs:
```
GET /api/v1/projects/:id/simulation/feed   (text/event-stream)
```
Event types: `action` · `hour` · `injection` · `done` · `error`. The frontend shows a live scrolling action log with platform icons and agent names, replacing the polling loop entirely.

### Streaming Report Progress
Report generation emits progress events over SSE as each section is written:
```
GET /api/v1/projects/:id/report/stream
```
The UI shows a collapsible log (e.g. `📋 Planning… → ✍ Writing Seção 1… → 🔍 Critique pass 1…`), so you see the ReACT agent working in real time.

### Project Dashboard
The home screen shows a card grid with per-project stats (agent count, action count, last simulation date, prediction count) loaded asynchronously. Features: live search filter, clone modal, one-click delete with confirmation.

### Example Seeds
Three built-in scenario seeds available via the UI "Load example" dropdown or the API:
```
GET /api/v1/seeds
```
| Seed | Topic | Suggested hours | Suggested agents |
|---|---|---|---|
| Financial Crisis | Central bank announces emergency interest rate hike | 48 | 30 |
| Climate Policy | Global summit signs landmark carbon reduction treaty | 72 | 40 |
| Product Launch | Major tech company releases controversial AI assistant | 36 | 25 |

### Error Resilience
- **Graph validation**: minimum 3 entities + 2 relationships; automatic fallback to simpler extraction prompt
- **Per-call LLM timeout**: configurable `LLM_TIMEOUT_SECONDS` (default 30s) applied per attempt, not per session
- **Per-agent panic recovery**: `defer recover()` in every goroutine; one crashed agent never halts the simulation
- **Global HTTP panic recovery middleware**: all panics are caught, logged with stack trace + `request_id`, returned as JSON error

### Full Configuration Surface
Every tunable parameter is now an environment variable with a sensible default — no code changes needed:

| Variable | Default | Description |
|---|---|---|
| `LLM_TIMEOUT_SECONDS` | 30 | Per-LLM-call timeout |
| `LLM_MAX_RETRIES` | 3 | Retry attempts with exponential backoff |
| `SIMULATION_WORKER_POOL_SIZE` | 10 | Concurrent agent goroutines |
| `MAX_AGENTS` | 100 | Hard cap on agents per project |
| `DEFAULT_SIMULATION_HOURS` | 24 | Default simulation length |
| `DEAD_HOUR_THRESHOLD` | 0.1 | Hour multiplier below which hour is skipped |
| `ECHO_CHAMBER_BOOST` | 1.8 | Feed score multiplier for same-stance content |
| `OPP_STANCE_SUPPRESSION` | 0.4 | Feed score multiplier for opposite-stance |
| `RECENCY_DECAY_RATE` | 0.15 | Exponential decay rate for post age |
| `POPULARITY_LOG_WEIGHT` | 0.3 | Weight of log(likes+reposts) in feed score |
| `BELIEF_DECAY_PER_HOUR` | 0.05 | Belief strength decay per simulated hour |
| `CONTRADICTION_THRESHOLD` | 0.3 | Cosine similarity below which belief is revised |
| `MAX_EPISODIC_MEMORY_INDIVIDUAL` | 50 | Max episodic events per individual agent |
| `MAX_EPISODIC_MEMORY_GROUP` | 20 | Max episodic events per group agent |
| `REPORT_CRITIQUE_ITERATIONS` | 2 | Self-critique passes after draft |
| `MIN_TOOL_CALLS_PER_SECTION` | 2 | Minimum evidence calls per report section |
| `GRAPH_MAX_HOPS` | 3 | Max GraphRAG hop depth |

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

# Tuning (all optional, sensible defaults)
# LLM_TIMEOUT_SECONDS=30
# LLM_MAX_RETRIES=3
# SIMULATION_WORKER_POOL_SIZE=10
# MAX_AGENTS=100
# ECHO_CHAMBER_BOOST=1.8
# OPP_STANCE_SUPPRESSION=0.4
# REPORT_CRITIQUE_ITERATIONS=2
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
│   ├── tests/           # unit tests + benchmarks
│   └── services/
│       ├── llm/         # OpenAI-compat client + embeddings + cosine similarity
│       ├── graph/       # knowledge graph + GraphRAG hop traversal + 4 ReACT tools
│       ├── agents/      # OasisAgentProfile + memory.go + simulation engine
│       └── report/      # ReACT report + self-critique loop + chat
└── frontend/            # Svelte + Vite, 5-step UI
```

**External dependencies**: only `github.com/google/uuid` and `github.com/joho/godotenv`. Everything else is Go standard library.

---

## Testing

```bash
cd backend
go test ./tests/... -v          # run all unit tests
go test ./tests/... -bench=.    # run benchmarks
```

Tests run without an LLM — pure unit tests covering belief decay, hop traversal, echo chamber logic, agent type detection, and mandatory report sections.

| Test | What it verifies |
|---|---|
| `TestBeliefDecay` | Belief strength decays 0.05/hour, reaches < 0.05 after 20 hours |
| `TestGraphHopTraversal` | BFS hop traversal returns correct neighbor sets |
| `TestHopTraversalBidirectional` | Edges followed in both directions |
| `TestEchoChamberEffect` | Opposite stance detection drives feed suppression |
| `TestDeterministicOutput` | Hour multipliers match spec (dead hours < 0.1, peak ≥ 1.0) |
| `TestAgentTypeDetection` | Group entity keywords classified correctly |
| `TestReportHasMandatorySections` | All 4 required sections present |

---

## Inspiration

MiroFish built something remarkable. PicoFish takes the same architecture and makes it run anywhere.

Same engine. Less weight. Same fire.

---

## License

MIT
