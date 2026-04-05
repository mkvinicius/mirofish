package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	agentsvc "picofish/services/agents"
	"picofish/services/llm"
	reportsvc "picofish/services/report"
)

func handleGenerateReport(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		SimRequirement string `json:"sim_requirement"`
	}
	_ = json.NewDecoder(req.Body).Decode(&body)
	if body.SimRequirement == "" {
		body.SimRequirement = "Social simulation analysis"
	}
	status, err := reportsvc.Generate(req.Context(), projectID, body.SimRequirement)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusAccepted, status)
}

func handleGetReport(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	status := reportsvc.GetStatus(projectID)
	if status == nil {
		Err(w, http.StatusNotFound, "no report found")
		return
	}
	JSON(w, http.StatusOK, status)
}

// handleStreamReport streams report status updates via Server-Sent Events (SSE).
// The frontend connects once and receives live updates as the report is generated.
func handleStreamReport(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		Err(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	sendEvent := func(data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// Send current status immediately
	if s := reportsvc.GetStatus(projectID); s != nil {
		sendEvent(s)
		if s.Status == "completed" || s.Status == "error" {
			return
		}
	}

	ch, unwatch := reportsvc.Subscribe(projectID)
	defer unwatch()

	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(15 * time.Second) // keepalive
	defer ticker.Stop()

	for {
		select {
		case <-req.Context().Done():
			return
		case <-timeout:
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-ch:
			s := reportsvc.GetStatus(projectID)
			if s == nil {
				continue
			}
			sendEvent(s)
			if s.Status == "completed" || s.Status == "error" {
				return
			}
		}
	}
}

// handleExportReport returns the report content as a downloadable Markdown file.
func handleExportReport(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	status := reportsvc.GetStatus(projectID)
	if status == nil || status.Content == "" {
		Err(w, http.StatusNotFound, "no completed report found")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s.md"`, projectID[:8]))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, status.Content)
}

// handleSimHistory returns the list of past simulation runs for a project.
func handleSimHistory(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	history := agentsvc.GetSimHistory(projectID)
	if history == nil {
		history = []agentsvc.SimHistoryEntry{}
	}
	JSON(w, http.StatusOK, history)
}

func handleChat(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		SimRequirement string        `json:"sim_requirement"`
		Message        string        `json:"message"`
		History        []llm.Message `json:"history"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Message == "" {
		Err(w, http.StatusBadRequest, "message is required")
		return
	}
	if body.SimRequirement == "" {
		body.SimRequirement = "Social simulation analysis"
	}
	response, err := reportsvc.Chat(req.Context(), projectID, body.SimRequirement, body.Message, body.History)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"response": response})
}
