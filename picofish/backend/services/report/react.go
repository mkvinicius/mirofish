// Package report implements the ReACT-pattern report agent,
// exactly replicating MiroFish's report_agent.py behavior.
package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"picofish/services/agents"
	"picofish/services/graph"
	"picofish/services/llm"
	"picofish/storage"
)

// ── Status types ───────────────────────────────────────────────────────────

type Status struct {
	ProjectID string   `json:"project_id"`
	ReportID  string   `json:"report_id"`
	Status    string   `json:"status"`
	Outline   *Outline `json:"outline,omitempty"`
	Content   string   `json:"content,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type Outline struct {
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Sections []Section `json:"sections"`
}

type Section struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Content     string `json:"content,omitempty"`
}

// ── Status store ───────────────────────────────────────────────────────────

type statusStore struct {
	mu sync.RWMutex
	m  map[string]*Status
}

var statuses = &statusStore{m: make(map[string]*Status)}

func (s *statusStore) set(key string, v *Status) {
	s.mu.Lock()
	s.m[key] = v
	s.mu.Unlock()
}

func (s *statusStore) get(key string) *Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[key]
}

// ── Entry point ────────────────────────────────────────────────────────────

func Generate(ctx context.Context, projectID, simRequirement string) (*Status, error) {
	reportID := uuid.NewString()
	status := &Status{
		ProjectID: projectID,
		ReportID:  reportID,
		Status:    "planning",
	}
	statuses.set(projectID, status)

	if err := storage.DB.Insert("reports", reportID, storage.Record{
		"id": reportID, "project_id": projectID,
		"status": "planning", "content": "",
	}); err != nil {
		return nil, err
	}

	go func() {
		content, err := runReACT(context.Background(), projectID, simRequirement, reportID, status)
		if err != nil {
			status.Status = "error"
			status.Error = err.Error()
			_ = storage.DB.Update("reports", reportID, storage.Record{"status": "error", "content": err.Error()})
			return
		}
		status.Status = "completed"
		status.Content = content
		_ = storage.DB.Update("reports", reportID, storage.Record{"status": "completed", "content": content})
	}()

	return status, nil
}

func GetStatus(projectID string) *Status {
	if s := statuses.get(projectID); s != nil {
		return s
	}
	records := storage.DB.QueryFunc("reports", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	if len(records) == 0 {
		return nil
	}
	r := records[0]
	return &Status{
		ProjectID: projectID,
		ReportID:  storage.GetStr(r, "id"),
		Status:    storage.GetStr(r, "status"),
		Content:   storage.GetStr(r, "content"),
	}
}

// ── Prompts ────────────────────────────────────────────────────────────────

const planSystemPrompt = `You are an expert "Future Prediction Report" writer with a god's eye view of a simulated world.

CORE CONCEPT: We injected variables (simulation requirement) into a simulated world. The simulation's evolution IS the prediction of the future.

YOUR TASK: Write a report answering:
1. What happened in the future under our conditions?
2. How did each agent group react?
3. What future trends and risks did the simulation reveal?

SECTION COUNT: 2-5 sections maximum. No sub-sections.

Return ONLY valid JSON:
{"title":"Report Title","summary":"One-sentence core prediction","sections":[{"title":"...","description":"..."}]}`

const sectionSystemPrompt = `You are writing section "%s" of the report "%s".
Scenario: %s | Summary: %s

RULES:
1. Call tools 3-5 times to observe the simulated world
2. Quote agents' actual words as evidence
3. Focus on PREDICTIONS — future tense
4. NO Markdown headers (system adds the title)
5. Use **bold**, > blockquotes, - lists

Available tools: InsightForge, PanoramaSearch, QuickSearch, InterviewAgents

Respond in JSON:
{"thinking":"what do I need?","tool":"ToolName","tool_input":"query","continue":true}
OR when ready to write:
{"thinking":"have enough info","tool":null,"content":"section content in Markdown"}`

// ── ReACT loop ─────────────────────────────────────────────────────────────

func runReACT(ctx context.Context, projectID, simRequirement, reportID string, status *Status) (string, error) {
	nodes, _ := graph.GetNodes(projectID, nil)
	edges, _ := graph.GetEdges(projectID)
	actions, _ := agents.GetRecentActions(projectID, 200)
	actionSummary := agents.GetActionSummary(projectID)

	if len(nodes) == 0 {
		return "", fmt.Errorf("no graph data — complete Steps 1-3 first")
	}

	var factSamples []string
	for _, e := range edges {
		if e.IsActive() && len(factSamples) < 15 {
			factSamples = append(factSamples, e.Fact)
		}
	}
	typeCount := map[string]int{}
	for _, n := range nodes {
		typeCount[n.Type]++
	}
	var typeStr []string
	for t, c := range typeCount {
		typeStr = append(typeStr, fmt.Sprintf("%s:%d", t, c))
	}

	// Plan
	status.Status = "planning"
	planUser := fmt.Sprintf(`Scenario: %s
Graph: %d entities | %d relationships | types: %s
Agents: %v | Actions: %v

Sample simulation facts:
- %s`,
		simRequirement, len(nodes), len(edges),
		strings.Join(typeStr, ", "),
		actionSummary["agent_count"], actionSummary["total"],
		strings.Join(factSamples, "\n- "))

	planResp, err := llm.Chat(ctx,
		[]llm.Message{llm.System(planSystemPrompt), llm.User(planUser)},
		llm.WithTemperature(0.4), llm.WithMaxTokens(2048))
	if err != nil {
		return "", fmt.Errorf("plan: %w", err)
	}

	var outline Outline
	if err := llm.ParseJSON(planResp, &outline); err != nil || len(outline.Sections) == 0 {
		outline = defaultOutline()
	}
	status.Outline = &outline
	status.Status = "generating"
	_ = storage.DB.Update("reports", reportID, storage.Record{"status": "generating"})

	// Generate sections
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("# %s\n\n*%s*\n\n*Generated: %s | Scenario: %s*\n\n---\n\n",
		outline.Title, outline.Summary,
		time.Now().Format("2006-01-02 15:04"), simRequirement))

	for _, section := range outline.Sections {
		content, err := generateSection(ctx, projectID, section, outline, simRequirement, actions)
		if err != nil {
			content = fmt.Sprintf("*Error: %v*", err)
		}
		buf.WriteString(fmt.Sprintf("## %s\n\n%s\n\n---\n\n", section.Title, content))
	}

	// Conclusion
	conclusion, err := llm.Chat(ctx,
		[]llm.Message{llm.User(fmt.Sprintf(
			"Write a 2-3 paragraph conclusion for this Future Prediction Report. Summarize key predictions, highlight risks/opportunities, and advise stakeholders.\n\nReport:\n%s",
			trunc(buf.String(), 4000)))},
		llm.WithTemperature(0.5), llm.WithMaxTokens(1024))
	if err == nil {
		buf.WriteString("## Conclusion\n\n" + conclusion)
	}

	return buf.String(), nil
}

func generateSection(ctx context.Context, projectID string, section Section,
	outline Outline, simRequirement string, actions []*agents.AgentAction) (string, error) {

	system := fmt.Sprintf(sectionSystemPrompt,
		section.Title, outline.Title, simRequirement, outline.Summary)

	actionCtx := buildActionContext(actions, 10)
	initial := fmt.Sprintf(`Write section: "%s"
Purpose: %s

Recent simulation agent actions:
%s

Start with what information you need, then use tools.`,
		section.Title, section.Description, actionCtx)

	messages := []llm.Message{llm.System(system), llm.User(initial)}

	for i := 0; i < 5; i++ {
		resp, err := llm.Chat(ctx, messages,
			llm.WithTemperature(0.6), llm.WithMaxTokens(2048))
		if err != nil {
			return "", err
		}

		var step struct {
			Thinking  string `json:"thinking"`
			Tool      string `json:"tool"`
			ToolInput string `json:"tool_input"`
			Content   string `json:"content"`
			Continue  bool   `json:"continue"`
		}
		if err := llm.ParseJSON(resp, &step); err != nil {
			return resp, nil // treat raw response as content
		}
		if step.Content != "" {
			return step.Content, nil
		}
		if step.Tool == "" {
			break
		}

		toolResult, err := executeTool(ctx, projectID, step.Tool, step.ToolInput, simRequirement)
		if err != nil {
			toolResult = fmt.Sprintf("Tool error: %v", err)
		}

		messages = append(messages,
			llm.Assistant(resp),
			llm.User(fmt.Sprintf("Tool result:\n\n%s\n\nContinue or write the section.", trunc(toolResult, 3000))),
		)
	}

	// Final write
	final, err := llm.Chat(ctx,
		append(messages, llm.User("Write the complete section now. Markdown formatting, no headers, prediction voice.")),
		llm.WithTemperature(0.65), llm.WithMaxTokens(3000))
	if err != nil {
		return "", err
	}
	// Strip JSON wrapper if returned
	if strings.HasPrefix(strings.TrimSpace(final), "{") {
		var step struct{ Content string `json:"content"` }
		if llm.ParseJSON(final, &step) == nil && step.Content != "" {
			return step.Content, nil
		}
	}
	return final, nil
}

func executeTool(ctx context.Context, projectID, tool, input, simRequirement string) (string, error) {
	switch tool {
	case "InsightForge":
		r, err := graph.InsightForge(ctx, projectID, input, simRequirement)
		if err != nil {
			return "", err
		}
		return r.ToText(), nil
	case "PanoramaSearch":
		r, err := graph.PanoramaSearch(ctx, projectID, input)
		if err != nil {
			return "", err
		}
		return r.ToText(), nil
	case "QuickSearch":
		r, err := graph.QuickSearch(ctx, projectID, input)
		if err != nil {
			return "", err
		}
		return r.ToText(), nil
	case "InterviewAgents":
		r, err := graph.InterviewAgents(ctx, projectID, input, simRequirement)
		if err != nil {
			return "", err
		}
		return r.ToText(), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", tool)
	}
}

// ── Chat (Step 5) ──────────────────────────────────────────────────────────

func Chat(ctx context.Context, projectID, simRequirement, userMessage string, history []llm.Message) (string, error) {
	nodes, _ := graph.GetNodes(projectID, nil)
	actions, _ := agents.GetRecentActions(projectID, 100)
	summary := agents.GetActionSummary(projectID)

	var entityStr strings.Builder
	for _, n := range nodes {
		entityStr.WriteString(fmt.Sprintf("- [%s] %s: %s\n", n.Type, n.Name, trunc(n.Summary, 100)))
	}

	var recentStr strings.Builder
	n := 20
	if len(actions) < n {
		n = len(actions)
	}
	for _, a := range actions[:n] {
		if a.Content != "" {
			recentStr.WriteString(fmt.Sprintf("[%s/%s H%d] %s: %s\n",
				a.Platform, a.ActionType, a.SimHour, a.AgentName, trunc(a.Content, 100)))
		}
	}

	system := fmt.Sprintf(`You are an AI analyst with god's-eye view of a social simulation.
Scenario: %s
Graph (%d entities): %s
Activity: %v total actions, %v agents
Recent behaviors: %s
Answer questions specifically, referencing actual simulation data. Speak in prediction voice.`,
		simRequirement, len(nodes), entityStr.String(),
		summary["total"], summary["agent_count"], recentStr.String())

	msgs := []llm.Message{llm.System(system)}
	msgs = append(msgs, history...)
	msgs = append(msgs, llm.User(userMessage))
	return llm.Chat(ctx, msgs, llm.WithTemperature(0.7), llm.WithMaxTokens(2048))
}

// ── Helpers ────────────────────────────────────────────────────────────────

func defaultOutline() Outline {
	return Outline{
		Title:   "Simulation Prediction Report",
		Summary: "Analysis of simulated agent behaviors and predicted future trends",
		Sections: []Section{
			{Title: "Executive Summary", Description: "Key predictions and findings"},
			{Title: "Agent Behavior Analysis", Description: "How different groups reacted"},
			{Title: "Emergent Dynamics", Description: "Patterns and cascades"},
			{Title: "Future Trends & Risks", Description: "What this simulation predicts"},
		},
	}
}

func buildActionContext(actions []*agents.AgentAction, limit int) string {
	var posts []*agents.AgentAction
	for _, a := range actions {
		if a.Content != "" {
			posts = append(posts, a)
		}
	}
	if len(posts) > limit {
		posts = posts[:limit]
	}
	var sb strings.Builder
	for _, a := range posts {
		sb.WriteString(fmt.Sprintf("[%s/%s H%d] %s: %s\n",
			a.Platform, a.ActionType, a.SimHour, a.AgentName, trunc(a.Content, 120)))
	}
	return sb.String()
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Ensure json import is used (used in GetStatus DB decode)
var _ = json.Marshal
