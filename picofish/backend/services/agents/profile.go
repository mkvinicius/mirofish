// Package agents provides OASIS-compatible agent profile generation
// and the full simulation engine.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"picofish/services/graph"
	"picofish/services/llm"
	"picofish/storage"
)

// OasisAgentProfile matches MiroFish's OasisAgentProfile dataclass exactly.
type OasisAgentProfile struct {
	// Core identity
	UserID   int    `json:"user_id"`
	UserName string `json:"user_name"`
	Name     string `json:"name"`
	Bio      string `json:"bio"`
	Persona  string `json:"persona"`

	// Twitter-style fields
	FriendCount   int `json:"friend_count"`
	FollowerCount int `json:"follower_count"`
	StatusesCount int `json:"statuses_count"`

	// Reddit-style fields
	Karma int `json:"karma"`

	// Extended persona
	Age              int      `json:"age"`
	Gender           string   `json:"gender"`
	MBTI             string   `json:"mbti"`
	Country          string   `json:"country"`
	Profession       string   `json:"profession"`
	InterestedTopics []string `json:"interested_topics"`

	// Behavioral config (from simulation_config_generator)
	ActivityLevel    float64  `json:"activity_level"`   // 0.0-1.0
	PostsPerHour     float64  `json:"posts_per_hour"`
	CommentsPerHour  float64  `json:"comments_per_hour"`
	ActiveHours      []int    `json:"active_hours"`       // 24h format
	SentimentBias    float64  `json:"sentiment_bias"`     // -1.0 to 1.0
	Stance           string   `json:"stance"`             // supportive|opposing|neutral|observer
	InfluenceWeight  float64  `json:"influence_weight"`   // 0.0-2.0
	ResponseDelayMin int      `json:"response_delay_min"` // simulated minutes
	ResponseDelayMax int      `json:"response_delay_max"`
	Platform         string   `json:"platform"` // twitter|reddit|both

	// Source entity
	SourceEntityID   string `json:"source_entity_id"`
	SourceEntityType string `json:"source_entity_type"`

	// Internal storage
	ProjectID string `json:"project_id"`
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// ToTwitterFormat matches MiroFish's OasisAgentProfile.to_twitter_format()
func (p *OasisAgentProfile) ToTwitterFormat() map[string]interface{} {
	m := map[string]interface{}{
		"user_id":        p.UserID,
		"username":       p.UserName,
		"name":           p.Name,
		"bio":            p.Bio,
		"persona":        p.Persona,
		"friend_count":   p.FriendCount,
		"follower_count": p.FollowerCount,
		"statuses_count": p.StatusesCount,
		"created_at":     p.CreatedAt,
	}
	if p.Age > 0 {
		m["age"] = p.Age
	}
	if p.Gender != "" {
		m["gender"] = p.Gender
	}
	if p.MBTI != "" {
		m["mbti"] = p.MBTI
	}
	if p.Country != "" {
		m["country"] = p.Country
	}
	if p.Profession != "" {
		m["profession"] = p.Profession
	}
	if len(p.InterestedTopics) > 0 {
		m["interested_topics"] = p.InterestedTopics
	}
	return m
}

// ToRedditFormat matches MiroFish's OasisAgentProfile.to_reddit_format()
func (p *OasisAgentProfile) ToRedditFormat() map[string]interface{} {
	m := map[string]interface{}{
		"user_id":    p.UserID,
		"username":   p.UserName,
		"name":       p.Name,
		"bio":        p.Bio,
		"persona":    p.Persona,
		"karma":      p.Karma,
		"created_at": p.CreatedAt,
	}
	if p.Age > 0 {
		m["age"] = p.Age
	}
	if p.Gender != "" {
		m["gender"] = p.Gender
	}
	if p.MBTI != "" {
		m["mbti"] = p.MBTI
	}
	if p.Country != "" {
		m["country"] = p.Country
	}
	if p.Profession != "" {
		m["profession"] = p.Profession
	}
	if len(p.InterestedTopics) > 0 {
		m["interested_topics"] = p.InterestedTopics
	}
	return m
}

// ── Profile generation ──────────────────────────────────────────────────────

// GenerateProfiles creates OASIS-compatible profiles from graph entities.
// Replicates MiroFish's OasisProfileGenerator.generate_profiles_from_entities()
func GenerateProfiles(ctx context.Context, projectID string, nodes []graph.Node, simRequirement string) ([]*OasisAgentProfile, error) {
	var mu sync.Mutex
	var profiles []*OasisAgentProfile
	sem := make(chan struct{}, 3) // max 3 concurrent LLM calls

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n graph.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p, err := generateSingleProfile(ctx, projectID, n, idx+1, simRequirement)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			mu.Lock()
			profiles = append(profiles, p)
			mu.Unlock()
		}(i, node)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	// Save all profiles to store
	for _, p := range profiles {
		if err := saveProfile(p); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func generateSingleProfile(ctx context.Context, projectID string, node graph.Node, userID int, simRequirement string) (*OasisAgentProfile, error) {
	// Enrich node with its graph edges
	edges, _ := graph.GetNodeEdges(node.ID)
	var edgeFacts []string
	for _, e := range edges {
		edgeFacts = append(edgeFacts, e.Fact)
	}
	edgeContext := ""
	if len(edgeFacts) > 0 {
		edgeContext = "\nRelated facts from the knowledge graph:\n- " + strings.Join(edgeFacts[:min(5, len(edgeFacts))], "\n- ")
	}

	isGroup := isAbstractGroup(node.Type)

	var prompt string
	if isGroup {
		prompt = fmt.Sprintf(`Create a social media user profile for a REPRESENTATIVE MEMBER of this group entity.
Entity: %s (type: %s)
Description: %s%s
Simulation scenario: %s

Create a concrete individual who represents this group. They should embody the typical characteristics,
values, and perspectives of this group entity.

Return ONLY valid JSON:
{
  "user_name": "username_no_spaces",
  "name": "Full Display Name",
  "bio": "Twitter/Reddit bio, 1-2 sentences, in first person",
  "persona": "Detailed 3-5 sentence description of personality, background, motivations, and typical behaviors. Include their relationship to the simulation scenario.",
  "age": 28,
  "gender": "male|female|non-binary",
  "mbti": "INTJ",
  "country": "Country Name",
  "profession": "Specific Job Title",
  "interested_topics": ["topic1", "topic2", "topic3"],
  "friend_count": 320,
  "follower_count": 850,
  "statuses_count": 2400,
  "karma": 15000,
  "activity_level": 0.75,
  "posts_per_hour": 1.2,
  "comments_per_hour": 2.5,
  "sentiment_bias": -0.2,
  "stance": "opposing",
  "influence_weight": 1.3,
  "response_delay_min": 10,
  "response_delay_max": 45
}

stance must be one of: supportive, opposing, neutral, observer
sentiment_bias: -1.0 (very negative) to 1.0 (very positive)
activity_level: 0.0 to 1.0`,
			node.Name, node.Type, node.Summary, edgeContext, simRequirement)
	} else {
		prompt = fmt.Sprintf(`Create a detailed social media user profile for this specific entity.
Entity: %s (type: %s)
Description: %s%s
Simulation scenario: %s

Return ONLY valid JSON:
{
  "user_name": "username_no_spaces",
  "name": "Full Display Name",
  "bio": "Bio in first person, 1-2 sentences",
  "persona": "3-5 sentences: personality, background, motivations, stance on the simulation scenario",
  "age": 35,
  "gender": "male|female|non-binary",
  "mbti": "ENFP",
  "country": "Country Name",
  "profession": "Specific Role",
  "interested_topics": ["topic1", "topic2"],
  "friend_count": 500,
  "follower_count": 1200,
  "statuses_count": 8000,
  "karma": 42000,
  "activity_level": 0.8,
  "posts_per_hour": 2.0,
  "comments_per_hour": 3.0,
  "sentiment_bias": 0.3,
  "stance": "neutral",
  "influence_weight": 1.5,
  "response_delay_min": 5,
  "response_delay_max": 30
}`,
			node.Name, node.Type, node.Summary, edgeContext, simRequirement)
	}

	resp, err := llm.Chat(ctx, []llm.Message{llm.User(prompt)},
		llm.WithTemperature(0.8), llm.WithMaxTokens(1024))
	if err != nil {
		return fallbackProfile(projectID, node, userID), nil
	}

	var raw struct {
		UserName         string   `json:"user_name"`
		Name             string   `json:"name"`
		Bio              string   `json:"bio"`
		Persona          string   `json:"persona"`
		Age              int      `json:"age"`
		Gender           string   `json:"gender"`
		MBTI             string   `json:"mbti"`
		Country          string   `json:"country"`
		Profession       string   `json:"profession"`
		InterestedTopics []string `json:"interested_topics"`
		FriendCount      int      `json:"friend_count"`
		FollowerCount    int      `json:"follower_count"`
		StatusesCount    int      `json:"statuses_count"`
		Karma            int      `json:"karma"`
		ActivityLevel    float64  `json:"activity_level"`
		PostsPerHour     float64  `json:"posts_per_hour"`
		CommentsPerHour  float64  `json:"comments_per_hour"`
		SentimentBias    float64  `json:"sentiment_bias"`
		Stance           string   `json:"stance"`
		InfluenceWeight  float64  `json:"influence_weight"`
		ResponseDelayMin int      `json:"response_delay_min"`
		ResponseDelayMax int      `json:"response_delay_max"`
	}
	if err := llm.ParseJSON(resp, &raw); err != nil {
		return fallbackProfile(projectID, node, userID), nil
	}

	// Determine which platform based on activity style
	platform := "both"
	if raw.FollowerCount > 5000 {
		platform = "twitter"
	} else if raw.Karma > 50000 {
		platform = "reddit"
	}

	return &OasisAgentProfile{
		UserID:           userID,
		UserName:         sanitizeUsername(raw.UserName, node.Name),
		Name:             orDefault(raw.Name, node.Name),
		Bio:              orDefault(raw.Bio, node.Summary),
		Persona:          orDefault(raw.Persona, node.Summary),
		FriendCount:      orDefaultInt(raw.FriendCount, 200),
		FollowerCount:    orDefaultInt(raw.FollowerCount, 500),
		StatusesCount:    orDefaultInt(raw.StatusesCount, 1000),
		Karma:            orDefaultInt(raw.Karma, 5000),
		Age:              orDefaultInt(raw.Age, 30),
		Gender:           raw.Gender,
		MBTI:             raw.MBTI,
		Country:          raw.Country,
		Profession:       orDefault(raw.Profession, node.Type),
		InterestedTopics: raw.InterestedTopics,
		ActivityLevel:    clamp(raw.ActivityLevel, 0.1, 1.0),
		PostsPerHour:     clampF(raw.PostsPerHour, 0.1, 5.0),
		CommentsPerHour:  clampF(raw.CommentsPerHour, 0.1, 8.0),
		ActiveHours:      defaultActiveHours(),
		SentimentBias:    clamp(raw.SentimentBias, -1, 1),
		Stance:           validStance(raw.Stance),
		InfluenceWeight:  clampF(raw.InfluenceWeight, 0.1, 3.0),
		ResponseDelayMin: orDefaultInt(raw.ResponseDelayMin, 10),
		ResponseDelayMax: orDefaultInt(raw.ResponseDelayMax, 60),
		Platform:         platform,
		SourceEntityID:   node.ID,
		SourceEntityType: node.Type,
		ProjectID:        projectID,
		ID:               uuid.NewString(),
	}, nil
}

func saveProfile(p *OasisAgentProfile) error {
	full, _ := json.Marshal(p)
	return storage.DB.Insert("agents", p.ID, storage.Record{
		"id":           p.ID,
		"project_id":   p.ProjectID,
		"name":         p.Name,
		"user_name":    p.UserName,
		"bio":          p.Bio,
		"persona":      p.Persona,
		"profession":   p.Profession,
		"stance":       p.Stance,
		"platform":     p.Platform,
		"activity_level": fmt.Sprintf("%f", p.ActivityLevel),
		"profile_full": string(full),
	})
}

// LoadProfiles returns all agent profiles for a project.
func LoadProfiles(projectID string) ([]*OasisAgentProfile, error) {
	records := storage.DB.QueryFunc("agents", func(r storage.Record) bool {
		return storage.GetStr(r, "project_id") == projectID
	})
	var profiles []*OasisAgentProfile
	for _, r := range records {
		var p OasisAgentProfile
		if err := json.Unmarshal([]byte(storage.GetStr(r, "profile_full")), &p); err == nil {
			profiles = append(profiles, &p)
		}
	}
	return profiles, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

func isAbstractGroup(entityType string) bool {
	abstract := []string{"Group", "Community", "Organization", "Institution",
		"Government", "Party", "Association", "Agency", "Department", "Public"}
	et := strings.ToLower(entityType)
	for _, a := range abstract {
		if strings.Contains(et, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

func sanitizeUsername(raw, fallback string) string {
	username := strings.ReplaceAll(raw, " ", "_")
	username = strings.ReplaceAll(username, "-", "_")
	if username == "" {
		username = strings.ReplaceAll(strings.ToLower(fallback), " ", "_")
	}
	if len(username) > 20 {
		username = username[:20]
	}
	return username
}

func defaultActiveHours() []int {
	// Chinese timezone default (MiroFish behavior)
	return []int{8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22}
}

func fallbackProfile(projectID string, node graph.Node, userID int) *OasisAgentProfile {
	return &OasisAgentProfile{
		UserID:           userID,
		UserName:         sanitizeUsername("", node.Name),
		Name:             node.Name,
		Bio:              node.Summary,
		Persona:          node.Summary,
		FriendCount:      200,
		FollowerCount:    500,
		StatusesCount:    1000,
		Karma:            5000,
		Age:              30,
		Profession:       node.Type,
		ActivityLevel:    0.5,
		PostsPerHour:     1.0,
		CommentsPerHour:  2.0,
		ActiveHours:      defaultActiveHours(),
		SentimentBias:    0,
		Stance:           "neutral",
		InfluenceWeight:  1.0,
		ResponseDelayMin: 10,
		ResponseDelayMax: 60,
		Platform:         "both",
		SourceEntityID:   node.ID,
		SourceEntityType: node.Type,
		ProjectID:        projectID,
		ID:               uuid.NewString(),
	}
}

func validStance(s string) string {
	for _, v := range []string{"supportive", "opposing", "neutral", "observer"} {
		if s == v {
			return s
		}
	}
	return "neutral"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 { return clamp(v, lo, hi) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
