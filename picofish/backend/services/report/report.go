// Package report implements a lightweight ReACT-pattern report generator.
package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"picofish/services/agents"
	"picofish/services/graph"
	"picofish/services/llm"
	"picofish/storage"
)

// ReportStatus tracks generation progress.
type ReportStatus struct {
	ProjectID string `json:"project_id"`
	ReportID  string `json:"report_id"`
	Status    string `json:"status"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
}

var statusMap = map[string]*ReportStatus{}

// Generate starts async report generation for a project.
func Generate(ctx context.Context, projectID string) (*ReportStatus, error) {
	reportID := uuid.NewString()
	status := &ReportStatus{
		ProjectID: projectID,
		ReportID:  reportID,
		Status:    "generating",
	}
	statusMap[projectID] = status

	r := storage.Record{
		"id":         reportID,
		"project_id": projectID,
		"status":     "generating",
		"content":    "",
	}
	if err := storage.DB.Insert("reports", reportID, r); err != nil {
		return nil, err
	}

	go generateAsync(context.Background(), projectID, reportID, status)
	return status, nil
}

func generateAsync(ctx context.Context, projectID, reportID string, status *ReportStatus) {
	report, err := runReACT(ctx, projectID)
	if err != nil {
		status.Status = "error"
		status.Error = err.Error()
		_ = storage.DB.Update("reports", reportID, storage.Record{"status": "error", "content": err.Error()})
		return
	}
	status.Status = "completed"
	status.Content = report
	_ = storage.DB.Update("reports", reportID, storage.Record{"status": "completed", "content": report})
}

func runReACT(ctx context.Context, projectID string) (string, error) {
	nodes, err := graph.GetNodes(projectID, nil)
	if err != nil {
		return "", fmt.Errorf("get nodes: %w", err)
	}
	actions, err := agents.GetRecentActions(projectID, 100)
	if err != nil {
		return "", fmt.Errorf("get actions: %w", err)
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("no graph data — complete Steps 1-3 first")
	}

	entitySummary := buildEntitySummary(nodes)
	actionSummary := buildActionSummary(actions)

	// THINK: plan report structure
	planPrompt := fmt.Sprintf(`You are an analyst generating a simulation report.

Knowledge Graph Entities:
%s

Simulation Activity Summary:
%s

Plan a report with 3-4 chapters. Return ONLY a JSON array of chapter titles:
["Chapter 1 Title", "Chapter 2 Title", ...]`, entitySummary, actionSummary)

	planResp, err := llm.Chat(ctx, []llm.Message{llm.User(planPrompt)}, llm.WithTemperature(0.3))
	if err != nil {
		return "", err
	}

	var chapters []string
	if err := extractJSON(planResp, &chapters); err != nil || len(chapters) == 0 {
		chapters = []string{
			"Executive Summary",
			"Key Entities and Relationships",
			"Simulation Dynamics",
			"Insights and Predictions",
		}
	}

	// ACT: generate each chapter
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("# Simulation Report\n*Generated: %s*\n\n",
		time.Now().Format("2006-01-02 15:04")))

	for i, chapter := range chapters {
		content, err := generateChapter(ctx, chapter, i+1, entitySummary, actionSummary)
		if err != nil {
			content = fmt.Sprintf("*Error generating section: %v*", err)
		}
		buf.WriteString(fmt.Sprintf("## %s\n\n%s\n\n---\n\n", chapter, content))
	}

	// SYNTHESIZE: conclusion
	conclusion, err := llm.Chat(ctx,
		[]llm.Message{llm.User(fmt.Sprintf(
			"Based on this simulation report, write a concise conclusion (2-3 paragraphs) with key insights and predictions.\n\nReport:\n%s",
			truncate(buf.String(), 3000)))},
		llm.WithTemperature(0.5))
	if err == nil {
		buf.WriteString("## Conclusion\n\n" + conclusion)
	}

	return buf.String(), nil
}

func generateChapter(ctx context.Context, chapter string, num int, entitySummary, actionSummary string) (string, error) {
	prompt := fmt.Sprintf(`Write content for report chapter %d: "%s".

Entities:
%s

Agent behavior:
%s

Write 2-4 focused paragraphs. Reference specific entities and behaviors. Use markdown.
Do not repeat the chapter title.`,
		num, chapter, truncate(entitySummary, 2000), truncate(actionSummary, 2000))

	return llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.6))
}

// Chat allows interactive Q&A about the simulation results (Step 5).
func Chat(ctx context.Context, projectID, userMessage string, history []llm.Message) (string, error) {
	nodes, _ := graph.GetNodes(projectID, nil)
	actions, _ := agents.GetRecentActions(projectID, 50)

	system := fmt.Sprintf(`You are an AI analyst with deep knowledge of a social simulation.

Knowledge Graph:
%s

Recent agent activity:
%s

Answer questions about the simulation. Be specific, reference actual entities and behaviors.`,
		truncate(buildEntitySummary(nodes), 2000),
		truncate(buildActionSummary(actions), 1500))

	messages := []llm.Message{llm.System(system)}
	messages = append(messages, history...)
	messages = append(messages, llm.User(userMessage))

	return llm.Chat(ctx, messages, llm.WithTemperature(0.7))
}

// GetStatus returns the current report status for a project.
func GetStatus(projectID string) *ReportStatus {
	if s, ok := statusMap[projectID]; ok {
		return s
	}
	records := storage.DB.QueryFunc("reports", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	if len(records) == 0 {
		return nil
	}
	r := records[0] // most recent (sorted desc)
	return &ReportStatus{
		ProjectID: projectID,
		ReportID:  storage.GetStr(r, "id"),
		Status:    storage.GetStr(r, "status"),
		Content:   storage.GetStr(r, "content"),
	}
}

func buildEntitySummary(nodes []graph.Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", n.Type, n.Name, n.Properties["description"]))
	}
	return sb.String()
}

func buildActionSummary(actions []agents.Action) string {
	if len(actions) == 0 {
		return "No simulation actions recorded yet."
	}
	typeCount := map[string]int{}
	platformCount := map[string]int{}
	for _, a := range actions {
		typeCount[a.ActionType]++
		platformCount[a.Platform]++
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total: %d | ", len(actions)))
	for t, c := range typeCount {
		sb.WriteString(fmt.Sprintf("%s:%d ", t, c))
	}
	sb.WriteString("\n")
	for p, c := range platformCount {
		sb.WriteString(fmt.Sprintf("%s:%d ", p, c))
	}
	sb.WriteString("\n\nSample posts:\n")
	n := 15
	if len(actions) < n {
		n = len(actions)
	}
	for _, a := range actions[:n] {
		if a.ActionType != "LIKE_POST" {
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n", a.Platform, a.AgentName, truncate(a.Content, 100)))
		}
	}
	return sb.String()
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
	return s[:max] + "..."
}
