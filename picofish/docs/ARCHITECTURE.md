# PicoFish Architecture

## Overview

PicoFish is a single-binary Go server with a Svelte frontend. The backend has no external database and no external services beyond an OpenAI-compatible LLM API. All state is stored as JSON files on disk.

```
┌─────────────────────────────────────────────────────────────────┐
│                        Browser (Svelte)                         │
│  ProjectSelect → Step1Graph → Step2Agents → Step3Sim → Step4   │
└───────────────────────────┬─────────────────────────────────────┘
                            │ HTTP + SSE
┌───────────────────────────▼─────────────────────────────────────┐
│                    Go HTTP Server (main.go)                      │
│  Recovery middleware → Router → Handlers                        │
│                                                                  │
│  ┌────────────┐  ┌─────────────┐  ┌──────────────┐             │
│  │ api/       │  │ api/        │  │ api/          │             │
│  │ projects   │  │ simulation  │  │ report        │             │
│  └─────┬──────┘  └──────┬──────┘  └──────┬────────┘            │
│        │                │                 │                      │
│  ┌─────▼──────┐  ┌──────▼──────┐  ┌──────▼────────┐            │
│  │services/   │  │services/    │  │services/      │            │
│  │graph       │  │agents       │  │report         │            │
│  └─────┬──────┘  └──────┬──────┘  └──────┬────────┘            │
│        │                │                 │                      │
│  ┌─────▼────────────────▼─────────────────▼────────┐            │
│  │              services/llm (OpenAI-compat)        │            │
│  └─────────────────────────────────────────────────┘            │
│                                                                  │
│  ┌─────────────────────────────────────────────────┐            │
│  │              storage/ (JSON files on disk)       │            │
│  └─────────────────────────────────────────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

---

## Layers

### `main.go`

Entry point. Loads config, registers routes, wraps the handler with `middleware.Recovery`, starts the HTTP server. Also calls `agentsvc.Global.Resume()` to re-attach to any simulation that was running before a restart.

### `config/config.go`

Single `Config` struct populated from environment variables at startup. All `services/` code reads from `config.Global`. Never passes config down as function parameters — it's a process-wide singleton.

### `api/middleware/recovery.go`

`Recovery(next http.Handler) http.Handler` — wraps every request handler in a `defer recover()`. On panic: logs the stack trace with a `request_id`, returns a JSON error body, and allows the process to continue serving other requests.

### `api/` — HTTP handlers

Thin layer. Handlers:
1. Parse the request body / path parameters
2. Call one service function
3. Return JSON

No business logic lives here. The router (`projects.go`) is a hand-written `net/http` router using string matching — no third-party router library.

**Routing convention:**
```
/api/v1/projects                    → handleListProjects / handleCreateProject
/api/v1/projects/:id/graph          → handleBuildGraph
/api/v1/projects/:id/agents         → handleListAgents / handleGenerateAgents
/api/v1/projects/:id/simulation/*   → simulation handlers
/api/v1/projects/:id/report/*       → report handlers
/api/v1/projects/:id/network        → handleGetNetwork
/api/v1/projects/:id/scenarios/*    → scenario comparison
/api/v1/seeds                       → handleListSeeds
```

### `storage/` — JSON file store

`storage.Store` provides `Get(collection, id, &v)`, `Set(collection, id, v)`, `Delete(collection, id)`, and `List(collection, &v)`. Data is stored as `{DATA_DIR}/{projectID}/{collection}/{id}.json`. No transactions — last write wins. Suitable for single-user or small team use.

### `services/llm/`

Two responsibilities:

**LLM calls** (`client.go`): `Chat(ctx, messages) (string, error)`. Uses `context.WithTimeout` per attempt, retries with exponential backoff. Reads `config.Global.LLMTimeoutSec` and `config.Global.LLMMaxRetries`.

**Embeddings** (`embed.go`): `Embed(ctx, text) ([]float32, error)`. Returns a vector for semantic similarity comparison. `CosineSimilarity(a, b []float32) float64`.

### `services/graph/`

**`graph.go`**: `BuildFromDocument(ctx, projectID, text)` — extracts entities and relationships from a document using the LLM, validates the result (≥3 nodes, ≥2 edges), falls back to `extractEntitiesSimple()` if validation fails, embeds each node/edge, and persists to storage.

**`tools.go`**: The 4 ReACT tools used during report generation:
- `InsightForge` — 2-hop GraphRAG, sub-query decomposition, semantic ranking
- `PanoramaSearch` — 3-hop GraphRAG, full graph overview
- `QuickSearch` — 1-hop, fast focused lookup
- `InterviewAgents` — semantic similarity to select agents, interviews each in-character

**GraphRAG hop traversal**: BFS from seed nodes, following edges in both directions, up to `config.Global.GraphMaxHops` hops. At each hop, ranks candidates by cosine similarity to the query embedding.

### `services/agents/`

**`profiles.go`**: `GenerateProfiles(ctx, projectID, nodes, simReq)` — calls the LLM once per entity batch to generate `OasisAgentProfile` structs. Detects agent type (individual vs. group) by relationship count and name keywords.

**`memory.go`**: `EpisodicMemory` (per-agent event log with embeddings), `BeliefStore` (per-agent belief map per topic), belief decay (`config.Global.BeliefDecayPerHour` per simulated hour), belief revision on contradiction (cosine similarity < `config.Global.ContradictionThreshold`).

**`engine.go`**: The simulation loop.
- `SimulationManager.Start(projectID, hours, platform, topic)` — launches `runLoop` in a goroutine
- `runLoop` — iterates hours 1..N, skipping dead hours (multiplier < `deadHourThreshold`), launches agent goroutines via a semaphore of size `workerPoolSize()`
- Each agent goroutine has `defer recover()` — panics are logged and the goroutine exits cleanly
- After each action, publishes a `SimEvent{Type:"action"}` to `Broadcaster`
- After each hour, publishes `SimEvent{Type:"hour"}`
- On completion, publishes `SimEvent{Type:"done"}`

**`broadcaster.go`** (embedded in engine.go): `SimulationBroadcaster` — a pub-sub hub. `Subscribe(projectID)` returns a buffered `chan SimEvent` (size 64) and a cancel function. `Publish(ev)` sends non-blocking to all active subscribers for the project.

### `services/report/`

**`react.go`**: `RunReport(ctx, projectID, simReq)` — the ReACT report generation pipeline:
1. `emit()` — appends to `Status.Progress` and calls `statuses.set()` to notify SSE subscribers
2. Calls the LLM to plan an outline (sections + descriptions)
3. For each section: calls tools until ≥ `MinToolCallsPerSection` evidence items gathered, then writes the section
4. Post-draft: checks mandatory sections (Timeline, Key Actors, Prediction Confidence, Divergence Points); appends any missing
5. `config.Global.ReportCritiqueIterations` critique passes: reviews the draft, adds scores, removes unsupported claims

**`stream.go`**: SSE subscriber map for report progress. `GetWatcher(projectID)` returns a channel that receives status updates. `handleReportStream` in `api/report.go` uses this to push events to the browser.

**`predictions.go`**: `ExtractPredictions(ctx, projectID, reportID, content)` — LLM extracts falsifiable predictions with confidence scores. `MarkOutcome(predID, correct)` records outcomes. `GetCalibrationScore(projectID)` computes Brier score and accuracy by confidence band.

### `services/network/`

`ComputeNetwork(projectID)` — builds a weighted directed graph from simulation actions (FOLLOW, REPOST, COMMENT targeting specific agents), runs PageRank (10 iterations, damping 0.85), detects stance clusters, returns `NetworkGraph` with D3.js-ready node/link arrays.

### `services/replay/`

`RecordFrame(projectID, hour, state)` — called after each simulation hour. `GetFrames(projectID)` returns all frames. `ExportJSON / ExportCSV / ExportMarkdown` convert frames to download-ready formats.

---

## Data model

All data lives under `{DATA_DIR}/{projectID}/`:

```
{DATA_DIR}/
└── {projectID}/
    ├── project.json
    ├── graph_nodes/
    │   └── {nodeID}.json
    ├── graph_edges/
    │   └── {edgeID}.json
    ├── agents/
    │   └── {agentID}.json
    ├── simulation_actions/
    │   └── {actionID}.json
    ├── simulation_state.json
    ├── simulation_history.json
    ├── replay_frames/
    │   └── {hour}.json
    ├── report_status.json
    ├── predictions/
    │   └── {predID}.json
    └── network.json
```

---

## SSE (Server-Sent Events)

Two SSE endpoints:

**`/api/v1/projects/:id/simulation/feed`**: Streams `SimEvent` objects as the simulation runs. Uses `SimulationBroadcaster.Subscribe()`. Sends a `keepalive` comment every 20 seconds to prevent proxy timeouts.

**`/api/v1/projects/:id/report/stream`**: Streams `report.Status` objects as the ReACT agent works. Uses `report.GetWatcher()`. Sends keepalive every 15 seconds.

Both endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and `Connection: keep-alive`.

---

## Concurrency model

- One `SimulationManager` goroutine per project (not per-request)
- Per-hour: up to `workerPoolSize()` agent goroutines run concurrently (semaphore pattern)
- Report generation: one goroutine per project; the HTTP handler returns immediately and the browser subscribes via SSE
- Broadcaster: uses `sync.RWMutex` to protect the subscriber map; channels are buffered (size 64) to avoid blocking the simulation loop

---

## Frontend

Five-step Svelte UI. State lives in two Svelte stores (`project.js`):
- `currentProject` — the selected project object
- `currentStep` — integer 0–5 (0 = project select)
- `simRequirement` — the simulation topic, shared between Step3 and Step4

The frontend communicates with the backend exclusively through `/api/v1/` — no direct file access or WebSocket. SSE connections are managed per-component with `onDestroy` cleanup.
