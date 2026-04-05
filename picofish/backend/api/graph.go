package api

import (
	"encoding/json"
	"net/http"
	"strings"

	graphsvc "picofish/services/graph"
)

func handleBuildGraph(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var body struct {
		Document string `json:"document"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Document == "" {
		Err(w, http.StatusBadRequest, "document is required")
		return
	}
	summary, err := graphsvc.BuildFromDocument(req.Context(), projectID, body.Document)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, summary)
}

func handleGetNodes(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	var types []string
	if t := req.URL.Query().Get("types"); t != "" {
		for _, v := range strings.Split(t, ",") {
			if s := strings.TrimSpace(v); s != "" {
				types = append(types, s)
			}
		}
	}
	nodes, err := graphsvc.GetNodes(projectID, types)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		nodes = []graphsvc.Node{}
	}
	JSON(w, http.StatusOK, nodes)
}

func handleSearchGraph(w http.ResponseWriter, req *http.Request) {
	projectID := extractProjectID(req.URL.Path)
	q := req.URL.Query().Get("q")
	if q == "" {
		Err(w, http.StatusBadRequest, "q is required")
		return
	}
	nodes, err := graphsvc.Search(projectID, q)
	if err != nil {
		Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		nodes = []graphsvc.Node{}
	}
	JSON(w, http.StatusOK, nodes)
}
