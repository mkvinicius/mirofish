// Package graph implements a knowledge graph with semantic vector search,
// replacing Zep Cloud with a fully local, embedding-powered store.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"picofish/services/llm"
	"picofish/storage"
)

// ── Data types ─────────────────────────────────────────────────────────────

type Node struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	Type      string            `json:"type"`       // entity type
	Name      string            `json:"name"`
	Labels    []string          `json:"labels"`     // ["Entity", "Person"]
	Summary   string            `json:"summary"`
	Attrs     map[string]string `json:"attributes"`
	Embedding []float64         `json:"embedding,omitempty"`
	CreatedAt string            `json:"created_at"`
}

type Edge struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	SourceID       string    `json:"source_id"`
	TargetID       string    `json:"target_id"`
	SourceName     string    `json:"source_name"`
	TargetName     string    `json:"target_name"`
	Relation       string    `json:"relation"`
	Fact           string    `json:"fact"`           // human-readable fact
	Embedding      []float64 `json:"embedding,omitempty"`
	ValidAt        string    `json:"valid_at"`       // temporal: when fact became valid
	InvalidAt      string    `json:"invalid_at"`     // temporal: when fact became invalid
	ExpiredAt      string    `json:"expired_at"`     // when overridden by simulation update
	CreatedAt      string    `json:"created_at"`
}

func (e *Edge) IsActive() bool  { return e.InvalidAt == "" && e.ExpiredAt == "" }
func (e *Edge) IsExpired() bool { return e.ExpiredAt != "" }

type GraphSummary struct {
	ProjectID   string   `json:"project_id"`
	NodeCount   int      `json:"node_count"`
	EdgeCount   int      `json:"edge_count"`
	EntityTypes []string `json:"entity_types"`
}

// ── Graph construction ─────────────────────────────────────────────────────

// BuildFromDocument extracts an ontology, entities, and relations from text
// using the LLM, generates embeddings for each, and persists to the store.
func BuildFromDocument(ctx context.Context, projectID, document string) (*GraphSummary, error) {
	// Step 1: ontology
	ont, err := generateOntology(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("ontology: %w", err)
	}

	// Step 2: entity + relation extraction
	nodes, edges, err := extractEntities(ctx, projectID, document, ont)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	// Step 3: generate embeddings concurrently (best-effort)
	embedNodes(ctx, nodes)
	embedEdges(ctx, edges)

	// Step 4: persist
	for i := range nodes {
		if err := saveNode(&nodes[i]); err != nil {
			return nil, err
		}
	}
	nodeByName := make(map[string]*Node)
	for i := range nodes {
		nodeByName[nodes[i].Name] = &nodes[i]
	}
	for i := range edges {
		// Resolve names
		if src, ok := nodeByName[edges[i].SourceName]; ok {
			edges[i].SourceID = src.ID
		}
		if tgt, ok := nodeByName[edges[i].TargetName]; ok {
			edges[i].TargetID = tgt.ID
		}
		if edges[i].SourceID == "" || edges[i].TargetID == "" {
			continue // skip unresolved edges
		}
		if err := saveEdge(&edges[i]); err != nil {
			return nil, err
		}
	}

	return &GraphSummary{
		ProjectID:   projectID,
		NodeCount:   len(nodes),
		EdgeCount:   len(edges),
		EntityTypes: ont.EntityTypes,
	}, nil
}

type ontology struct {
	EntityTypes   []string `json:"entity_types"`
	RelationTypes []string `json:"relation_types"`
}

func generateOntology(ctx context.Context, document string) (*ontology, error) {
	prompt := fmt.Sprintf(`Analyze this document and define a knowledge graph ontology.
Return ONLY valid JSON:
{"entity_types": ["Type1","Type2",...], "relation_types": ["RELATION1","RELATION2",...]}

Rules:
- 8-15 specific entity types relevant to the content
- Extract granular individual entities — specific people, named organizations, distinct user personas (e.g. 'Small Clinic Manager', 'Independent Physician', 'Insurance Company Executive'), and concrete concepts. Prefer many specific entities over few generic ones.
- 4-8 relation types in UPPER_SNAKE_CASE
- No explanation, just JSON

Document:
%s`, trunc(document, 3000))

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.2))
	if err != nil {
		return nil, err
	}
	var ont ontology
	if err := llm.ParseJSON(resp, &ont); err != nil {
		return nil, fmt.Errorf("parse ontology: %w", err)
	}
	return &ont, nil
}

func extractEntities(ctx context.Context, projectID, document string, ont *ontology) ([]Node, []Edge, error) {
	// Step A: extract entities only (smaller response, no truncation)
	entityPrompt := fmt.Sprintf(`Extract ALL distinct entities from this document.

Entity types available: %s

Instructions:
- Extract every distinct entity mentioned or implied.
- For personas/roles, create individual entries for each distinct perspective.
- Aim for 15-30 entities if the document supports it.
- Include implicit stakeholders who would be affected.
- Keep distinct roles separate — do not merge.

Return ONLY valid JSON:
{"entities": [{"type":"TypeName","name":"Entity Name","summary":"2-3 sentence description","attributes":{"key":"value"}}]}

Document:
%s`,
		strings.Join(ont.EntityTypes, ", "),
		trunc(document, 5000))

	respA, err := llm.Chat(ctx, []llm.Message{llm.User(entityPrompt)},
		llm.WithTemperature(0.2), llm.WithMaxTokens(8000))
	if err != nil {
		return nil, nil, err
	}

	var rawEntities struct {
		Entities []struct {
			Type       string            `json:"type"`
			Name       string            `json:"name"`
			Summary    string            `json:"summary"`
			Attributes map[string]string `json:"attributes"`
		} `json:"entities"`
	}
	if err := llm.ParseJSON(respA, &rawEntities); err != nil {
		return nil, nil, fmt.Errorf("parse entities: %w", err)
	}

	now := time.Now().Format(time.RFC3339)
	var nodes []Node
	var entityNames []string
	for _, e := range rawEntities.Entities {
		if e.Name == "" {
			continue
		}
		nodes = append(nodes, Node{
			ID:        uuid.NewString(),
			ProjectID: projectID,
			Type:      e.Type,
			Name:      e.Name,
			Labels:    []string{"Entity", e.Type},
			Summary:   e.Summary,
			Attrs:     e.Attributes,
			CreatedAt: now,
		})
		entityNames = append(entityNames, e.Name)
	}

	// Step B: extract relationships between known entities (separate call)
	relPrompt := fmt.Sprintf(`Given these entities from a document, extract the relationships between them.

Entities: %s
Relation types available: %s

Return ONLY valid JSON:
{"relations": [{"source":"Entity A","target":"Entity B","relation":"RELATION_TYPE","fact":"Full sentence stating the relationship"}]}

Document:
%s`,
		strings.Join(entityNames, ", "),
		strings.Join(ont.RelationTypes, ", "),
		trunc(document, 4000))

	respB, err := llm.Chat(ctx, []llm.Message{llm.User(relPrompt)},
		llm.WithTemperature(0.2), llm.WithMaxTokens(6000))
	if err != nil {
		// Relations are optional — return nodes only if this fails
		return nodes, nil, nil
	}

	var rawRelations struct {
		Relations []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			Relation string `json:"relation"`
			Fact     string `json:"fact"`
		} `json:"relations"`
	}
	_ = llm.ParseJSON(respB, &rawRelations) // best-effort

	var edges []Edge
	for _, r := range rawRelations.Relations {
		fact := r.Fact
		if fact == "" {
			fact = fmt.Sprintf("%s %s %s", r.Source, strings.ToLower(r.Relation), r.Target)
		}
		edges = append(edges, Edge{
			ID:         uuid.NewString(),
			ProjectID:  projectID,
			SourceName: r.Source,
			TargetName: r.Target,
			Relation:   r.Relation,
			Fact:       fact,
			ValidAt:    now,
			CreatedAt:  now,
		})
	}

	return nodes, edges, nil
}

// ── Embedding generation ───────────────────────────────────────────────────

func embedNodes(ctx context.Context, nodes []Node) {
	texts := make([]string, len(nodes))
	for i := range nodes {
		texts[i] = fmt.Sprintf("%s: %s. %s", nodes[i].Type, nodes[i].Name, nodes[i].Summary)
	}
	embs, err := llm.EmbedBatch(ctx, texts)
	if err != nil {
		// fallback: individual
		for i := range nodes {
			if emb, e2 := llm.Embed(ctx, texts[i]); e2 == nil {
				nodes[i].Embedding = emb
			}
		}
		return
	}
	for i := range nodes {
		if i < len(embs) {
			nodes[i].Embedding = embs[i]
		}
	}
}

func embedEdges(ctx context.Context, edges []Edge) {
	texts := make([]string, len(edges))
	for i := range edges {
		texts[i] = edges[i].Fact
	}
	embs, err := llm.EmbedBatch(ctx, texts)
	if err != nil {
		for i := range edges {
			if emb, e2 := llm.Embed(ctx, edges[i].Fact); e2 == nil {
				edges[i].Embedding = emb
			}
		}
		return
	}
	for i := range edges {
		if i < len(embs) {
			edges[i].Embedding = embs[i]
		}
	}
}

// ── Persistence ────────────────────────────────────────────────────────────

func saveNode(n *Node) error {
	b, _ := json.Marshal(n)
	return storage.DB.Insert("graph_nodes", n.ID, storage.Record{
		"id": n.ID, "project_id": n.ProjectID,
		"type": n.Type, "name": n.Name,
		"data": string(b),
	})
}

func saveEdge(e *Edge) error {
	b, _ := json.Marshal(e)
	return storage.DB.Insert("graph_edges", e.ID, storage.Record{
		"id": e.ID, "project_id": e.ProjectID,
		"source_id": e.SourceID, "target_id": e.TargetID,
		"relation": e.Relation,
		"data": string(b),
	})
}

// ── Query functions ────────────────────────────────────────────────────────

// GetNodes returns all nodes for a project, optionally filtered by type.
func GetNodes(projectID string, types []string) ([]Node, error) {
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}
	records := storage.DB.QueryFunc("graph_nodes", func(r storage.Record) bool {
		if storage.GetStr(r, "project_id") != projectID {
			return false
		}
		if len(typeSet) > 0 && !typeSet[storage.GetStr(r, "type")] {
			return false
		}
		return true
	})
	return parseNodes(records), nil
}

// GetEdges returns all edges for a project.
func GetEdges(projectID string) ([]Edge, error) {
	records := storage.DB.QueryFunc("graph_edges", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	return parseEdges(records), nil
}

// GetNodeEdges returns all edges involving a specific node.
func GetNodeEdges(nodeID string) ([]Edge, error) {
	records := storage.DB.QueryFunc("graph_edges", func(r storage.Record) bool {
		return storage.GetStr(r, "source_id") == nodeID ||
			storage.GetStr(r, "target_id") == nodeID
	})
	return parseEdges(records), nil
}

// UpdateEdgeTemporal marks an edge as expired and inserts the new version.
// Called by the simulation memory updater.
func UpdateEdgeTemporal(ctx context.Context, oldEdgeID, projectID, sourceID, targetID, sourceName, targetName, relation, fact string) error {
	now := time.Now().Format(time.RFC3339)

	// Expire old edge
	if rec, ok := storage.DB.Get("graph_edges", oldEdgeID); ok {
		var old Edge
		if err := json.Unmarshal([]byte(storage.GetStr(rec, "data")), &old); err == nil {
			old.ExpiredAt = now
			b, _ := json.Marshal(old)
			_ = storage.DB.Update("graph_edges", oldEdgeID, storage.Record{"data": string(b)})
		}
	}

	// Create updated edge
	newEdge := Edge{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		SourceID:   sourceID,
		TargetID:   targetID,
		SourceName: sourceName,
		TargetName: targetName,
		Relation:   relation,
		Fact:       fact,
		ValidAt:    now,
		CreatedAt:  now,
	}
	if emb, err := llm.Embed(ctx, fact); err == nil {
		newEdge.Embedding = emb
	}
	return saveEdge(&newEdge)
}

// scoredItem is used for ranked semantic search results.
type scoredItem struct {
	score float64
	idx   int
}

func sortScored(s []scoredItem) {
	// Simple insertion sort (small slices)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].score > s[j-1].score; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// SemanticSearch finds nodes and edges most similar to the query embedding.
// Returns top-k results sorted by cosine similarity.
func SemanticSearch(projectID string, queryEmb []float64, topK int) ([]Node, []Edge) {
	nodes, _ := GetNodes(projectID, nil)
	edges, _ := GetEdges(projectID)

	// Score nodes
	var nodeSc []scoredItem
	for i, n := range nodes {
		if len(n.Embedding) > 0 {
			nodeSc = append(nodeSc, scoredItem{llm.CosineSimilarity(queryEmb, n.Embedding), i})
		}
	}
	sortScored(nodeSc)
	if len(nodeSc) > topK {
		nodeSc = nodeSc[:topK]
	}
	var topNodes []Node
	for _, s := range nodeSc {
		topNodes = append(topNodes, nodes[s.idx])
	}

	// Score edges
	var edgeSc []scoredItem
	for i, e := range edges {
		if len(e.Embedding) > 0 {
			edgeSc = append(edgeSc, scoredItem{llm.CosineSimilarity(queryEmb, e.Embedding), i})
		}
	}
	sortScored(edgeSc)
	if len(edgeSc) > topK*2 {
		edgeSc = edgeSc[:topK*2]
	}
	var topEdges []Edge
	for _, s := range edgeSc {
		topEdges = append(topEdges, edges[s.idx])
	}

	return topNodes, topEdges
}

// ── Helpers ────────────────────────────────────────────────────────────────

func parseNodes(records []storage.Record) []Node {
	var nodes []Node
	for _, r := range records {
		var n Node
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &n); err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func parseEdges(records []storage.Record) []Edge {
	var edges []Edge
	for _, r := range records {
		var e Edge
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &e); err == nil {
			edges = append(edges, e)
		}
	}
	return edges
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
