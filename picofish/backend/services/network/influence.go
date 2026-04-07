// Package network computes agent influence networks from simulation data.
// Builds weighted directed graphs, scores agents via PageRank, detects
// stance clusters, and exports D3.js-compatible JSON for visualization.
package network

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"picofish/services/agents"
)

// ── Types ──────────────────────────────────────────────────────────────────

// AgentInfluenceScore holds a single agent's computed network metrics.
type AgentInfluenceScore struct {
	AgentID    string  `json:"agent_id"`
	AgentName  string  `json:"agent_name"`
	PageRank   float64 `json:"page_rank"`    // 0–1, higher = more influential
	OutDegree  int     `json:"out_degree"`   // edges going out (actions taken)
	InDegree   int     `json:"in_degree"`    // edges coming in (received interactions)
	TotalWeight float64 `json:"total_weight"` // sum of incoming edge weights
	Cluster    string  `json:"cluster"`      // stance cluster label
}

// Cluster groups agents by detected stance similarity.
type Cluster struct {
	Label   string   `json:"label"`
	Agents  []string `json:"agents"` // agent IDs
	Density float64  `json:"density"` // internal edge density 0–1
}

// InfluenceNetwork is the full computed network for a project.
type InfluenceNetwork struct {
	ProjectID string                 `json:"project_id"`
	Agents    []AgentInfluenceScore  `json:"agents"`
	Clusters  []Cluster              `json:"clusters"`
	EdgeCount int                    `json:"edge_count"`
	D3Data    D3Graph                `json:"d3"`
}

// D3Graph is the D3.js force-directed graph export format.
type D3Graph struct {
	Nodes []D3Node `json:"nodes"`
	Links []D3Link `json:"links"`
}

type D3Node struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	PageRank  float64 `json:"pageRank"`
	Cluster   string  `json:"cluster"`
	InDegree  int     `json:"inDegree"`
	OutDegree int     `json:"outDegree"`
	// radius in D3 = 5 + pageRank * 30
	Radius float64 `json:"radius"`
}

type D3Link struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
	Action string  `json:"action"`
}

// ── Build ──────────────────────────────────────────────────────────────────

// Build constructs an InfluenceNetwork from raw influence edges + agent profiles.
func Build(projectID string, edges []*agents.InfluenceEdge, profiles []*agents.OasisAgentProfile) *InfluenceNetwork {
	if len(profiles) == 0 {
		return &InfluenceNetwork{ProjectID: projectID}
	}

	// Build agent ID→name map
	agentName := make(map[string]string, len(profiles))
	agentStance := make(map[string]string, len(profiles))
	for _, p := range profiles {
		agentName[p.ID] = p.Name
		agentStance[p.ID] = p.Stance
	}

	// Aggregate edges: sum weights per (from,to) pair
	aggEdges := make(map[edgeMapKey]float64)
	edgeAction := make(map[edgeMapKey]string)
	for _, e := range edges {
		k := edgeMapKey{e.FromAgentID, e.ToAgentID}
		aggEdges[k] += e.Weight
		if _, ok := edgeAction[k]; !ok {
			edgeAction[k] = e.ActionType
		}
	}

	// Compute in/out degree and total incoming weight per agent
	inDeg := make(map[string]int)
	outDeg := make(map[string]int)
	inWeight := make(map[string]float64)
	for k, w := range aggEdges {
		outDeg[k.from]++
		inDeg[k.to]++
		inWeight[k.to] += w
	}

	// PageRank — 10 iterations, damping 0.85
	pageRank := pageRank(profiles, aggEdges, 10, 0.85)

	// Cluster agents by stance (greedy: group all same-stance agents together)
	clusters := buildClusters(profiles, aggEdges, agentStance)

	// Build scores slice
	scores := make([]AgentInfluenceScore, 0, len(profiles))
	for _, p := range profiles {
		scores = append(scores, AgentInfluenceScore{
			AgentID:     p.ID,
			AgentName:   p.Name,
			PageRank:    pageRank[p.ID],
			OutDegree:   outDeg[p.ID],
			InDegree:    inDeg[p.ID],
			TotalWeight: inWeight[p.ID],
			Cluster:     agentStance[p.ID],
		})
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].PageRank > scores[j].PageRank
	})

	// Build D3 graph
	d3Nodes := make([]D3Node, 0, len(profiles))
	for _, s := range scores {
		d3Nodes = append(d3Nodes, D3Node{
			ID:        s.AgentID,
			Name:      agentName[s.AgentID],
			PageRank:  s.PageRank,
			Cluster:   s.Cluster,
			InDegree:  s.InDegree,
			OutDegree: s.OutDegree,
			Radius:    5.0 + s.PageRank*30.0,
		})
	}

	d3Links := make([]D3Link, 0, len(aggEdges))
	for k, w := range aggEdges {
		d3Links = append(d3Links, D3Link{
			Source: k.from,
			Target: k.to,
			Weight: roundF(w, 3),
			Action: edgeAction[k],
		})
	}

	return &InfluenceNetwork{
		ProjectID: projectID,
		Agents:    scores,
		Clusters:  clusters,
		EdgeCount: len(aggEdges),
		D3Data:    D3Graph{Nodes: d3Nodes, Links: d3Links},
	}
}

// ── PageRank ───────────────────────────────────────────────────────────────

type edgeMapKey = struct{ from, to string }

func pageRank(profiles []*agents.OasisAgentProfile, edges map[edgeMapKey]float64, iterations int, damping float64) map[string]float64 {
	n := float64(len(profiles))
	if n == 0 {
		return nil
	}

	pr := make(map[string]float64, len(profiles))
	for _, p := range profiles {
		pr[p.ID] = 1.0 / n
	}

	// Build adjacency: outgoing total weight per node
	outTotal := make(map[string]float64)
	for k, w := range edges {
		outTotal[k.from] += w
	}

	for iter := 0; iter < iterations; iter++ {
		next := make(map[string]float64, len(profiles))
		for _, p := range profiles {
			next[p.ID] = (1.0 - damping) / n
		}
		for k, w := range edges {
			if outTotal[k.from] > 0 {
				next[k.to] += damping * pr[k.from] * (w / outTotal[k.from])
			}
		}
		pr = next
	}

	// Normalize to [0, 1]
	maxPR := 0.0
	for _, v := range pr {
		if v > maxPR {
			maxPR = v
		}
	}
	if maxPR > 0 {
		for k := range pr {
			pr[k] /= maxPR
		}
	}
	return pr
}

// ── Clusters ───────────────────────────────────────────────────────────────

func buildClusters(profiles []*agents.OasisAgentProfile, edges map[edgeMapKey]float64, stance map[string]string) []Cluster {
	// Group agents by stance
	groups := make(map[string][]string)
	for _, p := range profiles {
		s := stance[p.ID]
		if s == "" {
			s = "neutral"
		}
		groups[s] = append(groups[s], p.ID)
	}

	clusters := make([]Cluster, 0, len(groups))
	for label, members := range groups {
		memberSet := make(map[string]bool, len(members))
		for _, id := range members {
			memberSet[id] = true
		}

		// Count internal edges
		internalEdges := 0
		for k := range edges {
			if memberSet[k.from] && memberSet[k.to] {
				internalEdges++
			}
		}
		maxPossible := len(members) * (len(members) - 1)
		density := 0.0
		if maxPossible > 0 {
			density = float64(internalEdges) / float64(maxPossible)
		}

		clusters = append(clusters, Cluster{
			Label:   label,
			Agents:  members,
			Density: roundF(density, 3),
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].Agents) > len(clusters[j].Agents)
	})
	return clusters
}

// ── Export ─────────────────────────────────────────────────────────────────

// ToJSON serializes the network to pretty JSON.
func (n *InfluenceNetwork) ToJSON() ([]byte, error) {
	return json.MarshalIndent(n, "", "  ")
}

// ── Helpers ────────────────────────────────────────────────────────────────

func roundF(f float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(f*pow) / pow
}

// GetAgentInfluenceScores returns sorted agent scores for a project.
// Convenience wrapper used by API handlers.
func GetAgentInfluenceScores(projectID string) []AgentInfluenceScore {
	edges := agents.Global.GetInfluenceNetwork(projectID)
	profiles, err := agents.LoadProfiles(projectID)
	if err != nil || len(profiles) == 0 {
		return nil
	}
	net := Build(projectID, edges, profiles)
	return net.Agents
}

// GetD3Export returns the D3.js graph data for a project.
func GetD3Export(projectID string) (*D3Graph, error) {
	edges := agents.Global.GetInfluenceNetwork(projectID)
	profiles, err := agents.LoadProfiles(projectID)
	if err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}
	net := Build(projectID, edges, profiles)
	return &net.D3Data, nil
}
