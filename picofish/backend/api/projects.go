package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"picofish/storage"
)

// RegisterSeeds registers the /api/v1/seeds endpoint.
func RegisterSeeds(r *Router) {
	r.Handle("GET", "/api/v1/seeds", handleListSeeds)
}

func RegisterProjects(r *Router) {
	r.Handle("POST", "/api/v1/projects", handleCreateProject)
	r.Handle("GET", "/api/v1/projects", handleListProjects)
	r.mux.HandleFunc("/api/v1/projects/", projectsSubrouter)
}

func projectsSubrouter(w http.ResponseWriter, req *http.Request) {
	setCORS(w)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// /api/v1/projects/{id}  DELETE
	// /api/v1/projects/{id}/graph  POST/GET
	// /api/v1/projects/{id}/agents/generate  POST
	// /api/v1/projects/{id}/agents  GET
	// /api/v1/projects/{id}/simulation/*  GET/POST
	// /api/v1/projects/{id}/report/*  GET/POST
	// /api/v1/projects/{id}/chat  POST
	// Phase 2:
	// /api/v1/projects/{id}/network  GET
	// /api/v1/projects/{id}/scenarios/compare  POST
	// /api/v1/projects/{id}/simulation/inject  POST
	// /api/v1/projects/{id}/simulation/replay  GET
	// /api/v1/projects/{id}/simulation/replay/export  GET
	// /api/v1/projects/{id}/simulation/replay/:hour  GET
	// /api/v1/projects/{id}/report/predictions  GET
	// /api/v1/projects/{id}/report/predictions/extract  POST
	// /api/v1/projects/{id}/report/predictions/:id/outcome  POST
	path := req.URL.Path

	switch {
	case isGraphBuild(path) && req.Method == http.MethodPost:
		handleBuildGraph(w, req)
	case isGraphNodes(path) && req.Method == http.MethodGet:
		handleGetNodes(w, req)
	case isGraphSearch(path) && req.Method == http.MethodGet:
		handleSearchGraph(w, req)
	case isAgentsGenerate(path) && req.Method == http.MethodPost:
		handleGenerateAgentProfiles(w, req)
	case isAgentsList(path) && req.Method == http.MethodGet:
		handleListAgents(w, req)
	case isSimStart(path) && req.Method == http.MethodPost:
		handleStartSimulation(w, req)
	case isSimStop(path) && req.Method == http.MethodPost:
		handleStopSimulation(w, req)
	case isSimStatus(path) && req.Method == http.MethodGet:
		handleGetSimulationStatus(w, req)
	case isSimActions(path) && req.Method == http.MethodGet:
		handleGetSimulationActions(w, req)
	// Phase 2: inject, replay
	case isSimFeed(path) && req.Method == http.MethodGet:
		handleSimFeed(w, req)
	case isSimInject(path) && req.Method == http.MethodPost:
		handleInjectEvent(w, req)
	case isReplayExport(path) && req.Method == http.MethodGet:
		handleExportReplay(w, req)
	case isReplayFrame(path) && req.Method == http.MethodGet:
		handleGetReplayFrame(w, req)
	case isReplay(path) && req.Method == http.MethodGet:
		handleGetReplay(w, req)
	// Report handlers
	case isReportGenerate(path) && req.Method == http.MethodPost:
		handleGenerateReport(w, req)
	case isReportStream(path) && req.Method == http.MethodGet:
		handleStreamReport(w, req)
	case isReportExport(path) && req.Method == http.MethodGet:
		handleExportReport(w, req)
	case isSimHistory(path) && req.Method == http.MethodGet:
		handleSimHistory(w, req)
	// Phase 2: predictions
	case isPredictionExtract(path) && req.Method == http.MethodPost:
		handleExtractPredictions(w, req)
	case isPredictionOutcome(path) && req.Method == http.MethodPost:
		handleMarkPredictionOutcome(w, req)
	case isPredictions(path) && req.Method == http.MethodGet:
		handleGetPredictions(w, req)
	case isReport(path) && req.Method == http.MethodGet:
		handleGetReport(w, req)
	case isChat(path) && req.Method == http.MethodPost:
		handleChat(w, req)
	// Phase 2: network, scenarios
	case isNetwork(path) && req.Method == http.MethodGet:
		handleGetNetwork(w, req)
	case isScenarios(path) && req.Method == http.MethodPost:
		handleCompareScenarios(w, req)
	case isProjectClone(path) && req.Method == http.MethodPost:
		handleCloneProject(w, req)
	case isProjectDelete(path) && req.Method == http.MethodDelete:
		handleDeleteProject(w, req)
	default:
		http.NotFound(w, req)
	}
}

// Path matchers
func isProjectDelete(p string) bool  { return countParts(p, "/api/v1/projects/") == 0 }
func isGraphBuild(p string) bool     { return endsWith(p, "/graph") }
func isGraphNodes(p string) bool     { return endsWith(p, "/graph/nodes") }
func isGraphSearch(p string) bool    { return endsWith(p, "/graph/search") }
func isAgentsGenerate(p string) bool { return endsWith(p, "/agents/generate") }
func isAgentsList(p string) bool     { return endsWith(p, "/agents") }
func isSimStart(p string) bool       { return endsWith(p, "/simulation/start") }
func isSimStop(p string) bool        { return endsWith(p, "/simulation/stop") }
func isSimStatus(p string) bool      { return endsWith(p, "/simulation/status") }
func isSimActions(p string) bool     { return endsWith(p, "/simulation/actions") }
func isReportGenerate(p string) bool { return endsWith(p, "/report/generate") }
func isReportStream(p string) bool   { return endsWith(p, "/report/stream") }
func isReportExport(p string) bool   { return endsWith(p, "/report/export") }
func isSimHistory(p string) bool     { return endsWith(p, "/simulation/history") }
func isReport(p string) bool {
	return endsWith(p, "/report") &&
		!endsWith(p, "/report/generate") &&
		!endsWith(p, "/report/stream") &&
		!endsWith(p, "/report/export")
}
func isChat(p string) bool { return endsWith(p, "/chat") }

// Phase 2+3 path matchers
func isSimFeed(p string) bool         { return endsWith(p, "/simulation/feed") }
func isSimInject(p string) bool       { return endsWith(p, "/simulation/inject") }
func isReplay(p string) bool          { return endsWith(p, "/simulation/replay") }
func isReplayExport(p string) bool    { return endsWith(p, "/simulation/replay/export") }
func isReplayFrame(p string) bool {
	// matches /simulation/replay/{n} where n is a number
	return containsStr(p, "/simulation/replay/") && !endsWith(p, "/simulation/replay/export")
}
func isNetwork(p string) bool    { return endsWith(p, "/network") }
func isScenarios(p string) bool  { return endsWith(p, "/scenarios/compare") }
func isPredictions(p string) bool {
	return endsWith(p, "/report/predictions") &&
		!endsWith(p, "/report/predictions/extract")
}
func isPredictionExtract(p string) bool { return endsWith(p, "/report/predictions/extract") }
func isPredictionOutcome(p string) bool { return endsWith(p, "/outcome") && containsStr(p, "/predictions/") }

func isProjectClone(p string) bool  { return endsWith(p, "/clone") }

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func endsWith(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

func countParts(path, prefix string) int {
	trimmed := path[len(prefix):]
	parts := 0
	for _, c := range trimmed {
		if c == '/' {
			parts++
		}
	}
	return parts
}

// extractProjectID extracts the project ID from paths like /api/v1/projects/{id}/...
func extractProjectID(path string) string {
	trimmed := trimPrefix(path, "/api/v1/projects/")
	idx := indexOf(trimmed, '/')
	if idx < 0 {
		return trimmed
	}
	return trimmed[:idx]
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Handlers

func handleCreateProject(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" {
		Err(w, http.StatusBadRequest, "name is required")
		return
	}
	id := uuid.NewString()
	r := storage.Record{
		"id":          id,
		"name":        body.Name,
		"description": body.Description,
		"status":      "created",
	}
	if err := storage.DB.Insert("projects", id, r); err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, r)
}

func handleListProjects(w http.ResponseWriter, req *http.Request) {
	records := storage.DB.QueryAll("projects")
	if records == nil {
		records = []storage.Record{}
	}
	JSON(w, http.StatusOK, records)
}

func handleDeleteProject(w http.ResponseWriter, req *http.Request) {
	id := extractProjectID(req.URL.Path)
	for _, col := range []string{"graph_nodes", "graph_edges", "agents", "simulation_actions", "reports", "agent_memories", "sim_history", "replay_frames", "predictions"} {
		_ = storage.DB.DeleteWhere(col, func(r storage.Record) bool {
			return storage.GetStr(r, "project_id") == id
		})
	}
	if err := storage.DB.Delete("projects", id); err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// handleCloneProject duplicates graph nodes + agents into a new project.
// Simulation results (actions, reports, replay frames) are NOT copied.
// POST /api/v1/projects/:id/clone
func handleCloneProject(w http.ResponseWriter, req *http.Request) {
	srcID := extractProjectID(req.URL.Path)
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(req.Body).Decode(&body)

	// Load source project
	src, ok := storage.DB.Get("projects", srcID)
	if !ok {
		Err(w, http.StatusNotFound, "project not found")
		return
	}
	srcName := storage.GetStr(src, "name")
	if body.Name == "" {
		body.Name = srcName + " (clone)"
	}

	newID := uuid.NewString()
	if err := storage.DB.Insert("projects", newID, storage.Record{
		"id":          newID,
		"name":        body.Name,
		"description": storage.GetStr(src, "description"),
		"status":      "created",
	}); err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Copy graph nodes
	nodes := storage.DB.QueryFunc("graph_nodes", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == srcID
	})
	for _, n := range nodes {
		newNodeID := uuid.NewString()
		cloned := make(storage.Record)
		for k, v := range n {
			cloned[k] = v
		}
		cloned["id"] = newNodeID
		cloned["project_id"] = newID
		// Update project_id inside "data" JSON
		cloned["data"] = replaceJSONField(storage.GetStr(n, "data"), "project_id", newID)
		_ = storage.DB.Insert("graph_nodes", newNodeID, cloned)
	}

	// Copy graph edges
	edges := storage.DB.QueryFunc("graph_edges", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == srcID
	})
	for _, e := range edges {
		newEdgeID := uuid.NewString()
		cloned := make(storage.Record)
		for k, v := range e {
			cloned[k] = v
		}
		cloned["id"] = newEdgeID
		cloned["project_id"] = newID
		cloned["data"] = replaceJSONField(storage.GetStr(e, "data"), "project_id", newID)
		_ = storage.DB.Insert("graph_edges", newEdgeID, cloned)
	}

	// Copy agent profiles
	agentRecords := storage.DB.QueryFunc("agents", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == srcID
	})
	for _, a := range agentRecords {
		newAgentID := uuid.NewString()
		cloned := make(storage.Record)
		for k, v := range a {
			cloned[k] = v
		}
		cloned["id"] = newAgentID
		cloned["project_id"] = newID
		cloned["data"] = replaceJSONField(storage.GetStr(a, "data"), "project_id", newID)
		_ = storage.DB.Insert("agents", newAgentID, cloned)
	}

	JSON(w, http.StatusCreated, map[string]interface{}{
		"id":             newID,
		"name":           body.Name,
		"cloned_from":    srcID,
		"nodes_copied":   len(nodes),
		"edges_copied":   len(edges),
		"agents_copied":  len(agentRecords),
	})
}

// replaceJSONField does a naive string replacement of a JSON field value.
// Used for updating project_id inside copied records' data blobs.
func replaceJSONField(data, field, newVal string) string {
	old := `"` + field + `":"`
	idx := 0
	for i := 0; i <= len(data)-len(old); i++ {
		if data[i:i+len(old)] == old {
			idx = i + len(old)
			end := idx
			for end < len(data) && data[end] != '"' {
				end++
			}
			return data[:idx] + newVal + data[end:]
		}
	}
	return data
}

// ── Seeds ──────────────────────────────────────────────────────────────────

// Seed is a pre-built example scenario.
type Seed struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	SeedText             string `json:"seed_text"`
	SuggestedHours       int    `json:"suggested_hours"`
	SuggestedAgentCount  int    `json:"suggested_agent_count"`
}

var builtinSeeds = []Seed{
	{
		ID:   "financial_crisis",
		Name: "2008 Financial Crisis",
		Description: "Simulate public opinion dynamics around a major bank collapse",
		SeedText: `Major investment bank files for bankruptcy after mounting subprime mortgage losses. Stock markets plunge 7% in a single day. Government considers a massive bailout package worth hundreds of billions. Public anger grows as ordinary citizens face job losses and pension cuts while executives keep bonuses. Unemployment fears spread to manufacturing and retail sectors. Media coverage intensifies with 24-hour news cycles. Political parties take sharply opposing stances: conservatives oppose the bailout as corporate welfare while progressives demand strict conditions and executive pay caps. Small business owners struggle to access credit. International markets show contagion as European banks reveal their own exposure. Central banks coordinate emergency interest rate cuts.`,
		SuggestedHours:      24,
		SuggestedAgentCount: 30,
	},
	{
		ID:   "climate_policy",
		Name: "Climate Legislation Debate",
		Description: "Public reaction to sweeping new climate legislation",
		SeedText: `Government announces landmark climate legislation requiring 50% reduction in carbon emissions within 10 years. Industrial sector warns of massive job losses in coal, oil, and gas regions. Environmental groups celebrate the measure as long overdue. Economists debate whether the green transition will create more jobs than it destroys. Rural communities dependent on fossil fuel industries protest in capital cities. Tech companies announce accelerated clean energy investments. Opposition party vows to repeal the law if elected. Scientists warn the targets are still insufficient to limit warming to 1.5°C. Developing nations demand wealthy countries provide climate finance. Young climate activists call for even more aggressive action while unions negotiate transition support packages for displaced workers.`,
		SuggestedHours:      48,
		SuggestedAgentCount: 40,
	},
	{
		ID:   "product_launch",
		Name: "Viral Tech Product Launch",
		Description: "Public reaction to a transformative consumer technology release",
		SeedText: `Major tech company unveils revolutionary AI-powered device that replaces smartphones with a wearable screen-free assistant. Price set at $799. Early reviews are polarized — tech enthusiasts call it the future of human-computer interaction while privacy advocates warn of unprecedented surveillance potential. Pre-orders sell out in 3 hours. Competitor stocks fall sharply. Content creators rush to produce first impressions videos. Disability advocates praise the hands-free interface. School districts debate whether to ban the device in classrooms. Cybersecurity researchers warn the always-on microphone creates new attack vectors. Retail workers strike demanding higher wages after announcement of AI-powered store automation. The device's neural interface raises ethical questions about cognitive liberty and data ownership.`,
		SuggestedHours:      12,
		SuggestedAgentCount: 25,
	},
}

func handleListSeeds(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, builtinSeeds)
}
