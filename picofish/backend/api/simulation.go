package api

import (
	"encoding/json"
	"net/http"

	agentsvc "picofish/services/agents"
	graphsvc "picofish/services/graph"
)

func handleGenerateAgentProfiles(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		EntityTypes []string `json:"entity_types"`
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

	profiles, err := agentsvc.GenerateProfiles(req.Context(), projectID, nodes)
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
		profiles = []agentsvc.AgentProfile{}
	}
	JSON(w, http.StatusOK, profiles)
}

func handleStartSimulation(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		Rounds int    `json:"rounds"`
		Topic  string `json:"topic"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Topic == "" {
		Err(w, http.StatusBadRequest, "topic is required")
		return
	}
	if body.Rounds <= 0 {
		body.Rounds = 5
	}
	if body.Rounds > 20 {
		body.Rounds = 20
	}
	if err := agentsvc.Global.Start(projectID, body.Rounds, body.Topic); err != nil {
		Err(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"status": "started", "rounds": body.Rounds, "topic": body.Topic})
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
	actions, err := agentsvc.GetRecentActions(projectID, 50)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if actions == nil {
		actions = []agentsvc.Action{}
	}
	JSON(w, http.StatusOK, actions)
}
