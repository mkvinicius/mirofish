// Package graph implements a lightweight knowledge graph backed by JSON files.
// It replaces Zep Cloud from the original MiroFish with zero external dependencies.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"picofish/services/llm"
	"picofish/storage"
)

// Ontology describes entity types and relation types for a project.
type Ontology struct {
	EntityTypes   []string `json:"entity_types"`
	RelationTypes []string `json:"relation_types"`
}

// Node is a knowledge graph node (entity).
type Node struct {
	ID         string            `json:"id"`
	ProjectID  string            `json:"project_id"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

// Edge is a directional relationship between two nodes.
type Edge struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	Relation   string `json:"relation"`
}

// GraphSummary is returned after building a graph.
type GraphSummary struct {
	ProjectID   string   `json:"project_id"`
	NodeCount   int      `json:"node_count"`
	EdgeCount   int      `json:"edge_count"`
	EntityTypes []string `json:"entity_types"`
}

// BuildFromDocument extracts entities and relations from text using the LLM,
// then persists them in the JSON store.
func BuildFromDocument(ctx context.Context, projectID, document string) (*GraphSummary, error) {
	ont, err := generateOntology(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("ontology: %w", err)
	}

	nodes, edges, err := extractEntities(ctx, projectID, document, ont)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	for _, n := range nodes {
		r := storage.Record{
			"id":          n.ID,
			"project_id":  n.ProjectID,
			"type":        n.Type,
			"name":        n.Name,
			"properties":  propsToJSON(n.Properties),
		}
		if err := storage.DB.Insert("graph_nodes", n.ID, r); err != nil {
			return nil, err
		}
	}

	for _, e := range edges {
		r := storage.Record{
			"id":         e.ID,
			"project_id": e.ProjectID,
			"source_id":  e.SourceID,
			"target_id":  e.TargetID,
			"relation":   e.Relation,
		}
		if err := storage.DB.Insert("graph_edges", e.ID, r); err != nil {
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

func generateOntology(ctx context.Context, document string) (*Ontology, error) {
	prompt := fmt.Sprintf(`Analyze this document and define an ontology for a knowledge graph.
Return ONLY valid JSON with this exact structure:
{"entity_types": ["Type1", "Type2", ...], "relation_types": ["RELATION1", "RELATION2", ...]}

Rules:
- 6-10 specific entity types relevant to the content
- 4-8 relation types in UPPER_SNAKE_CASE
- No explanation, just JSON

Document:
%s`, truncate(document, 3000))

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.2))
	if err != nil {
		return nil, err
	}
	var ont Ontology
	if err := extractJSON(resp, &ont); err != nil {
		return nil, fmt.Errorf("parse ontology: %w", err)
	}
	return &ont, nil
}

func extractEntities(ctx context.Context, projectID, document string, ont *Ontology) ([]Node, []Edge, error) {
	prompt := fmt.Sprintf(`Extract entities and relationships from the document.

Entity types: %s
Relation types: %s

Return ONLY valid JSON:
{
  "entities": [{"type": "TypeName", "name": "Entity Name", "description": "brief description"}],
  "relations": [{"source": "Entity Name", "target": "Entity Name", "relation": "RELATION_TYPE"}]
}

Document:
%s`, strings.Join(ont.EntityTypes, ", "), strings.Join(ont.RelationTypes, ", "), truncate(document, 4000))

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.3), llm.WithMaxTokens(8192))
	if err != nil {
		return nil, nil, err
	}

	var raw struct {
		Entities []struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"entities"`
		Relations []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			Relation string `json:"relation"`
		} `json:"relations"`
	}

	if err := extractJSON(resp, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse entities: %w", err)
	}

	nodeMap := make(map[string]*Node)
	var nodes []Node
	for _, e := range raw.Entities {
		n := Node{
			ID:        uuid.NewString(),
			ProjectID: projectID,
			Type:      e.Type,
			Name:      e.Name,
			Properties: map[string]string{
				"description": e.Description,
			},
		}
		nodes = append(nodes, n)
		nodeMap[e.Name] = &nodes[len(nodes)-1]
	}

	var edges []Edge
	for _, r := range raw.Relations {
		src, ok1 := nodeMap[r.Source]
		tgt, ok2 := nodeMap[r.Target]
		if !ok1 || !ok2 {
			continue
		}
		edges = append(edges, Edge{
			ID:        uuid.NewString(),
			ProjectID: projectID,
			SourceID:  src.ID,
			TargetID:  tgt.ID,
			Relation:  r.Relation,
		})
	}

	return nodes, edges, nil
}

// GetNodes returns all nodes for a project, optionally filtered by type.
func GetNodes(projectID string, entityTypes []string) ([]Node, error) {
	typeSet := make(map[string]bool)
	for _, t := range entityTypes {
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

	var nodes []Node
	for _, r := range records {
		nodes = append(nodes, recordToNode(r))
	}
	return nodes, nil
}

// Search does a simple keyword search across node names and properties.
func Search(projectID, query string) ([]Node, error) {
	q := strings.ToLower(query)
	records := storage.DB.QueryFunc("graph_nodes", func(r storage.Record) bool {
		if storage.GetStr(r, "project_id") != projectID {
			return false
		}
		name := strings.ToLower(storage.GetStr(r, "name"))
		props := strings.ToLower(storage.GetStr(r, "properties"))
		return strings.Contains(name, q) || strings.Contains(props, q)
	})

	var nodes []Node
	for _, r := range records {
		nodes = append(nodes, recordToNode(r))
	}
	return nodes, nil
}

func recordToNode(r storage.Record) Node {
	n := Node{
		ID:         storage.GetStr(r, "id"),
		ProjectID:  storage.GetStr(r, "project_id"),
		Type:       storage.GetStr(r, "type"),
		Name:       storage.GetStr(r, "name"),
		Properties: map[string]string{},
	}
	propsStr := storage.GetStr(r, "properties")
	if propsStr != "" {
		_ = json.Unmarshal([]byte(propsStr), &n.Properties)
	}
	return n
}

func propsToJSON(props map[string]string) string {
	b, _ := json.Marshal(props)
	return string(b)
}

func extractJSON(s string, v interface{}) error {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if idx := strings.Index(s, fence); idx >= 0 {
			s = s[idx+len(fence):]
			if end := strings.Index(s, "```"); end >= 0 {
				s = s[:end]
			}
			break
		}
	}
	if idx := strings.IndexAny(s, "{["); idx >= 0 {
		s = s[idx:]
	}
	return json.Unmarshal([]byte(strings.TrimSpace(s)), v)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
