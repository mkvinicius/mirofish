package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	agentsvc "picofish/services/agents"
	graphsvc "picofish/services/graph"
	reportsvc "picofish/services/report"
	replaysvc "picofish/services/replay"
)

func handleGenerateAgentProfiles(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		EntityTypes    []string `json:"entity_types"`
		SimRequirement string   `json:"sim_requirement"`
	}
	_ = json.NewDecoder(req.Body).Decode(&body)

	nodes, err := graphsvc.GetNodes(projectID, body.EntityTypes)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(nodes) == 0 {
		Err(w, http.StatusBadRequest, "no nodes found — build graph first (Step 1)")
		return
	}

	profiles, err := agentsvc.GenerateProfiles(req.Context(), projectID, nodes, body.SimRequirement)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"count": len(profiles), "agents": profiles})
}

func handleListAgents(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	profiles, err := agentsvc.LoadProfiles(projectID)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if profiles == nil {
		profiles = []*agentsvc.OasisAgentProfile{}
	}
	JSON(w, http.StatusOK, profiles)
}

func handleStartSimulation(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		TotalHours int    `json:"total_hours"`
		Platform   string `json:"platform"`
		Topic      string `json:"topic"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Topic == "" {
		Err(w, http.StatusBadRequest, "topic is required")
		return
	}
	if body.TotalHours <= 0 {
		body.TotalHours = 24
	}
	if body.TotalHours > 168 {
		body.TotalHours = 168
	}
	if body.Platform == "" {
		body.Platform = "both"
	}
	if err := agentsvc.Global.Start(projectID, body.TotalHours, body.Platform, body.Topic); err != nil {
		Err(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"status":      "started",
		"total_hours": body.TotalHours,
		"platform":    body.Platform,
		"topic":       body.Topic,
	})
}

func handleStopSimulation(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	agentsvc.Global.Stop(projectID)
	JSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func handleGetSimulationStatus(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	state := agentsvc.Global.GetState(projectID)
	if state == nil {
		JSON(w, http.StatusOK, map[string]string{"status": "not_started"})
		return
	}
	JSON(w, http.StatusOK, state)
}

func handleGetSimulationActions(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	actions, _ := agentsvc.GetRecentActions(projectID, 50)
	if actions == nil {
		actions = []*agentsvc.AgentAction{}
	}
	JSON(w, http.StatusOK, actions)
}

// handleInjectEvent injects a mid-simulation event (breaking news / narrative shift).
// POST /api/v1/projects/:id/simulation/inject
func handleInjectEvent(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body agentsvc.InjectionEvent
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Content == "" {
		Err(w, http.StatusBadRequest, "content is required")
		return
	}
	if body.Visibility == "" {
		body.Visibility = "public"
	}
	agentsvc.Global.Inject(projectID, body)
	JSON(w, http.StatusOK, map[string]interface{}{
		"status":     "queued",
		"hour":       body.Hour,
		"visibility": body.Visibility,
	})
}

// handleGetReplay returns all replay frames for a project.
// GET /api/v1/projects/:id/simulation/replay
func handleGetReplay(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	frames, err := replaysvc.GetFrames(projectID)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"frame_count": len(frames),
		"frames":      frames,
	})
}

// handleExportReplay exports replay data in json/csv/markdown format.
// GET /api/v1/projects/:id/simulation/replay/export?format=json|csv|md
func handleExportReplay(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	format := req.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "csv":
		data, err := replaysvc.ExportCSV(projectID)
		if err != nil {
			Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="replay-%s.csv"`, projectID[:8]))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case "md", "markdown":
		data, err := replaysvc.ExportMarkdown(projectID)
		if err != nil {
			Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="replay-%s.md"`, projectID[:8]))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	default:
		data, err := replaysvc.ExportJSON(projectID)
		if err != nil {
			Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// handleGetPredictions returns all extracted predictions for a project.
// GET /api/v1/projects/:id/report/predictions
func handleGetPredictions(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	claims, err := reportsvc.GetPredictions(projectID)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if claims == nil {
		claims = []*reportsvc.PredictionClaim{}
	}
	cal, _ := reportsvc.GetCalibrationScore(projectID)
	JSON(w, http.StatusOK, map[string]interface{}{
		"predictions": claims,
		"calibration": cal,
	})
}

// handleMarkPredictionOutcome records an outcome for a prediction.
// POST /api/v1/projects/:id/report/predictions/:predid/outcome
func handleMarkPredictionOutcome(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Correct bool `json:"correct"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		Err(w, http.StatusBadRequest, "correct (bool) is required")
		return
	}
	// Extract prediction ID from path: .../predictions/{predid}/outcome
	path := req.URL.Path
	// Find "predictions/" in path
	const marker = "/predictions/"
	idx := 0
	for i := 0; i <= len(path)-len(marker); i++ {
		if path[i:i+len(marker)] == marker {
			idx = i + len(marker)
			break
		}
	}
	predID := path[idx:]
	if end := indexOf(predID, '/'); end >= 0 {
		predID = predID[:end]
	}
	if predID == "" {
		Err(w, http.StatusBadRequest, "prediction ID missing")
		return
	}
	if err := reportsvc.MarkOutcome(predID, body.Correct); err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"prediction_id": predID,
		"correct":       body.Correct,
	})
}

// handleExtractPredictions asks the LLM to extract predictions from the latest report.
// POST /api/v1/projects/:id/report/predictions/extract
func handleExtractPredictions(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	status := reportsvc.GetStatus(projectID)
	if status == nil || status.Content == "" {
		Err(w, http.StatusNotFound, "no completed report found — generate report first")
		return
	}
	claims, err := reportsvc.ExtractPredictions(req.Context(), projectID, status.ReportID, status.Content)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"extracted": len(claims),
		"claims":    claims,
	})
}

// handleGetReplayFrame returns a single replay frame by hour.
// GET /api/v1/projects/:id/simulation/replay/:hour
func handleGetReplayFrame(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	path := req.URL.Path
	// Extract hour from path end
	const marker = "/replay/"
	idx := 0
	for i := 0; i <= len(path)-len(marker); i++ {
		if path[i:i+len(marker)] == marker {
			idx = i + len(marker)
			break
		}
	}
	hourStr := path[idx:]
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		Err(w, http.StatusBadRequest, "invalid hour")
		return
	}
	frame, err := replaysvc.GetFrame(projectID, hour)
	if err != nil {
		Err(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, frame)
}
