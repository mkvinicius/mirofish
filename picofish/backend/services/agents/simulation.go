// Package agents implements a lightweight multi-agent simulation engine.
// Agents run as goroutines and persist actions to JSON files.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"picofish/services/graph"
	"picofish/services/llm"
	"picofish/storage"
)

// AgentProfile holds the persona and behavioral parameters for an agent.
type AgentProfile struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"project_id"`
	Name          string  `json:"name"`
	Age           int     `json:"age"`
	Profession    string  `json:"profession"`
	Personality   string  `json:"personality"`
	SentimentBias float64 `json:"sentiment_bias"`
	ActivityLevel float64 `json:"activity_level"`
	Interests     string  `json:"interests"`
	Background    string  `json:"background"`
}

// Action represents a single agent action during simulation.
type Action struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	AgentID    string `json:"agent_id"`
	AgentName  string `json:"agent_name"`
	Platform   string `json:"platform"`
	ActionType string `json:"action_type"`
	Content    string `json:"content"`
	Round      int    `json:"round"`
	Timestamp  string `json:"timestamp"`
}

// SimulationState tracks the running simulation.
type SimulationState struct {
	ProjectID    string    `json:"project_id"`
	Status       string    `json:"status"`
	CurrentRound int       `json:"current_round"`
	TotalRounds  int       `json:"total_rounds"`
	AgentCount   int       `json:"agent_count"`
	ActionCount  int       `json:"action_count"`
	StartedAt    time.Time `json:"started_at"`
	Error        string    `json:"error,omitempty"`
}

// Bus is a simple in-memory message bus for agent communication.
type Bus struct {
	mu    sync.RWMutex
	posts []string
	max   int
}

func newBus() *Bus { return &Bus{max: 200} }

func (b *Bus) publish(content string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.posts = append(b.posts, content)
	if len(b.posts) > b.max {
		b.posts = b.posts[len(b.posts)-b.max:]
	}
}

func (b *Bus) sample(n int) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.posts) <= n {
		out := make([]string, len(b.posts))
		copy(out, b.posts)
		return out
	}
	return b.posts[len(b.posts)-n:]
}

// Manager manages simulation lifecycle.
type Manager struct {
	mu     sync.Mutex
	states map[string]*SimulationState
	cancel map[string]context.CancelFunc
}

var Global = &Manager{
	states: make(map[string]*SimulationState),
	cancel: make(map[string]context.CancelFunc),
}

// GenerateProfiles creates agent profiles from graph nodes using the LLM.
func GenerateProfiles(ctx context.Context, projectID string, nodes []graph.Node) ([]AgentProfile, error) {
	var profiles []AgentProfile
	var mu sync.Mutex
	sem := make(chan struct{}, 3)

	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(n graph.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p, err := generateProfile(ctx, projectID, n)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			profiles = append(profiles, *p)
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return nil, err
	}

	// Save to store
	for _, p := range profiles {
		b, _ := json.Marshal(p)
		r := storage.Record{
			"id":         p.ID,
			"project_id": p.ProjectID,
			"name":       p.Name,
			"profile":    string(b),
		}
		if err := storage.DB.Insert("agents", p.ID, r); err != nil {
			return nil, err
		}
	}

	return profiles, nil
}

func generateProfile(ctx context.Context, projectID string, node graph.Node) (*AgentProfile, error) {
	desc := node.Properties["description"]

	prompt := fmt.Sprintf(`Create a social media user profile for this entity.
Entity: %s (type: %s)
Description: %s

Return ONLY valid JSON:
{
  "name": "realistic name",
  "age": 25,
  "profession": "job title",
  "personality": "MBTI or description",
  "sentiment_bias": 0.2,
  "activity_level": 0.7,
  "interests": "comma-separated interests",
  "background": "1-2 sentence backstory"
}

sentiment_bias: -1.0 (very negative) to 1.0 (very positive)
activity_level: 0.0 (rarely posts) to 1.0 (very active)`, node.Name, node.Type, desc)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)}, llm.WithTemperature(0.8))
	if err != nil {
		return nil, err
	}

	var raw struct {
		Name          string  `json:"name"`
		Age           int     `json:"age"`
		Profession    string  `json:"profession"`
		Personality   string  `json:"personality"`
		SentimentBias float64 `json:"sentiment_bias"`
		ActivityLevel float64 `json:"activity_level"`
		Interests     string  `json:"interests"`
		Background    string  `json:"background"`
	}

	if err := extractJSON(resp, &raw); err != nil {
		return &AgentProfile{
			ID:            uuid.NewString(),
			ProjectID:     projectID,
			Name:          node.Name,
			Age:           30,
			Profession:    node.Type,
			Personality:   "neutral",
			ActivityLevel: 0.5,
			Background:    desc,
		}, nil
	}

	return &AgentProfile{
		ID:            uuid.NewString(),
		ProjectID:     projectID,
		Name:          raw.Name,
		Age:           raw.Age,
		Profession:    raw.Profession,
		Personality:   raw.Personality,
		SentimentBias: raw.SentimentBias,
		ActivityLevel: raw.ActivityLevel,
		Interests:     raw.Interests,
		Background:    raw.Background,
	}, nil
}

// LoadProfiles retrieves all agent profiles for a project.
func LoadProfiles(projectID string) ([]AgentProfile, error) {
	records := storage.DB.QueryFunc("agents", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})

	var profiles []AgentProfile
	for _, r := range records {
		var p AgentProfile
		if err := json.Unmarshal([]byte(storage.GetStr(r, "profile")), &p); err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// Start launches the simulation for a project.
func (m *Manager) Start(projectID string, rounds int, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state, ok := m.states[projectID]; ok && state.Status == "running" {
		return fmt.Errorf("simulation already running")
	}

	profiles, err := LoadProfiles(projectID)
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no agents found — complete Step 2 first")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel[projectID] = cancel

	state := &SimulationState{
		ProjectID:   projectID,
		Status:      "running",
		TotalRounds: rounds,
		AgentCount:  len(profiles),
		StartedAt:   time.Now(),
	}
	m.states[projectID] = state

	go m.runSimulation(ctx, projectID, profiles, rounds, topic, state)
	return nil
}

func (m *Manager) Stop(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancel[projectID]; ok {
		cancel()
	}
}

func (m *Manager) GetState(projectID string) *SimulationState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[projectID]
}

// GetRecentActions returns recent simulation actions for a project.
func GetRecentActions(projectID string, limit int) ([]Action, error) {
	records := storage.DB.QueryFunc("simulation_actions", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})

	// Limit to most recent
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}

	var actions []Action
	for _, r := range records {
		actions = append(actions, Action{
			ID:         storage.GetStr(r, "id"),
			ProjectID:  storage.GetStr(r, "project_id"),
			AgentID:    storage.GetStr(r, "agent_id"),
			AgentName:  storage.GetStr(r, "agent_name"),
			Platform:   storage.GetStr(r, "platform"),
			ActionType: storage.GetStr(r, "action_type"),
			Content:    storage.GetStr(r, "content"),
			Round:      storage.GetInt(r, "round"),
			Timestamp:  storage.GetStr(r, "created_at"),
		})
	}
	return actions, nil
}

func (m *Manager) runSimulation(ctx context.Context, projectID string, profiles []AgentProfile, rounds int, topic string, state *SimulationState) {
	bus := newBus()
	platforms := []string{"twitter", "reddit"}

	for round := 1; round <= rounds; round++ {
		select {
		case <-ctx.Done():
			m.setStatus(projectID, "stopped")
			return
		default:
		}

		m.setRound(projectID, round)
		active := selectActive(profiles)

		var wg sync.WaitGroup
		for _, profile := range active {
			wg.Add(1)
			go func(p AgentProfile) {
				defer wg.Done()
				platform := platforms[rand.Intn(len(platforms))]
				action := m.agentAct(ctx, p, platform, topic, bus, round)
				if action == nil {
					return
				}
				saveAction(action)
				bus.publish(action.Content)
				m.incrementActions(projectID)
			}(profile)
		}
		wg.Wait()

		select {
		case <-ctx.Done():
			m.setStatus(projectID, "stopped")
			return
		case <-time.After(400 * time.Millisecond):
		}
	}

	m.setStatus(projectID, "completed")
}

func (m *Manager) agentAct(ctx context.Context, p AgentProfile, platform, topic string, bus *Bus, round int) *Action {
	recent := bus.sample(3)
	contextPosts := ""
	for _, rp := range recent {
		contextPosts += fmt.Sprintf("- %s\n", truncate(rp, 80))
	}

	sentimentDesc := "neutral"
	if p.SentimentBias > 0.3 {
		sentimentDesc = "positive/supportive"
	} else if p.SentimentBias < -0.3 {
		sentimentDesc = "negative/critical"
	}

	prompt := fmt.Sprintf(`You are %s, a %d-year-old %s on %s.
Personality: %s | Sentiment: %s | Interests: %s
Background: %s

Topic: %s

Recent posts:
%s

Round %d: Write a brief, realistic social media post (1-3 sentences). Stay in character.
Reply with just the post content, nothing else.`,
		p.Name, p.Age, p.Profession, platform,
		p.Personality, sentimentDesc, p.Interests,
		p.Background, topic, contextPosts, round)

	content, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.9), llm.WithMaxTokens(200))
	if err != nil {
		return nil
	}

	return &Action{
		ID:         uuid.NewString(),
		ProjectID:  p.ProjectID,
		AgentID:    p.ID,
		AgentName:  p.Name,
		Platform:   platform,
		ActionType: pickActionType(),
		Content:    strings.TrimSpace(content),
		Round:      round,
		Timestamp:  time.Now().Format(time.RFC3339),
	}
}

func pickActionType() string {
	r := rand.Float64()
	if r < 0.6 {
		return "CREATE_POST"
	} else if r < 0.85 {
		return "COMMENT"
	}
	return "LIKE_POST"
}

func selectActive(profiles []AgentProfile) []AgentProfile {
	var active []AgentProfile
	for _, p := range profiles {
		if rand.Float64() < p.ActivityLevel {
			active = append(active, p)
		}
	}
	if len(active) == 0 && len(profiles) > 0 {
		active = append(active, profiles[rand.Intn(len(profiles))])
	}
	return active
}

func saveAction(a *Action) {
	r := storage.Record{
		"id":          a.ID,
		"project_id":  a.ProjectID,
		"agent_id":    a.AgentID,
		"agent_name":  a.AgentName,
		"platform":    a.Platform,
		"action_type": a.ActionType,
		"content":     a.Content,
		"round":       a.Round,
	}
	_ = storage.DB.Insert("simulation_actions", a.ID, r)
}

func (m *Manager) setStatus(projectID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.Status = status
	}
}

func (m *Manager) setRound(projectID string, round int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.CurrentRound = round
	}
}

func (m *Manager) incrementActions(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.ActionCount++
	}
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
