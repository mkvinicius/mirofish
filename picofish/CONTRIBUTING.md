# Contributing to PicoFish

Thank you for your interest in contributing. PicoFish is deliberately small — please read this guide before opening a PR.

---

## Philosophy

- **No new dependencies.** The entire backend depends only on `github.com/google/uuid` and `github.com/joho/godotenv`. Keep it that way.
- **No bloat.** Every feature should make the simulation more faithful or the UX noticeably better. "Nice to have" is not enough.
- **Tests for invariants.** You don't need 100% coverage — you need tests for the logic that must not break (belief decay, hop traversal, agent type detection, mandatory report sections).

---

## How to add a new agent action type

Agent action types are defined in two places:

**1. Backend — `services/agents/engine.go`**

Find the `performAction()` function. Add a new `case` for your action type:
```go
case "MY_NEW_ACTION":
    content, err = agentDoMyThing(ctx, agent, feed, simReq)
```

Add the corresponding helper function. It must return `(string, error)`.

**2. Backend — tests**

If your action type affects the feed algorithm (echo chamber, stance scoring, etc.), add a test in `tests/simulation_test.go`.

**3. Frontend — `steps/Step3Simulation.svelte`**

Add your action type to the `actionColor()` function so it gets a distinct color in the live feed:
```js
'MY_NEW_ACTION': '#your-hex-color',
```

If it has a platform icon, add it to `platformIcon()`.

---

## How to add a new ReACT tool (Step 4)

Tools are defined in `services/graph/tools.go`. Each tool is a function with this signature:

```go
func myNewTool(ctx context.Context, projectID, query string, hops int) (string, error)
```

Then register it in `services/report/react.go` inside `buildToolMap()`:

```go
"MyNewTool": func(ctx context.Context, projectID, query string) (string, error) {
    return graphsvc.MyNewTool(ctx, projectID, query, 2)
},
```

Finally, add it to the system prompt in `runReACT()` so the LLM knows it exists and when to use it.

---

## How to add a new replay export format

Export formats are handled in `services/replay/export.go`. Add a new function:

```go
func ExportXML(projectID string) ([]byte, error) {
    frames, err := GetFrames(projectID)
    // ... marshal to your format
}
```

Then wire it in `api/simulation.go` inside `handleExportReplay()`:

```go
case "xml":
    data, err := replaysvc.ExportXML(projectID)
    w.Header().Set("Content-Type", "application/xml")
    // ...
```

---

## How to add a new platform

Platforms affect agent scheduling, action type distribution, and icon display.

**1. `services/agents/engine.go`**

- Add your platform name to the `platforms` slice inside `resolveplatforms()`
- Add its action type weights to the platform action map
- Add its hourly multiplier curve if it differs from the default

**2. Frontend — `steps/Step3Simulation.svelte`**

Add your platform to the `<select>` options and to `platformIcon()`.

---

## How to change a configuration parameter

All tunable parameters live in `config/config.go` in the `Config` struct. To add a new one:

1. Add the field to the `Config` struct with a descriptive name
2. Set the default in `Load()` using `getEnvInt()` or `getEnvFloat()`
3. Document it in `README.md` in the configuration table

Do not hardcode values anywhere in `services/`. Always read from `config.Global`.

---

## Running tests

```bash
cd backend
go test ./tests/... -v
go test ./tests/... -bench=. -benchmem
```

Tests do not require a running LLM. They use synthetic data for all logic tests.

---

## Submitting a PR

- One logical change per PR
- Include a test if you're touching simulation logic, agent behavior, or graph traversal
- `go build ./...` and `go test ./tests/...` must pass
- Keep the diff small — if it's over 500 lines, split it
