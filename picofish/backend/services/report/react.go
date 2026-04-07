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
	"picofish/config"
	"picofish/services/agents"
	"picofish/services/graph"
	"picofish/services/llm"
	"picofish/storage"
)

// ── Status types ───────────────────────────────────────────────────────────

type Status struct {
	ProjectID   string   `json:"project_id"`
	ReportID    string   `json:"report_id"`
	Status      string   `json:"status"`
	Outline     *Outline `json:"outline,omitempty"`
	Content     string   `json:"content,omitempty"`
	Error       string   `json:"error,omitempty"`
	Progress    []string `json:"progress,omitempty"`    // live progress log
	CurrentStep string   `json:"current_step,omitempty"` // e.g. "Writing: Timeline..."
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
	mu       sync.RWMutex
	m        map[string]*Status
	watchers map[string][]chan struct{} // SSE listeners per project
}

var statuses = &statusStore{
	m:        make(map[string]*Status),
	watchers: make(map[string][]chan struct{}),
}

func (s *statusStore) set(key string, v *Status) {
	s.mu.Lock()
	s.m[key] = v
	// Notify SSE watchers
	for _, ch := range s.watchers[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *statusStore) get(key string) *Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[key]
}

func (s *statusStore) watch(key string) (chan struct{}, func()) {
	ch := make(chan struct{}, 4)
	s.mu.Lock()
	s.watchers[key] = append(s.watchers[key], ch)
	s.mu.Unlock()
	unwatch := func() {
		s.mu.Lock()
		ws := s.watchers[key]
		for i, w := range ws {
			if w == ch {
				s.watchers[key] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}
	return ch, unwatch
}

// Subscribe returns a channel that receives updates whenever the report status changes.
// The caller must call the returned cancel func when done.
func Subscribe(projectID string) (chan struct{}, func()) {
	return statuses.watch(projectID)
}

// emit appends a progress message to the status and notifies SSE watchers.
func emit(projectID, msg string, status *Status) {
	status.Progress = append(status.Progress, msg)
	status.CurrentStep = msg
	statuses.set(projectID, status)
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
			statuses.set(projectID, status) // notify watchers
			_ = storage.DB.Update("reports", reportID, storage.Record{"status": "error", "content": err.Error()})
			return
		}
		status.Status = "completed"
		status.Content = content
		statuses.set(projectID, status) // notify watchers
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
	totalActionCount := 0
	if v, ok := actionSummary["total"].(int); ok {
		totalActionCount = v
	}
	emit(projectID, fmt.Sprintf("📋 Planning report for %d entities, %d actions...", len(nodes), totalActionCount), status)
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
	emit(projectID, fmt.Sprintf("📑 Outline ready: %d sections — %s", len(outline.Sections), outline.Title), status)
	_ = storage.DB.Update("reports", reportID, storage.Record{"status": "generating"})

	// Generate sections
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("# %s\n\n*%s*\n\n*Generated: %s | Scenario: %s*\n\n---\n\n",
		outline.Title, outline.Summary,
		time.Now().Format("2006-01-02 15:04"), simRequirement))

	for i, section := range outline.Sections {
		emit(projectID, fmt.Sprintf("✍️ Writing section %d/%d: %s", i+1, len(outline.Sections), section.Title), status)
		content, err := generateSection(ctx, projectID, section, outline, simRequirement, actions)
		if err != nil {
			content = fmt.Sprintf("*Error: %v*", err)
		}
		buf.WriteString(fmt.Sprintf("## %s\n\n%s\n\n---\n\n", section.Title, content))
	}

	// Conclusion
	emit(projectID, "📝 Writing conclusion...", status)
	conclusion, err := llm.Chat(ctx,
		[]llm.Message{llm.User(fmt.Sprintf(
			"Write a 2-3 paragraph conclusion for this Future Prediction Report. Summarize key predictions, highlight risks/opportunities, and advise stakeholders.\n\nReport:\n%s",
			trunc(buf.String(), 4000)))},
		llm.WithTemperature(0.5), llm.WithMaxTokens(1024))
	if err == nil {
		buf.WriteString("## Conclusion\n\n" + conclusion)
	}

	draft := buf.String()

	// Ensure all mandatory sections are present
	emit(projectID, "🔍 Checking mandatory sections...", status)
	draft = ensureMandatorySections(ctx, projectID, draft, simRequirement, actions)

	// Self-critique loop
	critiqueIters := 2
	if config.Global != nil && config.Global.ReportCritiqueIterations > 0 {
		critiqueIters = config.Global.ReportCritiqueIterations
	}
	for i := 0; i < critiqueIters; i++ {
		emit(projectID, fmt.Sprintf("🔄 Self-critique pass %d/%d...", i+1, critiqueIters), status)
	}
	draft = selfCritiqueLoop(ctx, draft)
	emit(projectID, "✅ Report complete!", status)

	return draft, nil
}

// selfCritiqueLoop sends the draft report to the LLM for editorial review.
// Runs up to 2 iterations to improve evidence support and prediction confidence.
func selfCritiqueLoop(ctx context.Context, draft string) string {
	critiqueSystem := `You are a critical editor reviewing a Future Prediction Report. Improve it by:
1) Adding confidence scores (High/Medium/Low) to any predictions that lack them
2) Flagging and removing conclusions not supported by simulation evidence
3) Ensuring all key actors mentioned in the simulation appear in the report
4) Making predictions more specific and falsifiable

Return ONLY the improved report text. No commentary, no explanations.`

	for i := 0; i < 2; i++ {
		improved, err := llm.Chat(ctx,
			[]llm.Message{
				llm.System(critiqueSystem),
				llm.User(trunc(draft, 6000)),
			},
			llm.WithTemperature(0.3), llm.WithMaxTokens(4096))
		if err != nil || len(improved) < len(draft)/2 {
			// Don't replace with something clearly shorter/broken
			break
		}
		draft = improved
	}
	return draft
}

// ensureMandatorySections appends any missing mandatory sections to the report.
func ensureMandatorySections(ctx context.Context, projectID, draft, simRequirement string, actions []*agents.AgentAction) string {
	type mandatorySection struct {
		header  string
		prompt  string
		tool    string
	}
	mandatory := []mandatorySection{
		{
			header: "## Timeline of Key Events",
			prompt: "Write a chronological Timeline of Key Events from this simulation. List the most significant moments in order, with simulated hour and description.",
			tool:   "PanoramaSearch",
		},
		{
			header: "## Key Actors & Influence Scores",
			prompt: "List the top 5 most influential agents in this simulation. For each: name, role, stance, and an estimated influence score (1-10) with brief reasoning.",
			tool:   "PanoramaSearch",
		},
		{
			header: "## Prediction Confidence",
			prompt: "Rate the confidence of the main predictions in this report: High (strong simulation evidence), Medium (partial evidence), or Low (extrapolation). Justify each rating.",
			tool:   "InsightForge",
		},
		{
			header: "## Divergence Points",
			prompt: "Identify 2-3 divergence points: moments in the simulation where a different agent action could have led to a substantially different outcome. Explain each briefly.",
			tool:   "QuickSearch",
		},
	}

	for _, ms := range mandatory {
		if strings.Contains(draft, ms.header) {
			continue
		}
		// Generate section content using the required tool first
		toolResult := ""
		if ms.tool != "" {
			r, err := executeTool(ctx, projectID, ms.tool, ms.prompt, simRequirement)
			if err == nil {
				toolResult = trunc(r, 2000)
			}
		}
		contextStr := ""
		if toolResult != "" {
			contextStr = "\n\nResearch findings:\n" + toolResult
		}
		content, err := llm.Chat(ctx,
			[]llm.Message{llm.User(ms.prompt + contextStr)},
			llm.WithTemperature(0.5), llm.WithMaxTokens(800))
		if err != nil {
			content = "*Section could not be generated.*"
		}
		draft += "\n\n" + ms.header + "\n\n" + content
	}
	return draft
}

// requiredToolForSection returns the tool that must be called before writing this section.
func requiredToolForSection(title string) string {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "conclusion") || strings.Contains(t, "summary") || strings.Contains(t, "trend") || strings.Contains(t, "risk"):
		return "InsightForge"
	case strings.Contains(t, "actor") || strings.Contains(t, "influence") || strings.Contains(t, "agent") || strings.Contains(t, "behavior"):
		return "PanoramaSearch"
	case strings.Contains(t, "interview") || strings.Contains(t, "quote") || strings.Contains(t, "perspective"):
		return "InterviewAgents"
	default:
		return "" // no mandatory tool
	}
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

	// Enforce mandatory tool call before the ReACT loop
	if reqTool := requiredToolForSection(section.Title); reqTool != "" {
		toolResult, err := executeTool(ctx, projectID, reqTool, section.Description, simRequirement)
		if err == nil {
			messages = append(messages,
				llm.Assistant(fmt.Sprintf(`{"thinking":"Pre-loading required context","tool":%q,"tool_input":%q,"continue":true}`,
					reqTool, section.Description)),
				llm.User("Tool result:\n\n"+trunc(toolResult, 3000)+"\n\nContinue with the section."),
			)
		}
	}

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
