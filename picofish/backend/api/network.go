package api

import (
	"net/http"

	networksvc "picofish/services/network"
)

// handleGetNetwork returns the influence network for a project.
// GET /api/v1/projects/:id/network
func handleGetNetwork(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	edges := networksvc.GetAgentInfluenceScores(projectID)
	if edges == nil {
		edges = []networksvc.AgentInfluenceScore{}
	}

	d3, err := networksvc.GetD3Export(projectID)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"agents": edges,
		"d3":     d3,
	})
}
