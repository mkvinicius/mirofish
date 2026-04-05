package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"picofish/storage"
)

func RegisterProjects(r *Router) {
	r.Handle("POST", "/api/v1/projects", handleCreateProject)
	r.Handle("GET", "/api/v1/projects", handleListProjects)
	// DELETE uses prefix handler
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
	case isReportGenerate(path) && req.Method == http.MethodPost:
		handleGenerateReport(w, req)
	case isReport(path) && req.Method == http.MethodGet:
		handleGetReport(w, req)
	case isChat(path) && req.Method == http.MethodPost:
		handleChat(w, req)
	case isProjectDelete(path) && req.Method == http.MethodDelete:
		handleDeleteProject(w, req)
	default:
		http.NotFound(w, req)
	}
}

// Path matchers
func isProjectDelete(p string) bool { return countParts(p, "/api/v1/projects/") == 1 }
func isGraphBuild(p string) bool    { return endsWith(p, "/graph") }
func isGraphNodes(p string) bool    { return endsWith(p, "/graph/nodes") }
func isGraphSearch(p string) bool   { return endsWith(p, "/graph/search") }
func isAgentsGenerate(p string) bool { return endsWith(p, "/agents/generate") }
func isAgentsList(p string) bool    { return endsWith(p, "/agents") }
func isSimStart(p string) bool      { return endsWith(p, "/simulation/start") }
func isSimStop(p string) bool       { return endsWith(p, "/simulation/stop") }
func isSimStatus(p string) bool     { return endsWith(p, "/simulation/status") }
func isSimActions(p string) bool    { return endsWith(p, "/simulation/actions") }
func isReportGenerate(p string) bool { return endsWith(p, "/report/generate") }
func isReport(p string) bool        { return endsWith(p, "/report") && !endsWith(p, "/report/generate") }
func isChat(p string) bool          { return endsWith(p, "/chat") }

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
	for _, col := range []string{"graph_nodes", "graph_edges", "agents", "simulation_actions", "reports"} {
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
