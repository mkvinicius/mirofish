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

	// Instagram actions
	ActionStory       = "STORY"
	ActionReel        = "REEL"

	// TikTok actions
	ActionCreateVideo = "CREATE_VIDEO"
	ActionDuet        = "DUET"
	ActionStitch      = "STITCH"

	// WhatsApp actions
	ActionSendMessage = "SEND_MESSAGE"
	ActionForward     = "FORWARD"
	ActionReact       = "REACT"

	// Facebook actions
	ActionJoinGroup   = "JOIN_GROUP"

	// Platform constants
	PlatformTwitter   = "twitter"
	PlatformReddit    = "reddit"
	PlatformInstagram = "instagram"
	PlatformTikTok    = "tiktok"
	PlatformWhatsApp  = "whatsapp"
	PlatformFacebook  = "facebook"
)

// ── World state ────────────────────────────────────────────────────────────

type Post struct {
	ID           string   `json:"id"`
	Platform     string   `json:"platform"`
	AuthorID     string   `json:"author_id"`
	AuthorName   string   `json:"author_name"`
	Content      string   `json:"content"`
	LikeCount    int      `json:"like_count"`
	RepostCount  int      `json:"repost_count"`
	CommentCount int      `json:"comment_count"`
	ParentID     string   `json:"parent_id"`
	Round        int      `json:"round"`
	SimHour      int      `json:"sim_hour"`
	CreatedAt    string   `json:"created_at"`
	Visibility   string   `json:"visibility,omitempty"`    // "public"|"targeted"|"rumor"
	TargetAgents []string `json:"target_agents,omitempty"` // for targeted/rumor posts
	Score        float64  `json:"-"`                       // feed ranking score
}

type AgentAction struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	SimID        string `json:"simulation_id"`
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	Platform     string `json:"platform"`
	ActionType   string `json:"action_type"`
	Content      string `json:"content"`
	Round        int    `json:"round"`
	SimHour      int    `json:"sim_hour"`
	TargetID     string `json:"target_id"`
	Success      bool   `json:"success"`
	Timestamp    string `json:"timestamp"`
	ReplyTo      string `json:"reply_to,omitempty"`      // @username being replied to
	ReplyContent string `json:"reply_content,omitempty"` // content of post being replied to
}

// InjectionEvent represents a mid-simulation content injection (breaking news, narrative shift).
type InjectionEvent struct {
	Hour         int      `json:"hour"`                    // simulated hour (0-23) to inject
	Content      string   `json:"content"`                 // post content
	TargetAgents []string `json:"target_agents,omitempty"` // empty = broadcast to all
	Visibility   string   `json:"visibility"`              // "public"|"targeted"|"rumor"
	AgentName    string   `json:"agent_name,omitempty"`    // author display name, defaults to "NewsBot"
}

// InfluenceEdge represents a weighted influence relationship between two agents.
type InfluenceEdge struct {
	FromAgentID string  `json:"from_agent_id"`
	ToAgentID   string  `json:"to_agent_id"`
	ActionType  string  `json:"action_type"`
	Hour        int     `json:"sim_hour"`
	Weight      float64 `json:"weight"`
}

// PostSummary is a lightweight post record for replay frames.
type PostSummary struct {
	AuthorName string `json:"author_name"`
	Content    string `json:"content"`
	Platform   string `json:"platform"`
	SimHour    int    `json:"sim_hour"`
	LikeCount  int    `json:"like_count"`
}

// AgentSnapshot captures an agent's state at a specific simulation hour.
type AgentSnapshot struct {
	AgentID       string  `json:"agent_id"`
	AgentName     string  `json:"agent_name"`
	SentimentBias float64 `json:"sentiment_bias"`
	ActionCount   int     `json:"action_count"`
	Stance        string  `json:"stance"`
}

// ReplayFrame records the full world state at one simulation hour.
type ReplayFrame struct {
	ProjectID   string          `json:"project_id"`
	SimID       string          `json:"simulation_id"`
	Hour        int             `json:"hour"`         // wall-clock iteration
	SimHour     int             `json:"sim_hour"`     // 0-23
	AgentCount  int             `json:"agent_count"`
	ActionCount int             `json:"action_count"`
	Posts       []PostSummary   `json:"posts"`
	Agents      []AgentSnapshot `json:"agents"`
	Timestamp   string          `json:"timestamp"`
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
	mu           sync.RWMutex
	posts        map[string]*Post    // id → post
	follows      map[string][]string // agentID → []agentID
	likes        map[string][]string // postID → []agentID
	agentStances map[string]string   // agentID → stance (for echo chamber scoring)
	agentTypes   map[string]string   // agentID → "individual"|"group"
}

func newWorld() *World {
	return &World{
		posts:        make(map[string]*Post),
		follows:      make(map[string][]string),
		likes:        make(map[string][]string),
		agentStances: make(map[string]string),
		agentTypes:   make(map[string]string),
	}
}

// registerAgents populates stance and type maps for echo chamber scoring.
func (w *World) registerAgents(profiles []*OasisAgentProfile) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range profiles {
		w.agentStances[p.ID] = p.Stance
		w.agentTypes[p.ID] = p.AgentType
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
		if p.Platform != platform && platform != "both" && platform != "all" {
			continue
		}
		// Visibility filtering for injected/targeted posts
		switch p.Visibility {
		case "targeted":
			if len(p.TargetAgents) > 0 {
				show := false
				for _, ta := range p.TargetAgents {
					if ta == agentID {
						show = true
						break
					}
				}
				if !show {
					continue
				}
			}
		case "rumor":
			// Rumor posts appear in feeds with 30% probability
			if rand.Float64() > 0.30 {
				continue
			}
		}
		candidates = append(candidates, p)
	}

	// Score each post — calibrated parameters
	now := time.Now().Unix()
	followed := make(map[string]bool)
	for _, f := range w.follows[agentID] {
		followed[f] = true
	}

	for _, p := range candidates {
		// Calibrated recency decay: exp(-0.15 * hours_since_post)
		hoursSincePost := float64(now-parseTime(p.CreatedAt)) / 3600.0
		recencyScore := math.Exp(-0.15 * hoursSincePost)

		// Logarithmic popularity: log(1 + likes + reposts) * 0.3
		popScore := math.Log1p(float64(p.LikeCount+p.RepostCount)) * 0.3

		// Echo chamber: stance-based boosting and suppression
		chamberBoost := 1.0
		if agent != nil && p.AuthorID != "" {
			authorStance := w.agentStances[p.AuthorID]
			if authorStance != "" {
				if agent.Stance == authorStance {
					chamberBoost = 1.8 // same stance: strong algorithmic boost
				} else if isOppositeStance(agent.Stance, authorStance) {
					chamberBoost = 0.4 // opposite stance: suppressed
				}
			} else if followed[p.AuthorID] {
				chamberBoost = 1.8 // explicitly followed: boost
			}
		}

		// Group/institutional agents have 2.5x reach multiplier
		if w.agentTypes[p.AuthorID] == "group" {
			chamberBoost *= 2.5
		}

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
	Seed         int64     `json:"seed,omitempty"` // for reproducibility
	Error        string    `json:"error,omitempty"`
}

// ── Manager ────────────────────────────────────────────────────────────────

type Manager struct {
	mu            sync.Mutex
	states        map[string]*SimState
	cancel        map[string]context.CancelFunc
	injections    map[string][]InjectionEvent  // projectID → pending injections
	influenceNets map[string][]*InfluenceEdge  // projectID → accumulated edges
}

var Global = &Manager{
	states:        make(map[string]*SimState),
	cancel:        make(map[string]context.CancelFunc),
	injections:    make(map[string][]InjectionEvent),
	influenceNets: make(map[string][]*InfluenceEdge),
}

// Start launches the simulation for a project.
// seed is optional: pass a non-zero value for deterministic/reproducible output.
func (m *Manager) Start(projectID string, totalHours int, platform, topic string, seed ...int64) error {
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

	var rngSeed int64
	if len(seed) > 0 && seed[0] != 0 {
		rngSeed = seed[0]
	} else {
		rngSeed = time.Now().UnixNano()
	}

	simID := uuid.NewString()
	state := &SimState{
		ProjectID:   projectID,
		SimID:       simID,
		Status:      "running",
		TotalRounds: totalHours,
		TotalHours:  totalHours,
		AgentCount:  len(profiles),
		Platform:    platform,
		Topic:       topic,
		StartedAt:   time.Now(),
		Seed:        rngSeed,
	}
	m.states[projectID] = state

	go m.runLoop(ctx, projectID, simID, profiles, totalHours, platform, topic, state, rngSeed)
	return nil
}

func (m *Manager) Stop(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancel[projectID]; ok {
		cancel()
	}
}

// Inject queues a mid-simulation event to be processed at the specified hour.
func (m *Manager) Inject(projectID string, event InjectionEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.injections == nil {
		m.injections = make(map[string][]InjectionEvent)
	}
	m.injections[projectID] = append(m.injections[projectID], event)
}

// GetInfluenceNetwork returns a copy of all influence edges recorded for a project.
func (m *Manager) GetInfluenceNetwork(projectID string) []*InfluenceEdge {
	m.mu.Lock()
	defer m.mu.Unlock()
	edges := m.influenceNets[projectID]
	result := make([]*InfluenceEdge, len(edges))
	copy(result, edges)
	return result
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

// Calibrated activity multipliers (China timezone base, tuned for long simulations).
// Dead hour threshold: multiplier < 0.1 → skip agent turns entirely (performance).
var hourMultiplier = map[int]float64{
	0: 0.05, 1: 0.03, 2: 0.02, 3: 0.02, 4: 0.03, 5: 0.05,
	6: 0.15, 7: 0.35, 8: 0.60, 9: 0.80, 10: 0.90, 11: 0.95,
	12: 0.85, 13: 0.75, 14: 0.80, 15: 0.85, 16: 0.90, 17: 0.95,
	18: 1.00, 19: 1.50, 20: 1.40, 21: 1.20, 22: 0.80, 23: 0.40,
}

const deadHourThreshold = 0.1 // below this multiplier, skip agent turns

func (m *Manager) runLoop(ctx context.Context, projectID, simID string,
	profiles []*OasisAgentProfile, totalHours int, platform, topic string, state *SimState, seed int64) {

	// Deterministic RNG — reproducible output when same seed is used
	rng := rand.New(rand.NewSource(seed))

	world := newWorld()
	world.registerAgents(profiles) // populate stance/type maps for feed scoring

	// Load persistent memories from previous simulations
	memories := loadOrInitMemories(projectID, profiles)

	// Initialize episodic memory managers (Upgrade 1)
	memManagers := make(map[string]*AgentMemoryManager, len(profiles))
	for _, p := range profiles {
		memManagers[p.ID] = NewAgentMemoryManager(p.ID, projectID)
	}

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

		// Dead hour check: skip agent turns entirely for performance
		if mult < deadHourThreshold {
			select {
			case <-ctx.Done():
				persistMemories(projectID, memories)
				m.setStatus(projectID, "stopped")
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

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
			if isActiveHour && rng.Float64() < threshold {
				active = append(active, p)
			}
		}
		if len(active) == 0 && len(profiles) > 0 {
			active = []*OasisAgentProfile{profiles[rng.Intn(len(profiles))]}
		}

		activePlatform := platform
		switch platform {
		case "both":
			if simHour >= 19 {
				activePlatform = "twitter"
			} else {
				activePlatform = "reddit"
			}
		case "all":
			// Rotate through all 6 platforms based on hour
			allPlatforms := []string{"twitter", "reddit", "instagram", "tiktok", "whatsapp", "facebook"}
			activePlatform = allPlatforms[simHour%len(allPlatforms)]
		}

		// Process injections scheduled for this simulated hour (before agents act)
		m.processHourInjections(ctx, projectID, simID, world, simHour)

		var hourActions []*AgentAction
		var hourActionsMu sync.Mutex
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		for _, agent := range active {
			wg.Add(1)
			go func(ag *OasisAgentProfile) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				action := m.agentAct(ctx, ag, activePlatform, topic, world,
					memories[ag.ID], memManagers[ag.ID], hour, simHour)
				if action == nil {
					return
				}
				saveSimAction(action)
				m.incrementActions(projectID)
				hourActionsMu.Lock()
				hourActions = append(hourActions, action)
				hourActionsMu.Unlock()
			}(agent)
		}
		wg.Wait()

		// Record influence edges from actions this hour
		m.recordInfluence(projectID, hourActions, simHour, world)

		// Save replay frame for this hour
		m.saveReplayFrame(projectID, simID, hour, simHour, hourActions, profiles, world)

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

	// Mark completed before saving history so the stored status is correct
	state.Status = "completed"
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
// Replicates OASIS agent decision logic with chain-of-thought reasoning.
func (m *Manager) agentAct(ctx context.Context, agent *OasisAgentProfile,
	platform, topic string, world *World, mem *AgentMemory, memMgr *AgentMemoryManager,
	round, simHour int) *AgentAction {

	// Get feed context (recent posts the agent would see) — 8 posts, 200 char truncation
	feed := world.getFeed(agent.ID, platform, 8, agent)
	feedText := ""
	for _, p := range feed {
		feedText += fmt.Sprintf("- @%s: %s [👍%d 🔄%d]\n",
			p.AuthorName, trunc(p.Content, 200), p.LikeCount, p.RepostCount)
	}
	if feedText == "" {
		feedText = "(No posts yet — you could be among the first to post)"
	}

	// Get semantically relevant memories using current topic as query
	var memCtx string
	if len(mem.Memories) > 0 {
		queryEmb, err := llm.Embed(ctx, fmt.Sprintf("%s hour %d", topic, simHour))
		if err == nil && len(queryEmb) > 0 {
			relevant := RetrieveRelevantMemories(mem, queryEmb, 4)
			for _, mr := range relevant {
				memCtx += fmt.Sprintf("- [H%d] %s\n", mr.SimHour, trunc(mr.Content, 100))
			}
		}
	}
	if memCtx == "" && len(mem.Memories) > 0 {
		// fallback to recent
		n := 3
		if len(mem.Memories) < n {
			n = len(mem.Memories)
		}
		recent := mem.Memories[len(mem.Memories)-n:]
		for _, mr := range recent {
			memCtx += fmt.Sprintf("- [H%d] %s\n", mr.SimHour, trunc(mr.Content, 100))
		}
	}

	// Determine platform behavior description
	platformDescMap := map[string]string{
		"twitter":   "X (formerly Twitter) — microblogging platform rebranded in 2023, real-time news, trending topics, public discourse, now owned by Elon Musk",
		"reddit":    "community forums, upvote-driven discussions, niche subreddits, long-form debate",
		"instagram": "visual-first platform, food content thrives, influencer culture, hashtags",
		"tiktok":    "short video platform, trends spread fast, Brazilian expat communities active",
		"whatsapp":  "private group messaging, Brazilian community groups, word-of-mouth",
		"facebook":  "community groups, Brazilian expats in Vancouver, event sharing",
	}
	platformDesc := platformDescMap[platform]
	if platformDesc == "" {
		platformDesc = "social media platform"
	}

	// ── Step 1: Internal monologue (chain-of-thought) ──────────────────────
	// Group/institutional agents skip emotional reasoning — they post rationally.
	var thoughts string
	if agent.AgentType != "group" {
		thinkPrompt := fmt.Sprintf(`You are %s (@%s), %s, %s.
Bio: %s
MBTI: %s | Stance: %s | Sentiment: %s

Platform: %s (%s)
Simulation topic: %s | Time: %02d:00

Your relevant memories:
%s

What you see in your feed:
%s

Think deeply as this person. Write 2-3 sentences of internal thought:
- How do you feel about what you're seeing?
- Does anything in the feed provoke you, inspire you, or concern you?
- What is on your mind right now, in character?

Respond with ONLY your internal thoughts, in first person, no JSON.`,
			agent.Name, agent.UserName, agent.Profession, agent.Country,
			agent.Bio, agent.MBTI, agent.Stance, sentimentLabel(agent.SentimentBias),
			platform, platformDesc,
			topic, simHour, memCtx, feedText)

		t, err := llm.Chat(ctx, []llm.Message{llm.User(thinkPrompt)},
			llm.WithTemperature(0.85), llm.WithMaxTokens(150))
		if err == nil {
			thoughts = t
		}
	}

	// ── Step 2: Action decision (informed by internal thoughts) ───────────

	// Determine available actions based on platform
	var actionList string
	switch platform {
	case "twitter":
		actionList = "CREATE_POST, LIKE_POST, REPOST, REPLY_TO_POST, FOLLOW, DO_NOTHING"
	case "reddit":
		actionList = "CREATE_POST, UPVOTE, DOWNVOTE, COMMENT, SHARE, DO_NOTHING"
	case "instagram":
		actionList = "CREATE_POST, STORY, REEL, LIKE, COMMENT, SHARE, FOLLOW, DO_NOTHING"
	case "tiktok":
		actionList = "CREATE_VIDEO, DUET, STITCH, LIKE, COMMENT, SHARE, FOLLOW, DO_NOTHING"
	case "whatsapp":
		actionList = "SEND_MESSAGE, FORWARD, REACT, DO_NOTHING"
	case "facebook":
		actionList = "CREATE_POST, COMMENT, LIKE, SHARE, JOIN_GROUP, DO_NOTHING"
	default:
		actionList = "CREATE_POST, LIKE_POST, REPLY_TO_POST, SHARE, DO_NOTHING"
	}
	// Group agents cannot follow/unfollow — they don't build personal networks
	if agent.AgentType == "group" {
		actionList = strings.ReplaceAll(actionList, ", FOLLOW", "")
		actionList = strings.ReplaceAll(actionList, "FOLLOW, ", "")
	}

	stanceMap := map[string]string{
		"supportive": "You strongly support and advocate for the topic.",
		"opposing":   "You are critical and skeptical about the topic.",
		"neutral":    "You observe and discuss the topic without strong bias.",
		"observer":   "You mostly observe, occasionally sharing factual information.",
	}
	stanceDesc := stanceMap[agent.Stance]
	// Group agents: override to rational, fact-based communication
	if agent.AgentType == "group" {
		stanceDesc = "You represent an institution/organization. Post factually and professionally. No emotional language."
	}

	// Determine reply target: pick most relevant post from feed
	replyTargetText := ""
	var replyTargetPost *Post
	if len(feed) > 0 {
		replyTargetPost = feed[0]
		replyTargetText = fmt.Sprintf("\nIf you choose to reply/comment, you are replying to: @%s: %s",
			replyTargetPost.AuthorName, trunc(replyTargetPost.Content, 200))
	}

	thoughtsSection := ""
	if thoughts != "" {
		thoughtsSection = fmt.Sprintf("\nYour internal thoughts right now:\n%s\n", thoughts)
	}

	prompt := fmt.Sprintf(`You are %s (@%s), a %d-year-old %s on %s (%s).
Bio: %s
Persona: %s
Your stance: %s
Sentiment tendency: %s
MBTI: %s | Country: %s

Simulation topic: %s
Current simulated time: %02d:00

Your recent activity:
%s
%s
Current feed on %s:
%s
%s
Available actions: %s

Decide what to do. Respond with JSON:
{
  "action": "ACTION_TYPE",
  "content": "your post/comment text (only if action creates content)",
  "reply_to": "@username you are replying to (only if replying)",
  "target_post": "brief description of which post you're interacting with (if applicable)",
  "reasoning": "brief internal reasoning (1 sentence)",
  "stance_shift": 0.0
}

Rules:
- Stay completely in character
- Content should be 1-3 sentences, authentic to your personality
- If DO_NOTHING, content can be empty
- Your stance should influence your content direction
- stance_shift: float between -0.3 and 0.3 (how much this interaction shifts your sentiment; 0 = no change)`,
		agent.Name, agent.UserName, agent.Age, agent.Profession, platform, platformDesc,
		agent.Bio, trunc(agent.Persona, 200),
		stanceDesc,
		sentimentLabel(agent.SentimentBias),
		agent.MBTI, agent.Country,
		topic, simHour,
		memCtx,
		thoughtsSection,
		platform, feedText,
		replyTargetText,
		actionList)

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.9), llm.WithMaxTokens(600))
	if err != nil {
		return nil
	}

	var raw struct {
		Action      string  `json:"action"`
		Content     string  `json:"content"`
		ReplyTo     string  `json:"reply_to"`
		Reasoning   string  `json:"reasoning"`
		StanceShift float64 `json:"stance_shift"`
	}
	if err := llm.ParseJSON(resp, &raw); err != nil {
		return nil
	}

	// Apply stance evolution
	if raw.StanceShift != 0 {
		agent.SentimentBias = clampF(agent.SentimentBias+raw.StanceShift, -1.0, 1.0)
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
		ReplyTo:    raw.ReplyTo,
	}

	// Attach reply content if replying to a specific post
	if replyTargetPost != nil && (action.ActionType == ActionReplyPost || action.ActionType == ActionComment) {
		if action.ReplyTo == "" {
			action.ReplyTo = "@" + replyTargetPost.AuthorName
		}
		action.ReplyContent = trunc(replyTargetPost.Content, 200)
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

	// ── Episodic memory update (Upgrade 1) ─────────────────────────────────
	if memMgr != nil && action.Content != "" {
		memMgr.AddEpisode(ctx, action.Content, topic, round, simHour)

		if agent.AgentType == "group" {
			// Group agents: simplified memory — cap at 10 episodes, no belief decay
			if len(memMgr.Episodes) > 10 {
				memMgr.Episodes = memMgr.Episodes[len(memMgr.Episodes)-10:]
			}
		} else {
			// Individual agents: full episodic memory with belief decay and updates
			memMgr.DecayBeliefs(simHour)
			// Update beliefs from observed feed posts
			for _, post := range feed {
				if post.Content != "" {
					memMgr.UpdateBelief(ctx, topic, post.Content, 0.5, simHour)
				}
			}
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
		// Twitter
		ActionCreatePost: true,
		ActionLikePost:   true,
		ActionRepost:     true,
		ActionReplyPost:  true,
		ActionFollow:     true,
		ActionDoNothing:  true,
		// Reddit
		ActionUpvote:     true,
		ActionDownvote:   true,
		ActionComment:    true,
		ActionShare:      true,
		ActionCollect:    true,
		// Instagram
		ActionStory:       true,
		ActionReel:        true,
		"LIKE":            true,
		// TikTok
		ActionCreateVideo: true,
		ActionDuet:        true,
		ActionStitch:      true,
		// WhatsApp
		ActionSendMessage: true,
		ActionForward:     true,
		ActionReact:       true,
		// Facebook
		ActionJoinGroup:   true,
	}
	if valid[action] {
		return action
	}
	return ActionCreatePost
}

// isOppositeStance returns true when two stances directly conflict.
func isOppositeStance(a, b string) bool {
	return (a == "supportive" && b == "opposing") ||
		(a == "opposing" && b == "supportive")
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

// ── Phase 2: Injection, Influence, Replay ─────────────────────────────────

// processHourInjections consumes any injections scheduled for the current simHour.
func (m *Manager) processHourInjections(ctx context.Context, projectID, simID string, world *World, simHour int) {
	m.mu.Lock()
	var pending, remaining []InjectionEvent
	for _, inj := range m.injections[projectID] {
		if inj.Hour == simHour {
			pending = append(pending, inj)
		} else {
			remaining = append(remaining, inj)
		}
	}
	m.injections[projectID] = remaining
	m.mu.Unlock()

	for _, inj := range pending {
		m.applyInjection(ctx, projectID, simID, world, inj, simHour)
	}
}

// applyInjection creates a synthetic post from an injection event.
func (m *Manager) applyInjection(_ context.Context, projectID, simID string, world *World, inj InjectionEvent, simHour int) {
	authorName := inj.AgentName
	if authorName == "" {
		authorName = "NewsBot"
	}
	visibility := inj.Visibility
	if visibility == "" {
		visibility = "public"
	}
	post := &Post{
		ID:           uuid.NewString(),
		Platform:     "twitter",
		AuthorID:     "injection-" + projectID,
		AuthorName:   authorName,
		Content:      inj.Content,
		SimHour:      simHour,
		Round:        simHour,
		CreatedAt:    time.Now().Format(time.RFC3339),
		Visibility:   visibility,
		TargetAgents: inj.TargetAgents,
	}
	world.addPost(post)

	action := &AgentAction{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		SimID:      simID,
		AgentID:    "injection-" + projectID,
		AgentName:  authorName,
		Platform:   "twitter",
		ActionType: ActionCreatePost,
		Content:    inj.Content,
		Round:      simHour,
		SimHour:    simHour,
		TargetID:   post.ID,
		Success:    true,
		Timestamp:  time.Now().Format(time.RFC3339),
	}
	saveSimAction(action)
}

// influenceWeight returns the social influence weight for an action type.
func influenceWeight(actionType string) float64 {
	switch actionType {
	case ActionFollow:
		return 0.5
	case ActionRepost, ActionShare, ActionForward:
		return 0.4
	case ActionReplyPost, ActionComment:
		return 0.3
	case ActionUpvote:
		return 0.2
	case ActionLikePost, ActionReact:
		return 0.1
	default:
		return 0.0
	}
}

// recordInfluence records InfluenceEdge entries for all actions in an hour.
// Resolves post authors via world.posts to get real agent-to-agent relationships.
func (m *Manager) recordInfluence(projectID string, actions []*AgentAction, simHour int, world *World) {
	if len(actions) == 0 {
		return
	}
	world.mu.RLock()
	postAuthors := make(map[string]string, len(world.posts))
	for id, p := range world.posts {
		postAuthors[id] = p.AuthorID
	}
	world.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.influenceNets == nil {
		m.influenceNets = make(map[string][]*InfluenceEdge)
	}
	for _, a := range actions {
		w := influenceWeight(a.ActionType)
		if w == 0 {
			continue
		}
		toAgentID := postAuthors[a.TargetID]
		if toAgentID == "" || toAgentID == a.AgentID {
			continue
		}
		m.influenceNets[projectID] = append(m.influenceNets[projectID], &InfluenceEdge{
			FromAgentID: a.AgentID,
			ToAgentID:   toAgentID,
			ActionType:  a.ActionType,
			Hour:        simHour,
			Weight:      w,
		})
	}
}

// saveReplayFrame persists a snapshot of the world state for one simulation hour.
func (m *Manager) saveReplayFrame(projectID, simID string, hour, simHour int,
	actions []*AgentAction, profiles []*OasisAgentProfile, world *World) {

	world.mu.RLock()
	var posts []PostSummary
	for _, p := range world.posts {
		posts = append(posts, PostSummary{
			AuthorName: p.AuthorName,
			Content:    trunc(p.Content, 150),
			Platform:   p.Platform,
			SimHour:    p.SimHour,
			LikeCount:  p.LikeCount,
		})
	}
	world.mu.RUnlock()

	// Most recent posts first, cap at 20
	sort.Slice(posts, func(i, j int) bool { return posts[i].SimHour > posts[j].SimHour })
	if len(posts) > 20 {
		posts = posts[:20]
	}

	// Count actions per agent this hour
	agentActionCounts := make(map[string]int, len(profiles))
	for _, a := range actions {
		agentActionCounts[a.AgentID]++
	}

	agentSnaps := make([]AgentSnapshot, 0, len(profiles))
	for _, p := range profiles {
		agentSnaps = append(agentSnaps, AgentSnapshot{
			AgentID:       p.ID,
			AgentName:     p.Name,
			SentimentBias: p.SentimentBias,
			ActionCount:   agentActionCounts[p.ID],
			Stance:        p.Stance,
		})
	}

	frame := ReplayFrame{
		ProjectID:   projectID,
		SimID:       simID,
		Hour:        hour,
		SimHour:     simHour,
		AgentCount:  len(profiles),
		ActionCount: len(actions),
		Posts:       posts,
		Agents:      agentSnaps,
		Timestamp:   time.Now().Format(time.RFC3339),
	}
	b, _ := json.Marshal(frame)
	frameID := fmt.Sprintf("%s_%s_h%03d", projectID, simID, hour)
	_ = storage.DB.Insert("replay_frames", frameID, storage.Record{
		"id":         frameID,
		"project_id": projectID,
		"sim_id":     simID,
		"hour":       fmt.Sprintf("%d", hour),
		"sim_hour":   fmt.Sprintf("%d", simHour),
		"data":       string(b),
	})
}
