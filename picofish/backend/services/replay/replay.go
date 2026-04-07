// Package replay queries stored simulation replay frames and exports them
// in JSON, CSV, or Markdown formats for analysis and download.
package replay

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"picofish/services/agents"
	"picofish/storage"
)

// ── Types ──────────────────────────────────────────────────────────────────

// ReplayExport bundles all frames for a simulation run.
type ReplayExport struct {
	ProjectID  string               `json:"project_id"`
	SimID      string               `json:"sim_id,omitempty"`
	FrameCount int                  `json:"frame_count"`
	Frames     []agents.ReplayFrame `json:"frames"`
}

// ── Queries ────────────────────────────────────────────────────────────────

// GetFrames returns all replay frames for a project, sorted by hour ascending.
func GetFrames(projectID string) ([]agents.ReplayFrame, error) {
	records := storage.DB.QueryFunc("replay_frames", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	frames := make([]agents.ReplayFrame, 0, len(records))
	for _, r := range records {
		var f agents.ReplayFrame
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &f); err == nil {
			frames = append(frames, f)
		}
	}
	sort.Slice(frames, func(i, j int) bool {
		return frames[i].Hour < frames[j].Hour
	})
	return frames, nil
}

// GetFrame returns a single replay frame by hour index.
func GetFrame(projectID string, hour int) (*agents.ReplayFrame, error) {
	frames, err := GetFrames(projectID)
	if err != nil {
		return nil, err
	}
	for i, f := range frames {
		if f.Hour == hour {
			return &frames[i], nil
		}
	}
	return nil, fmt.Errorf("frame for hour %d not found", hour)
}

// ── Export formats ─────────────────────────────────────────────────────────

// ExportJSON returns all replay frames as a pretty JSON byte slice.
func ExportJSON(projectID string) ([]byte, error) {
	frames, err := GetFrames(projectID)
	if err != nil {
		return nil, err
	}
	export := ReplayExport{
		ProjectID:  projectID,
		FrameCount: len(frames),
		Frames:     frames,
	}
	return json.MarshalIndent(export, "", "  ")
}

// ExportCSV returns all replay frames as a CSV byte slice.
// One row per (frame, agent) combination.
func ExportCSV(projectID string) ([]byte, error) {
	frames, err := GetFrames(projectID)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("hour,sim_hour,agent_id,agent_name,stance,sentiment_bias,action_count,frame_action_count,frame_post_count\n")

	for _, f := range frames {
		for _, ag := range f.Agents {
			sb.WriteString(fmt.Sprintf("%d,%d,%s,%s,%s,%.4f,%d,%d,%d\n",
				f.Hour,
				f.SimHour,
				csvEscape(ag.AgentID),
				csvEscape(ag.AgentName),
				csvEscape(ag.Stance),
				ag.SentimentBias,
				ag.ActionCount,
				f.ActionCount,
				len(f.Posts),
			))
		}
	}
	return []byte(sb.String()), nil
}

// ExportMarkdown returns all replay frames as a Markdown report.
func ExportMarkdown(projectID string) ([]byte, error) {
	frames, err := GetFrames(projectID)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Simulation Replay — Project `%s`\n\n", projectID))
	sb.WriteString(fmt.Sprintf("**Total frames:** %d\n\n", len(frames)))
	sb.WriteString("---\n\n")

	for _, f := range frames {
		sb.WriteString(fmt.Sprintf("## Hour %d (Sim time: %02d:00)\n\n", f.Hour, f.SimHour))
		sb.WriteString(fmt.Sprintf("- **Active agents:** %d\n", f.AgentCount))
		sb.WriteString(fmt.Sprintf("- **Actions this hour:** %d\n", f.ActionCount))
		sb.WriteString(fmt.Sprintf("- **Posts in world:** %d\n\n", len(f.Posts)))

		if len(f.Posts) > 0 {
			sb.WriteString("### Recent Posts\n\n")
			limit := 5
			if len(f.Posts) < limit {
				limit = len(f.Posts)
			}
			for _, p := range f.Posts[:limit] {
				sb.WriteString(fmt.Sprintf("- **@%s** [%02d:00, 👍%d]: %s\n",
					p.AuthorName, p.SimHour, p.LikeCount, truncate(p.Content, 120)))
			}
			sb.WriteString("\n")
		}

		if len(f.Agents) > 0 {
			sb.WriteString("### Agent States\n\n")
			sb.WriteString("| Agent | Stance | Sentiment | Actions |\n")
			sb.WriteString("|---|---|---|---|\n")
			for _, ag := range f.Agents {
				sb.WriteString(fmt.Sprintf("| %s | %s | %.2f | %d |\n",
					ag.AgentName, ag.Stance, ag.SentimentBias, ag.ActionCount))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("---\n\n")
	}
	return []byte(sb.String()), nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
