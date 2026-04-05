// tools.go implements the 4 ReACT search tools that replace Zep Cloud:
//   1. InsightForge   — deep multi-dimensional semantic search
//   2. PanoramaSearch — full graph overview with temporal tracking
//   3. QuickSearch    — fast single-query semantic lookup
//   4. InterviewAgents — interview running agents via LLM personas
package graph

import (
	"context"
	"fmt"
	"strings"

	"picofish/services/llm"
	"picofish/storage"
)

// ── InsightForge ──────────────────────────────────────────────────────────

// InsightForgeResult mirrors MiroFish's InsightForgeResult structure.
type InsightForgeResult struct {
	Query              string          `json:"query"`
	SimRequirement     string          `json:"simulation_requirement"`
	SubQueries         []string        `json:"sub_queries"`
	SemanticFacts      []string        `json:"semantic_facts"`
	EntityInsights     []EntityInsight `json:"entity_insights"`
	RelationshipChains []string        `json:"relationship_chains"`
	TotalFacts         int             `json:"total_facts"`
	TotalEntities      int             `json:"total_entities"`
	TotalRelationships int             `json:"total_relationships"`
}

type EntityInsight struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Summary      string   `json:"summary"`
	RelatedFacts []string `json:"related_facts"`
}

func (r *InsightForgeResult) ToText() string {
	var sb strings.Builder
	sb.WriteString("## Deep Insight Analysis\n")
	sb.WriteString(fmt.Sprintf("Query: %s\n", r.Query))
	sb.WriteString(fmt.Sprintf("Scenario: %s\n\n", r.SimRequirement))
	sb.WriteString(fmt.Sprintf("Statistics: %d facts | %d entities | %d relationships\n\n",
		r.TotalFacts, r.TotalEntities, r.TotalRelationships))

	if len(r.SubQueries) > 0 {
		sb.WriteString("### Sub-questions analyzed:\n")
		for i, sq := range r.SubQueries {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, sq))
		}
		sb.WriteString("\n")
	}
	if len(r.SemanticFacts) > 0 {
		sb.WriteString("### Key Facts (cite these in the report):\n")
		for i, f := range r.SemanticFacts {
			sb.WriteString(fmt.Sprintf("%d. \"%s\"\n", i+1, f))
		}
		sb.WriteString("\n")
	}
	if len(r.EntityInsights) > 0 {
		sb.WriteString("### Core Entities:\n")
		for _, e := range r.EntityInsights {
			sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", e.Name, e.Type, e.Summary))
		}
		sb.WriteString("\n")
	}
	if len(r.RelationshipChains) > 0 {
		sb.WriteString("### Relationship Chains:\n")
		for _, c := range r.RelationshipChains {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}
	return sb.String()
}

// InsightForge is the most powerful search tool.
// It decomposes the query into sub-questions and searches from multiple dimensions.
func InsightForge(ctx context.Context, projectID, query, simRequirement string) (*InsightForgeResult, error) {
	// Step 1: Generate sub-queries
	subQueries, err := generateSubQueries(ctx, query, simRequirement)
	if err != nil {
		subQueries = []string{query}
	}

	result := &InsightForgeResult{
		Query:          query,
		SimRequirement: simRequirement,
		SubQueries:     subQueries,
	}

	seen := make(map[string]bool)

	// Step 2: Search for each sub-query
	for _, sq := range subQueries {
		emb, err := llm.Embed(ctx, sq)
		if err != nil {
			continue
		}
		topNodes, topEdges := SemanticSearch(projectID, emb, 5)

		for _, e := range topEdges {
			if !seen[e.Fact] && e.Fact != "" {
				result.SemanticFacts = append(result.SemanticFacts, e.Fact)
				seen[e.Fact] = true
			}
		}
		for _, n := range topNodes {
			if !seen[n.Name] {
				insight := EntityInsight{
					Name:    n.Name,
					Type:    n.Type,
					Summary: n.Summary,
				}
				// Get related facts for this entity
				edges, _ := GetNodeEdges(n.ID)
				for _, e := range edges {
					if e.IsActive() {
						insight.RelatedFacts = append(insight.RelatedFacts, e.Fact)
					}
				}
				result.EntityInsights = append(result.EntityInsights, insight)
				seen[n.Name] = true
			}
		}
	}

	// Step 3: Build relationship chains from edges
	edges, _ := GetEdges(projectID)
	for _, e := range edges {
		if e.IsActive() && e.SourceName != "" && e.TargetName != "" {
			chain := fmt.Sprintf("%s -[%s]-> %s: %s", e.SourceName, e.Relation, e.TargetName, trunc(e.Fact, 80))
			result.RelationshipChains = append(result.RelationshipChains, chain)
		}
	}
	if len(result.RelationshipChains) > 15 {
		result.RelationshipChains = result.RelationshipChains[:15]
	}

	result.TotalFacts = len(result.SemanticFacts)
	result.TotalEntities = len(result.EntityInsights)
	result.TotalRelationships = len(result.RelationshipChains)
	return result, nil
}

func generateSubQueries(ctx context.Context, query, simRequirement string) ([]string, error) {
	prompt := fmt.Sprintf(`Decompose this analysis question into 3-5 specific sub-questions for searching a knowledge graph.
Main question: %s
Simulation scenario: %s

Return ONLY a JSON array of strings: ["sub-question 1", "sub-question 2", ...]`, query, simRequirement)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.3))
	if err != nil {
		return nil, err
	}
	var subs []string
	if err := llm.ParseJSON(resp, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}

// ── PanoramaSearch ─────────────────────────────────────────────────────────

// PanoramaResult mirrors MiroFish's PanoramaResult.
type PanoramaResult struct {
	Query          string   `json:"query"`
	ActiveFacts    []string `json:"active_facts"`
	HistoricalFacts []string `json:"historical_facts"`
	AllNodes       []Node   `json:"all_nodes"`
	TotalNodes     int      `json:"total_nodes"`
	TotalEdges     int      `json:"total_edges"`
	ActiveCount    int      `json:"active_count"`
	HistoricalCount int     `json:"historical_count"`
}

func (r *PanoramaResult) ToText() string {
	var sb strings.Builder
	sb.WriteString("## Full Simulation Overview\n")
	sb.WriteString(fmt.Sprintf("Query: %s\n\n", r.Query))
	sb.WriteString(fmt.Sprintf("Graph: %d entities | %d total facts | %d active | %d historical\n\n",
		r.TotalNodes, r.TotalEdges, r.ActiveCount, r.HistoricalCount))

	if len(r.ActiveFacts) > 0 {
		sb.WriteString("### Current Active Facts:\n")
		for i, f := range r.ActiveFacts {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(r.ActiveFacts)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
		}
		sb.WriteString("\n")
	}
	if len(r.HistoricalFacts) > 0 {
		sb.WriteString("### Historical / Expired Facts (how things evolved):\n")
		for i, f := range r.HistoricalFacts {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more historical facts\n", len(r.HistoricalFacts)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. [HISTORICAL] %s\n", i+1, f))
		}
		sb.WriteString("\n")
	}
	if len(r.AllNodes) > 0 {
		sb.WriteString("### All Entities:\n")
		for _, n := range r.AllNodes {
			sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", n.Name, n.Type, trunc(n.Summary, 80)))
		}
	}
	return sb.String()
}

// PanoramaSearch returns the full graph overview with temporal tracking.
func PanoramaSearch(ctx context.Context, projectID, query string) (*PanoramaResult, error) {
	nodes, _ := GetNodes(projectID, nil)
	edges, _ := GetEdges(projectID)

	result := &PanoramaResult{
		Query:      query,
		AllNodes:   nodes,
		TotalNodes: len(nodes),
		TotalEdges: len(edges),
	}

	for _, e := range edges {
		if e.IsActive() {
			result.ActiveFacts = append(result.ActiveFacts, e.Fact)
			result.ActiveCount++
		} else {
			result.HistoricalFacts = append(result.HistoricalFacts, e.Fact)
			result.HistoricalCount++
		}
	}
	return result, nil
}

// ── QuickSearch ────────────────────────────────────────────────────────────

// QuickSearchResult mirrors MiroFish's SearchResult.
type QuickSearchResult struct {
	Query      string   `json:"query"`
	Facts      []string `json:"facts"`
	TotalCount int      `json:"total_count"`
}

func (r *QuickSearchResult) ToText() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Quick Search: %s\nFound %d results\n\n", r.Query, r.TotalCount))
	for i, f := range r.Facts {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
	}
	return sb.String()
}

// QuickSearch performs a fast single-query semantic lookup.
func QuickSearch(ctx context.Context, projectID, query string) (*QuickSearchResult, error) {
	emb, err := llm.Embed(ctx, query)
	if err != nil {
		// Fallback: keyword search
		return quickSearchKeyword(projectID, query), nil
	}
	_, topEdges := SemanticSearch(projectID, emb, 10)

	result := &QuickSearchResult{Query: query}
	seen := make(map[string]bool)
	for _, e := range topEdges {
		if !seen[e.Fact] && e.Fact != "" {
			result.Facts = append(result.Facts, e.Fact)
			seen[e.Fact] = true
		}
	}
	result.TotalCount = len(result.Facts)
	return result, nil
}

func quickSearchKeyword(projectID, query string) *QuickSearchResult {
	q := strings.ToLower(query)
	edges, _ := GetEdges(projectID)
	result := &QuickSearchResult{Query: query}
	for _, e := range edges {
		if strings.Contains(strings.ToLower(e.Fact), q) {
			result.Facts = append(result.Facts, e.Fact)
			if len(result.Facts) >= 10 {
				break
			}
		}
	}
	result.TotalCount = len(result.Facts)
	return result
}

// ── InterviewAgents ────────────────────────────────────────────────────────

// AgentInterview holds one agent's interview response.
type AgentInterview struct {
	AgentID   string   `json:"agent_id"`
	AgentName string   `json:"agent_name"`
	AgentRole string   `json:"agent_role"`
	AgentBio  string   `json:"agent_bio"`
	Platform  string   `json:"platform"`
	Question  string   `json:"question"`
	Response  string   `json:"response"`
	KeyQuotes []string `json:"key_quotes"`
}

// InterviewResult mirrors MiroFish's InterviewResult.
type InterviewResult struct {
	Topic             string           `json:"interview_topic"`
	Questions         []string         `json:"interview_questions"`
	Interviews        []AgentInterview `json:"interviews"`
	SelectionReasoning string          `json:"selection_reasoning"`
	Summary           string           `json:"summary"`
	TotalAgents       int              `json:"total_agents"`
	InterviewedCount  int              `json:"interviewed_count"`
}

func (r *InterviewResult) ToText() string {
	var sb strings.Builder
	sb.WriteString("## Agent Interview Report\n")
	sb.WriteString(fmt.Sprintf("**Topic:** %s\n", r.Topic))
	sb.WriteString(fmt.Sprintf("**Interviewed:** %d agents\n\n", r.InterviewedCount))
	sb.WriteString("### Selection Reasoning\n")
	sb.WriteString(r.SelectionReasoning + "\n\n---\n\n")
	sb.WriteString("### Interview Transcripts\n")
	for i, iv := range r.Interviews {
		sb.WriteString(fmt.Sprintf("\n#### #%d: %s (%s) [%s]\n", i+1, iv.AgentName, iv.AgentRole, iv.Platform))
		sb.WriteString(fmt.Sprintf("_Bio: %s_\n\n", iv.AgentBio))
		sb.WriteString(fmt.Sprintf("**Q:** %s\n\n**A:** %s\n", iv.Question, iv.Response))
		if len(iv.KeyQuotes) > 0 {
			sb.WriteString("\n**Key quotes:**\n")
			for _, q := range iv.KeyQuotes {
				sb.WriteString(fmt.Sprintf("> \"%s\"\n", q))
			}
		}
		sb.WriteString("\n---\n")
	}
	sb.WriteString("\n### Summary\n")
	sb.WriteString(r.Summary)
	return sb.String()
}

// InterviewAgents selects relevant agents and interviews them via LLM.
// This replaces the OASIS interview API with equivalent LLM-driven responses.
func InterviewAgents(ctx context.Context, projectID, topic, simRequirement string) (*InterviewResult, error) {
	// Load agent profiles from store
	records := storage.DB.QueryFunc("agents", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	if len(records) == 0 {
		return nil, fmt.Errorf("no agents found — run simulation first")
	}

	// Select 3-5 most relevant agents using semantic similarity
	selectedAgents, reasoning, err := selectAgentsForTopic(ctx, records, topic)
	if err != nil || len(selectedAgents) == 0 {
		// Fallback: use first 4 agents
		if len(records) > 4 {
			records = records[:4]
		}
		selectedAgents = records
		reasoning = "Automatically selected based on availability."
	}

	// Generate interview questions
	questions, err := generateInterviewQuestions(ctx, topic, simRequirement)
	if err != nil || len(questions) == 0 {
		questions = []string{
			fmt.Sprintf("What is your perspective on: %s?", topic),
			"How has this situation affected you or those around you?",
			"What do you think will happen next?",
		}
	}

	result := &InterviewResult{
		Topic:              topic,
		Questions:          questions,
		SelectionReasoning: reasoning,
		TotalAgents:        len(records),
	}

	// Interview each selected agent
	platforms := []string{"twitter", "reddit"}
	for i, rec := range selectedAgents {
		profileJSON := storage.GetStr(rec, "profile_full")
		platform := platforms[i%len(platforms)]
		question := questions[i%len(questions)]

		iv, err := interviewAgent(ctx, rec, profileJSON, platform, question, topic)
		if err != nil {
			continue
		}
		result.Interviews = append(result.Interviews, *iv)
		result.InterviewedCount++
	}

	// Generate summary
	result.Summary = generateInterviewSummary(ctx, result)
	return result, nil
}

func selectAgentsForTopic(ctx context.Context, records []storage.Record, topic string) ([]storage.Record, string, error) {
	topicEmb, err := llm.Embed(ctx, topic)
	if err != nil {
		return nil, "", err
	}

	type scored struct {
		rec   storage.Record
		score float64
	}
	var scored_agents []scored
	for _, rec := range records {
		profileText := storage.GetStr(rec, "name") + " " +
			storage.GetStr(rec, "profession") + " " +
			storage.GetStr(rec, "bio")
		emb, err := llm.Embed(ctx, profileText)
		if err != nil {
			scored_agents = append(scored_agents, scored{rec, 0.5})
			continue
		}
		scored_agents = append(scored_agents, scored{rec, llm.CosineSimilarity(topicEmb, emb)})
	}

	// Sort by score descending
	for i := 1; i < len(scored_agents); i++ {
		for j := i; j > 0 && scored_agents[j].score > scored_agents[j-1].score; j-- {
			scored_agents[j], scored_agents[j-1] = scored_agents[j-1], scored_agents[j]
		}
	}

	// Take top 4
	n := 4
	if len(scored_agents) < n {
		n = len(scored_agents)
	}
	var selected []storage.Record
	for i := 0; i < n; i++ {
		selected = append(selected, scored_agents[i].rec)
	}

	reasoning := fmt.Sprintf("Selected %d agents most relevant to the topic '%s' based on semantic similarity.", n, topic)
	return selected, reasoning, nil
}

func generateInterviewQuestions(ctx context.Context, topic, simRequirement string) ([]string, error) {
	prompt := fmt.Sprintf(`Generate 3 interview questions for agents in a simulation about: "%s"
Simulation scenario: %s
Return ONLY a JSON array: ["question 1", "question 2", "question 3"]`, topic, simRequirement)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.6))
	if err != nil {
		return nil, err
	}
	var qs []string
	_ = llm.ParseJSON(resp, &qs)
	return qs, nil
}

func interviewAgent(ctx context.Context, rec storage.Record, profileJSON, platform, question, topic string) (*AgentInterview, error) {
	name := storage.GetStr(rec, "name")
	profession := storage.GetStr(rec, "profession")
	bio := storage.GetStr(rec, "bio")
	persona := storage.GetStr(rec, "persona")
	stance := storage.GetStr(rec, "stance")

	prompt := fmt.Sprintf(`You are %s, a %s on %s.
Bio: %s
Persona: %s
Your stance on topics: %s

You are being interviewed about: %s

Question: %s

Respond in character. Be authentic, specific, and direct. 2-4 sentences.`,
		name, profession, platform, bio, persona, stance, topic, question)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.85), llm.WithMaxTokens(300))
	if err != nil {
		return nil, err
	}

	iv := &AgentInterview{
		AgentID:   storage.GetStr(rec, "id"),
		AgentName: name,
		AgentRole: profession,
		AgentBio:  bio,
		Platform:  platform,
		Question:  question,
		Response:  resp,
	}

	// Extract key quotes (sentences > 20 chars)
	for _, sent := range strings.Split(resp, ".") {
		sent = strings.TrimSpace(sent)
		if len(sent) > 20 {
			iv.KeyQuotes = append(iv.KeyQuotes, sent)
			if len(iv.KeyQuotes) >= 2 {
				break
			}
		}
	}
	return iv, nil
}

func generateInterviewSummary(ctx context.Context, result *InterviewResult) string {
	if len(result.Interviews) == 0 {
		return "No interviews conducted."
	}
	var sb strings.Builder
	sb.WriteString("Interviews conducted:\n")
	for _, iv := range result.Interviews {
		sb.WriteString(fmt.Sprintf("- %s [%s]: %s\n", iv.AgentName, iv.Platform, trunc(iv.Response, 100)))
	}

	prompt := fmt.Sprintf(`Summarize these agent interviews about "%s" in 2-3 sentences, highlighting key perspectives and agreements/disagreements:

%s`, result.Topic, sb.String())

	summary, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.5))
	if err != nil {
		return sb.String()
	}
	return summary
}
