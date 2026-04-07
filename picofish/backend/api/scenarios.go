package api

import (
	"encoding/json"
	"net/http"

	scenariosvc "picofish/services/scenarios"
)

// handleCompareScenarios runs a multi-scenario comparison and returns the report.
// POST /api/v1/projects/:id/scenarios/compare
func handleCompareScenarios(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)

	var body struct {
		BaseTopic string                    `json:"base_topic"`
		Scenarios []scenariosvc.Scenario    `json:"scenarios"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		Err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.BaseTopic == "" {
		Err(w, http.StatusBadRequest, "base_topic is required")
		return
	}
	if len(body.Scenarios) < 2 {
		Err(w, http.StatusBadRequest, "at least 2 scenarios required")
		return
	}

	set := scenariosvc.ScenarioSet{
		ProjectID: projectID,
		BaseTopic: body.BaseTopic,
		Scenarios: body.Scenarios,
	}
	report, err := scenariosvc.RunComparison(set)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, report)
}
