package api

import (
	"encoding/json"
	"net/http"

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
