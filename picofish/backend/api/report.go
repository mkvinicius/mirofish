package api

import (
	"encoding/json"
	"net/http"

	"picofish/services/llm"
	reportsvc "picofish/services/report"
)

func handleGenerateReport(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	status, err := reportsvc.Generate(req.Context(), projectID)
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
		Message string        `json:"message"`
		History []llm.Message `json:"history"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Message == "" {
		Err(w, http.StatusBadRequest, "message is required")
		return
	}
	response, err := reportsvc.Chat(req.Context(), projectID, body.Message, body.History)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"response": response})
}
