// engine.go implements the full OASIS-equivalent simulation engine.
// Replicates: time-based scheduling (China timezone), all action types,
// feed algorithm (recency + popularity), echo chamber effects,
// per-agent stance/activity/influence, and agent memory with semantic retrieval.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"picofish/services/llm"
	"picofish/storage"
)

// ── Action types (matches OASIS exactly) ──────────────────────────────────

const (
	// Twitter actions
	ActionCreatePost  = "CREATE_POST"
	ActionLikePost    = "LIKE_POST"
	ActionRepost      = "REPOST"
	ActionReplyPost   = "REPLY_TO_POST"
	ActionFollow      = "FOLLOW"
	ActionDoNothing   = "DO_NOTHING"

	// Reddit actions (superset)
	ActionUpvote      = "UPVOTE"
	ActionDownvote    = "DOWNVOTE"
	ActionComment     = "COMMENT"
	ActionShare       = "SHARE"
	ActionCollect     = "COLLECT"
)

// ── World state ────────────────────────────────────────────────────────────

type Post struct {
	ID          string  `json:"id"`
	Platform    string  `json:"platform"`
	AuthorID    string  `json:"author_id"`
	AuthorName  string  `json:"author_name"`
	Content     string  `json:"content"`
	LikeCount   int     `json:"like_count"`
	RepostCount int     `json:"repost_count"`
	CommentCount int    `json:"comment_count"`
	ParentID    string  `json:"parent_id"`
	Round       int     `json:"round"`
	SimHour     int     `json:"sim_hour"`
	CreatedAt   string  `json:"created_at"`
	Score       float64 `json:"-"` // feed ranking score
}

type AgentAction struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	SimID      string `json:"simulation_id"`
	AgentID    string `json:"agent_id"`
	AgentName  string `json:"agent_name"`
	Platform   string `json:"platform"`
	ActionType string `json:"action_type"`
	Content    string `json:"content"`
	Round      int    `json:"round"`
	SimHour    int    `json:"sim_hour"`
	TargetID   string `json:"target_id"`
	Success    bool   `json:"success"`
	Timestamp  string `json:"timestamp"`
}

// AgentMemory stores an agent's recent experiences for context.
type AgentMemory struct {
	AgentID   string        `json:"agent_id"`
	ProjectID string        `json:"project_id"`
	Memories  []MemoryEntry `json:"memories"`
}

type MemoryEntry struct {
	Content   string    `json:"content"`
	Embedding []float64 `json:"embedding,omitempty"`
	Round     int       `json:"round"`
	SimHour   int       `json:"sim_hour"`
	CreatedAt string    `json:"created_at"`
}

// World is the shared simulation state.
type World struct {
	mu      sync.RWMutex
	posts   map[string]*Post   // id → post
	follows map[string][]string // agentID → []agentID
	likes   map[string][]string // postID → []agentID
}

func newWorld() *World {
	return &World{
		posts:   make(map[string]*Post),
		follows: make(map[string][]string),
		likes:   make(map[string][]string),
	}
}

func (w *World) addPost(p *Post) {
	w.mu.Lock()
	w.posts[p.ID] = p
	w.mu.Unlock()
}

func (w *World) like(postID, agentID string) {
	w.mu.Lock()
	w.likes[postID] = append(w.likes[postID], agentID)
	if p, ok := w.posts[postID]; ok {
		p.LikeCount++
	}
	w.mu.Unlock()
}

func (w *World) repost(postID string) {
	w.mu.Lock()
	if p, ok := w.posts[postID]; ok {
		p.RepostCount++
	}
	w.mu.Unlock()
}

// getFeed returns a ranked feed for an agent.
// Replicates OASIS feed algorithm: recency + popularity + echo chamber.
func (w *World) getFeed(agentID, platform string, size int, agent *OasisAgentProfile) []*Post {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var candidates []*Post
	for _, p := range w.posts {
		if p.Platform != platform && platform != "both" {
			continue
		}
		candidates = append(candidates, p)
	}

	// Score each post
	now := time.Now().Unix()
	followed := make(map[string]bool)
	for _, f := range w.follows[agentID] {
		followed[f] = true
	}

	for _, p := range candidates {
		// Recency decay (exponential)
		ageMin := float64(now-parseTime(p.CreatedAt)) / 60.0
		recencyScore := math.Exp(-ageMin / 120.0) // half-life 2 hours

		// Popularity
		popScore := math.Log1p(float64(p.LikeCount+p.RepostCount*2+p.CommentCount)) / 10.0

		// Echo chamber: boost posts from same-stance agents
		chamberBoost := 1.0
		if agent != nil && p.AuthorID != "" {
			if isFollowed := followed[p.AuthorID]; isFollowed {
				chamberBoost = 1.5
			}
		}

		// Influence weight boost
		p.Score = (recencyScore*0.5 + popScore*0.3 + 0.2) * chamberBoost
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > size {
		candidates = candidates[:size]
	}
	return candidates
}

// ── Simulation state ───────────────────────────────────────────────────────

type SimState struct {
	ProjectID    string    `json:"project_id"`
	SimID        string    `json:"simulation_id"`
	Status       string    `json:"status"` // running|completed|stopped|error
	CurrentRound int       `json:"current_round"`
	TotalRounds  int       `json:"total_rounds"`
	CurrentHour  int       `json:"current_hour"`  // simulated hour (0-23)
	TotalHours   int       `json:"total_hours"`   // total simulated hours
	AgentCount   int       `json:"agent_count"`
	ActionCount  int       `json:"action_count"`
	Platform     string    `json:"platform"` // twitter|reddit|both
	Topic        string    `json:"topic"`
	StartedAt    time.Time `json:"started_at"`
	Error        string    `json:"error,omitempty"`
}

// ── Manager ────────────────────────────────────────────────────────────────

type Manager struct {
	mu     sync.Mutex
	states map[string]*SimState
	cancel map[string]context.CancelFunc
}

var Global = &Manager{
	states: make(map[string]*SimState),
	cancel: make(map[string]context.CancelFunc),
}

// Start launches the simulation for a project.
func (m *Manager) Start(projectID string, totalHours int, platform, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.states[projectID]; ok && s.Status == "running" {
		return fmt.Errorf("simulation already running")
	}

	profiles, err := LoadProfiles(projectID)
	if err != nil || len(profiles) == 0 {
		return fmt.Errorf("no agents found — complete Step 2 first")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel[projectID] = cancel

	simID := uuid.NewString()
	state := &SimState{
		ProjectID:  projectID,
		SimID:      simID,
		Status:     "running",
		TotalRounds: totalHours,
		TotalHours: totalHours,
		AgentCount: len(profiles),
		Platform:   platform,
		Topic:      topic,
		StartedAt:  time.Now(),
	}
	m.states[projectID] = state

	go m.runLoop(ctx, projectID, simID, profiles, totalHours, platform, topic, state)
	return nil
}

func (m *Manager) Stop(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancel[projectID]; ok {
		cancel()
	}
}

func (m *Manager) GetState(projectID string) *SimState {
	m.mu.Lock()
	s := m.states[projectID]
	m.mu.Unlock()
	if s != nil {
		return s
	}
	// After a server restart, rebuild state from the most recent sim_history entry.
	history := GetSimHistory(projectID)
	if len(history) == 0 {
		return nil
	}
	h := history[0]
	t, _ := time.Parse(time.RFC3339, h.StartedAt)
	return &SimState{
		ProjectID:   projectID,
		SimID:       h.SimID,
		Status:      h.Status,
		TotalHours:  h.TotalHours,
		CurrentHour: h.TotalHours,
		AgentCount:  h.AgentCount,
		ActionCount: h.ActionCount,
		Platform:    h.Platform,
		Topic:       h.Topic,
		StartedAt:   t,
	}
}

// ── Main simulation loop ───────────────────────────────────────────────────

// China timezone activity multipliers (from MiroFish's simulation_config_generator.py)
var hourMultiplier = map[int]float64{
	0: 0.05, 1: 0.05, 2: 0.05, 3: 0.05, 4: 0.05, 5: 0.05, // dead hours
	6: 0.4, 7: 0.4, 8: 0.4,                                  // morning
	9: 0.7, 10: 0.7, 11: 0.7, 12: 0.7, 13: 0.7, 14: 0.7,   // work hours
	15: 0.7, 16: 0.7, 17: 0.7, 18: 0.7,
	19: 1.5, 20: 1.5, 21: 1.5, 22: 1.5,                     // evening peak
	23: 0.5,                                                  // night
}

func (m *Manager) runLoop(ctx context.Context, projectID, simID string,
	profiles []*OasisAgentProfile, totalHours int, platform, topic string, state *SimState) {

	world := newWorld()

	// Load persistent memories from previous simulations
	memories := loadOrInitMemories(projectID, profiles)

	// Build initial social graph: agents with same stance follow each other
	initSocialGraph(world, profiles)

	startHour := 8

	for hour := 0; hour < totalHours; hour++ {
		select {
		case <-ctx.Done():
			persistMemories(projectID, memories)
			m.setStatus(projectID, "stopped")
			return
		default:
		}

		simHour := (startHour + hour) % 24
		m.setHour(projectID, hour+1, simHour)

		mult := hourMultiplier[simHour]
		var active []*OasisAgentProfile
		for _, p := range profiles {
			isActiveHour := false
			for _, h := range p.ActiveHours {
				if h == simHour {
					isActiveHour = true
					break
				}
			}
			threshold := p.ActivityLevel * mult
			if isActiveHour && rand.Float64() < threshold {
				active = append(active, p)
			}
		}
		if len(active) == 0 && len(profiles) > 0 {
			active = []*OasisAgentProfile{profiles[rand.Intn(len(profiles))]}
		}

		activePlatform := platform
		if platform == "both" {
			if simHour >= 19 {
				activePlatform = "twitter"
			} else {
				activePlatform = "reddit"
			}
		}

		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		for _, agent := range active {
			wg.Add(1)
			go func(ag *OasisAgentProfile) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				action := m.agentAct(ctx, ag, activePlatform, topic, world, memories[ag.ID], hour, simHour)
				if action == nil {
					return
				}
				saveSimAction(action)
				m.incrementActions(projectID)
			}(agent)
		}
		wg.Wait()

		// Viral cascade: check for trending posts and shift agent sentiments
		applyViralCascade(world, profiles, simHour)

		select {
		case <-ctx.Done():
			persistMemories(projectID, memories)
			m.setStatus(projectID, "stopped")
			return
		case <-time.After(300 * time.Millisecond):
		}
	}

	// Persist memories for next simulation
	persistMemories(projectID, memories)

	// Save simulation run to history
	saveSimHistory(projectID, simID, state)

	m.setStatus(projectID, "completed")
}

// initSocialGraph seeds the follow graph: same-stance agents follow each other.
func initSocialGraph(world *World, profiles []*OasisAgentProfile) {
	byStance := make(map[string][]string)
	for _, p := range profiles {
		byStance[p.Stance] = append(byStance[p.Stance], p.ID)
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	for _, ids := range byStance {
		for i, id := range ids {
			for j, other := range ids {
				if i != j {
					world.follows[id] = append(world.follows[id], other)
				}
			}
		}
	}
	// High-influence agents get cross-stance followers too
	for _, p := range profiles {
		if p.InfluenceWeight >= 1.8 {
			for _, other := range profiles {
				if other.ID != p.ID {
					world.follows[other.ID] = append(world.follows[other.ID], p.ID)
				}
			}
		}
	}
}

// applyViralCascade detects posts with high engagement and shifts agent sentiment.
// Replicates OASIS cascade dynamics: viral content biases agents toward its tone.
func applyViralCascade(world *World, profiles []*OasisAgentProfile, simHour int) {
	world.mu.RLock()
	var viral []*Post
	for _, p := range world.posts {
		if p.LikeCount+p.RepostCount*2 >= 5 {
			viral = append(viral, p)
		}
	}
	world.mu.RUnlock()

	if len(viral) == 0 {
		return
	}

	// Determine dominant sentiment of viral posts (positive content = positive bias shift)
	positiveViral, negativeViral := 0, 0
	for _, p := range viral {
		text := strings.ToLower(p.Content)
		for _, w := range []string{"great", "support", "agree", "good", "excellent", "amazing"} {
			if strings.Contains(text, w) {
				positiveViral++
			}
		}
		for _, w := range []string{"bad", "wrong", "oppose", "crisis", "fail", "terrible"} {
			if strings.Contains(text, w) {
				negativeViral++
			}
		}
	}

	shift := 0.0
	if positiveViral > negativeViral {
		shift = 0.05
	} else if negativeViral > positiveViral {
		shift = -0.05
	}
	if shift == 0 {
		return
	}

	// Nudge neutral/observer agents toward the viral sentiment
	for _, p := range profiles {
		if p.Stance == "neutral" || p.Stance == "observer" {
			p.SentimentBias = clampF(p.SentimentBias+shift, -1.0, 1.0)
		}
	}
}

// ── Persistent memory ──────────────────────────────────────────────────────

func loadOrInitMemories(projectID string, profiles []*OasisAgentProfile) map[string]*AgentMemory {
	memories := make(map[string]*AgentMemory)
	for _, p := range profiles {
		mem := loadMemory(projectID, p.ID)
		if mem == nil {
			mem = &AgentMemory{AgentID: p.ID, ProjectID: projectID}
		}
		memories[p.ID] = mem
	}
	return memories
}

func loadMemory(projectID, agentID string) *AgentMemory {
	records := storage.DB.QueryFunc("agent_memories", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID &&
			storage.GetStr(r, "agent_id") == agentID
	})
	if len(records) == 0 {
		return nil
	}
	var mem AgentMemory
	if err := json.Unmarshal([]byte(storage.GetStr(records[0], "data")), &mem); err != nil {
		return nil
	}
	return &mem
}

func persistMemories(projectID string, memories map[string]*AgentMemory) {
	for _, mem := range memories {
		if len(mem.Memories) == 0 {
			continue
		}
		b, err := json.Marshal(mem)
		if err != nil {
			continue
		}
		id := projectID + "_" + mem.AgentID
		_ = storage.DB.Insert("agent_memories", id, storage.Record{
			"id":         id,
			"project_id": projectID,
			"agent_id":   mem.AgentID,
			"data":       string(b),
		})
	}
}

// agentAct decides an action for one agent using LLM.
// Replicates OASIS agent decision logic.
func (m *Manager) agentAct(ctx context.Context, agent *OasisAgentProfile,
	platform, topic string, world *World, mem *AgentMemory, round, simHour int) *AgentAction {

	// Get feed context (recent posts the agent would see)
	feed := world.getFeed(agent.ID, platform, 5, agent)
	feedText := ""
	for _, p := range feed {
		feedText += fmt.Sprintf("- @%s: %s [👍%d 🔄%d]\n",
			p.AuthorName, trunc(p.Content, 100), p.LikeCount, p.RepostCount)
	}
	if feedText == "" {
		feedText = "(No posts yet — you could be among the first to post)"
	}

	// Get relevant memories
	memCtx := ""
	if len(mem.Memories) > 0 {
		n := 3
		if len(mem.Memories) < n {
			n = len(mem.Memories)
		}
		recent := mem.Memories[len(mem.Memories)-n:]
		for _, mr := range recent {
			memCtx += fmt.Sprintf("- [H%d] %s\n", mr.SimHour, trunc(mr.Content, 80))
		}
	}

	// Determine available actions based on platform
	var actionList string
	if platform == "twitter" {
		actionList = "CREATE_POST, LIKE_POST, REPOST, REPLY_TO_POST, FOLLOW, DO_NOTHING"
	} else {
		actionList = "CREATE_POST, UPVOTE, DOWNVOTE, COMMENT, SHARE, DO_NOTHING"
	}

	stanceMap := map[string]string{
		"supportive": "You strongly support and advocate for the topic.",
		"opposing":   "You are critical and skeptical about the topic.",
		"neutral":    "You observe and discuss the topic without strong bias.",
		"observer":   "You mostly observe, occasionally sharing factual information.",
	}
	stanceDesc := stanceMap[agent.Stance]

	prompt := fmt.Sprintf(`You are %s (@%s), a %d-year-old %s on %s.
Bio: %s
Persona: %s
Your stance: %s
Sentiment tendency: %s
MBTI: %s | Country: %s

Simulation topic: %s
Current simulated time: %02d:00

Your recent activity:
%s

Current feed on %s:
%s

Available actions: %s

Decide what to do. Respond with JSON:
{
  "action": "ACTION_TYPE",
  "content": "your post/comment text (only if action creates content)",
  "target_post": "brief description of which post you're interacting with (if applicable)",
  "reasoning": "brief internal reasoning (1 sentence)"
}

Rules:
- Stay completely in character
- Content should be 1-3 sentences, authentic to your personality
- If DO_NOTHING, content can be empty
- Your stance should influence your content direction`,
		agent.Name, agent.UserName, agent.Age, agent.Profession, platform,
		agent.Bio, trunc(agent.Persona, 200),
		stanceDesc,
		sentimentLabel(agent.SentimentBias),
		agent.MBTI, agent.Country,
		topic, simHour,
		memCtx,
		platform, feedText,
		actionList)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.9), llm.WithMaxTokens(400))
	if err != nil {
		return nil
	}

	var raw struct {
		Action    string `json:"action"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning"`
	}
	if err := llm.ParseJSON(resp, &raw); err != nil {
		return nil
	}

	action := &AgentAction{
		ID:         uuid.NewString(),
		ProjectID:  agent.ProjectID,
		AgentID:    agent.ID,
		AgentName:  agent.Name,
		Platform:   platform,
		ActionType: normalizeAction(raw.Action, platform),
		Content:    strings.TrimSpace(raw.Content),
		Round:      round,
		SimHour:    simHour,
		Success:    true,
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	// Apply action to world state
	if action.ActionType == ActionCreatePost || action.ActionType == ActionComment || action.ActionType == ActionReplyPost {
		if action.Content != "" {
			p := &Post{
				ID:        uuid.NewString(),
				Platform:  platform,
				AuthorID:  agent.ID,
				AuthorName: agent.UserName,
				Content:   action.Content,
				Round:     round,
				SimHour:   simHour,
				CreatedAt: time.Now().Format(time.RFC3339),
			}
			world.addPost(p)
			action.TargetID = p.ID

			// Store memory
			addMemory(ctx, mem, action.Content, round, simHour)
		}
	} else if action.ActionType == ActionLikePost || action.ActionType == ActionUpvote {
		feed2 := world.getFeed(agent.ID, platform, 3, agent)
		if len(feed2) > 0 {
			target := feed2[0]
			world.like(target.ID, agent.ID)
			action.TargetID = target.ID
		}
	} else if action.ActionType == ActionRepost || action.ActionType == ActionShare {
		feed2 := world.getFeed(agent.ID, platform, 3, agent)
		if len(feed2) > 0 {
			target := feed2[0]
			world.repost(target.ID)
			action.TargetID = target.ID
		}
	}

	return action
}

// ── Memory ─────────────────────────────────────────────────────────────────

func addMemory(ctx context.Context, mem *AgentMemory, content string, round, simHour int) {
	entry := MemoryEntry{
		Content:   content,
		Round:     round,
		SimHour:   simHour,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	// Generate embedding for semantic retrieval (best-effort)
	if emb, err := llm.Embed(ctx, content); err == nil {
		entry.Embedding = emb
	}
	mem.Memories = append(mem.Memories, entry)
	// Keep last 50 memories per agent
	if len(mem.Memories) > 50 {
		mem.Memories = mem.Memories[len(mem.Memories)-50:]
	}
}

// RetrieveRelevantMemories returns the most semantically relevant memories for a query.
func RetrieveRelevantMemories(mem *AgentMemory, queryEmb []float64, topK int) []MemoryEntry {
	type scored struct {
		entry MemoryEntry
		score float64
	}
	var candidates []scored
	for _, m := range mem.Memories {
		if len(m.Embedding) > 0 {
			candidates = append(candidates, scored{m, llm.CosineSimilarity(queryEmb, m.Embedding)})
		} else {
			candidates = append(candidates, scored{m, 0.5}) // recent without embedding
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	var result []MemoryEntry
	for i, c := range candidates {
		if i >= topK {
			break
		}
		result = append(result, c.entry)
	}
	return result
}

// ── Queries ────────────────────────────────────────────────────────────────

func saveSimAction(a *AgentAction) {
	b, _ := json.Marshal(a)
	_ = storage.DB.Insert("simulation_actions", a.ID, storage.Record{
		"id":          a.ID,
		"project_id":  a.ProjectID,
		"agent_id":    a.AgentID,
		"agent_name":  a.AgentName,
		"platform":    a.Platform,
		"action_type": a.ActionType,
		"content":     a.Content,
		"round":       fmt.Sprintf("%d", a.Round),
		"sim_hour":    fmt.Sprintf("%d", a.SimHour),
		"data":        string(b),
	})
}

// GetRecentActions returns recent simulation actions for a project.
func GetRecentActions(projectID string, limit int) ([]*AgentAction, error) {
	records := storage.DB.QueryFunc("simulation_actions", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	var actions []*AgentAction
	for _, r := range records {
		var a AgentAction
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &a); err == nil {
			actions = append(actions, &a)
		}
	}
	return actions, nil
}

// GetActionSummary returns aggregated stats for a project's simulation.
func GetActionSummary(projectID string) map[string]interface{} {
	actions, _ := GetRecentActions(projectID, 0)
	typeCount := map[string]int{}
	platformCount := map[string]int{}
	agents := map[string]int{}
	for _, a := range actions {
		typeCount[a.ActionType]++
		platformCount[a.Platform]++
		agents[a.AgentName]++
	}
	return map[string]interface{}{
		"total":    len(actions),
		"by_type":  typeCount,
		"by_platform": platformCount,
		"agent_count": len(agents),
	}
}

// ── State helpers ──────────────────────────────────────────────────────────

func (m *Manager) setStatus(projectID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.Status = status
	}
}

func (m *Manager) setHour(projectID string, round, simHour int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.CurrentRound = round
		s.CurrentHour = simHour
	}
}

func (m *Manager) incrementActions(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[projectID]; ok {
		s.ActionCount++
	}
}

// ── Utility ────────────────────────────────────────────────────────────────

func normalizeAction(action, platform string) string {
	action = strings.ToUpper(strings.TrimSpace(action))
	valid := map[string]bool{
		ActionCreatePost: true,
		ActionLikePost:   true,
		ActionRepost:     true,
		ActionReplyPost:  true,
		ActionFollow:     true,
		ActionDoNothing:  true,
		ActionUpvote:     true,
		ActionDownvote:   true,
		ActionComment:    true,
		ActionShare:      true,
		ActionCollect:    true,
	}
	if valid[action] {
		return action
	}
	return ActionCreatePost
}

func sentimentLabel(bias float64) string {
	if bias > 0.5 {
		return "very positive/enthusiastic"
	} else if bias > 0.2 {
		return "generally positive"
	} else if bias < -0.5 {
		return "very negative/critical"
	} else if bias < -0.2 {
		return "generally skeptical"
	}
	return "balanced/neutral"
}

func parseTime(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().Unix()
	}
	return t.Unix()
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ── Simulation history ─────────────────────────────────────────────────────

type SimHistoryEntry struct {
	SimID       string `json:"sim_id"`
	ProjectID   string `json:"project_id"`
	Topic       string `json:"topic"`
	Platform    string `json:"platform"`
	TotalHours  int    `json:"total_hours"`
	AgentCount  int    `json:"agent_count"`
	ActionCount int    `json:"action_count"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

func saveSimHistory(projectID, simID string, state *SimState) {
	entry := SimHistoryEntry{
		SimID:       simID,
		ProjectID:   projectID,
		Topic:       state.Topic,
		Platform:    state.Platform,
		TotalHours:  state.TotalHours,
		AgentCount:  state.AgentCount,
		ActionCount: state.ActionCount,
		Status:      state.Status,
		StartedAt:   state.StartedAt.Format(time.RFC3339),
		CompletedAt: time.Now().Format(time.RFC3339),
	}
	b, _ := json.Marshal(entry)
	_ = storage.DB.Insert("sim_history", simID, storage.Record{
		"id":         simID,
		"project_id": projectID,
		"data":       string(b),
		"started_at": entry.StartedAt,
	})
}

// GetSimHistory returns all past simulation runs for a project.
func GetSimHistory(projectID string) []SimHistoryEntry {
	records := storage.DB.QueryFunc("sim_history", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	var entries []SimHistoryEntry
	for _, r := range records {
		var e SimHistoryEntry
		if err := json.Unmarshal([]byte(storage.GetStr(r, "data")), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries
}
