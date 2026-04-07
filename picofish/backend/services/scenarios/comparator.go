// Package scenarios implements the Multi-Scenario Comparison Engine.
// Runs 2–4 parallel scenario variants (different seeds, topic tweaks, or
// agent patches) and computes statistical divergence metrics without LLM calls.
package scenarios

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"picofish/services/agents"
	"picofish/storage"

	"github.com/google/uuid"
)

// ── Types ──────────────────────────────────────────────────────────────────

// AgentPatch overrides specific fields for a named agent in a scenario.
type AgentPatch struct {
	AgentName     string  `json:"agent_name"`
	StanceOverride string  `json:"stance_override,omitempty"` // "supportive"|"opposing"|"neutral"
	SentimentDelta float64 `json:"sentiment_delta,omitempty"` // additive shift -1.0 to 1.0
}

// Scenario is a single simulation variant within a comparison set.
type Scenario struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`        // human-readable name, e.g. "Baseline"
	Seed        int64        `json:"seed"`
	TopicSuffix string       `json:"topic_suffix,omitempty"` // appended to the base topic
	Patches     []AgentPatch `json:"patches,omitempty"`
	TotalHours  int          `json:"total_hours"`
	Platform    string       `json:"platform"`
}

// ScenarioResult holds the computed metrics for one completed scenario.
type ScenarioResult struct {
	ScenarioID      string             `json:"scenario_id"`
	Label           string             `json:"label"`
	ActionCount     int                `json:"action_count"`
	PostCount       int                `json:"post_count"`
	AgentSentiments map[string]float64 `json:"agent_sentiments"` // agentName → final sentimentBias
	StanceBreakdown map[string]int     `json:"stance_breakdown"`
	PolarizationIdx float64            `json:"polarization_index"` // 0=consensus, 1=maximum polarization
	TopKeywords     []string           `json:"top_keywords"`
	AvgEngagement   float64            `json:"avg_engagement"` // avg likes+reposts per post
	CompletedAt     string             `json:"completed_at"`
}

// ComparisonReport aggregates results across all scenarios.
type ComparisonReport struct {
	ID         string           `json:"id"`
	ProjectID  string           `json:"project_id"`
	BaseTopic  string           `json:"base_topic"`
	Scenarios  []Scenario       `json:"scenarios"`
	Results    []ScenarioResult `json:"results"`
	Divergence DivergenceStats  `json:"divergence"`
	CreatedAt  string           `json:"created_at"`
}

// DivergenceStats summarises how differently scenarios evolved.
type DivergenceStats struct {
	MaxPolarizationDelta float64            `json:"max_polarization_delta"` // max - min polarization
	SentimentDivergence  map[string]float64 `json:"sentiment_divergence"`   // per-agent sentiment range
	MostDivergentAgent   string             `json:"most_divergent_agent"`
	ActionCountRange     [2]int             `json:"action_count_range"` // [min, max]
}

// ScenarioSet is the input to RunComparison.
type ScenarioSet struct {
	ProjectID  string     `json:"project_id"`
	BaseTopic  string     `json:"base_topic"`
	Scenarios  []Scenario `json:"scenarios"`
}

// ── Entry point ────────────────────────────────────────────────────────────

// RunComparison executes all scenarios in parallel (capped at 4) and returns
// a ComparisonReport with statistical divergence metrics. Does NOT call LLM.
func RunComparison(set ScenarioSet) (*ComparisonReport, error) {
	if len(set.Scenarios) < 2 {
		return nil, fmt.Errorf("need at least 2 scenarios to compare")
	}
	if len(set.Scenarios) > 4 {
		set.Scenarios = set.Scenarios[:4]
	}

	// Load base profiles once
	baseProfiles, err := agents.LoadProfiles(set.ProjectID)
	if err != nil || len(baseProfiles) == 0 {
		return nil, fmt.Errorf("no agents found — complete Step 2 first")
	}

	results := make([]ScenarioResult, len(set.Scenarios))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, sc := range set.Scenarios {
		wg.Add(1)
		go func(idx int, scenario Scenario) {
			defer wg.Done()
			res, err := runScenario(set.ProjectID, set.BaseTopic, scenario, baseProfiles)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
				return
			}
			if res != nil {
				results[idx] = *res
			}
		}(i, sc)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	divergence := computeDivergence(results)

	report := &ComparisonReport{
		ID:        uuid.NewString(),
		ProjectID: set.ProjectID,
		BaseTopic: set.BaseTopic,
		Scenarios: set.Scenarios,
		Results:   results,
		Divergence: divergence,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	// Persist
	b, _ := json.Marshal(report)
	_ = storage.DB.Insert("predictions", "comparison_"+report.ID, storage.Record{
		"id":         "comparison_" + report.ID,
		"project_id": set.ProjectID,
		"type":       "comparison",
		"data":       string(b),
	})

	return report, nil
}

// ── Internal scenario runner ───────────────────────────────────────────────

func runScenario(projectID, baseTopic string, sc Scenario, baseProfiles []*agents.OasisAgentProfile) (*ScenarioResult, error) {
	// Clone profiles and apply patches
	profiles := cloneProfiles(baseProfiles, sc.Patches)

	topic := baseTopic
	if sc.TopicSuffix != "" {
		topic = baseTopic + " — " + sc.TopicSuffix
	}

	totalHours := sc.TotalHours
	if totalHours <= 0 {
		totalHours = 12
	}

	// Run a lightweight in-memory simulation (no LLM, no storage)
	result := simulateLite(projectID, topic, sc.Seed, int64(totalHours), sc.Platform, profiles)
	result.ScenarioID = sc.ID
	result.Label = sc.Label

	return result, nil
}

// simulateLite runs a statistical simulation without LLM calls.
// Models: activity levels, sentiment drift, echo chamber dynamics.
func simulateLite(projectID, topic string, seed, totalHours int64, platform string, profiles []*agents.OasisAgentProfile) *ScenarioResult {
	rng := newRNG(seed)

	// Simple time-step model: hourly sentiment drift based on echo chamber
	stanceCount := make(map[string]int)
	for _, p := range profiles {
		stanceCount[p.Stance]++
	}

	sentiments := make(map[string]float64, len(profiles))
	for _, p := range profiles {
		sentiments[p.Name] = p.SentimentBias
	}

	actionCount := 0
	postCount := 0
	totalEngagement := 0.0

	hourMult := map[int]float64{
		0: 0.05, 1: 0.03, 2: 0.02, 3: 0.02, 4: 0.03, 5: 0.05,
		6: 0.15, 7: 0.35, 8: 0.60, 9: 0.80, 10: 0.90, 11: 0.95,
		12: 0.85, 13: 0.75, 14: 0.80, 15: 0.85, 16: 0.90, 17: 0.95,
		18: 1.00, 19: 1.50, 20: 1.40, 21: 1.20, 22: 0.80, 23: 0.40,
	}

	for hour := 0; hour < int(totalHours); hour++ {
		simHour := (8 + hour) % 24
		mult := hourMult[simHour]
		if mult < 0.1 {
			continue
		}

		for _, p := range profiles {
			if rng.Float64() > p.ActivityLevel*mult {
				continue
			}
			actionCount++
			// Post probability
			if rng.Float64() < 0.4 {
				postCount++
				likes := int(rng.Float64() * 10)
				reposts := int(rng.Float64() * 5)
				totalEngagement += float64(likes + reposts)
			}
			// Sentiment drift: pulled toward majority stance
			dominant := dominantStance(stanceCount)
			if p.Stance == dominant {
				sentiments[p.Name] = clampF(sentiments[p.Name]+rng.Float64()*0.02, -1.0, 1.0)
			} else {
				sentiments[p.Name] = clampF(sentiments[p.Name]-rng.Float64()*0.01, -1.0, 1.0)
			}
		}
	}

	avgEngagement := 0.0
	if postCount > 0 {
		avgEngagement = totalEngagement / float64(postCount)
	}

	return &ScenarioResult{
		ActionCount:     actionCount,
		PostCount:       postCount,
		AgentSentiments: sentiments,
		StanceBreakdown: stanceCount,
		PolarizationIdx: PolarizationIndex(sentiments),
		TopKeywords:     extractKeywords(topic),
		AvgEngagement:   roundF(avgEngagement, 2),
		CompletedAt:     time.Now().Format(time.RFC3339),
	}
}

// ── Polarization Index ─────────────────────────────────────────────────────

// PolarizationIndex computes the average pairwise sentiment distance across agents.
// Returns 0 (full consensus) to 1 (maximum polarization).
func PolarizationIndex(sentiments map[string]float64) float64 {
	vals := make([]float64, 0, len(sentiments))
	for _, v := range sentiments {
		vals = append(vals, v)
	}
	if len(vals) < 2 {
		return 0
	}
	totalDist := 0.0
	pairs := 0
	for i := 0; i < len(vals); i++ {
		for j := i + 1; j < len(vals); j++ {
			totalDist += math.Abs(vals[i] - vals[j])
			pairs++
		}
	}
	// Normalize: max possible pairwise distance is 2.0 (from -1 to +1)
	return roundF(totalDist/float64(pairs)/2.0, 4)
}

// ── Divergence stats ───────────────────────────────────────────────────────

func computeDivergence(results []ScenarioResult) DivergenceStats {
	if len(results) == 0 {
		return DivergenceStats{}
	}

	// Action count range
	minA, maxA := results[0].ActionCount, results[0].ActionCount
	for _, r := range results[1:] {
		if r.ActionCount < minA {
			minA = r.ActionCount
		}
		if r.ActionCount > maxA {
			maxA = r.ActionCount
		}
	}

	// Polarization delta
	minP, maxP := results[0].PolarizationIdx, results[0].PolarizationIdx
	for _, r := range results[1:] {
		if r.PolarizationIdx < minP {
			minP = r.PolarizationIdx
		}
		if r.PolarizationIdx > maxP {
			maxP = r.PolarizationIdx
		}
	}

	// Per-agent sentiment divergence (range across scenarios)
	allAgents := make(map[string][]float64)
	for _, r := range results {
		for name, s := range r.AgentSentiments {
			allAgents[name] = append(allAgents[name], s)
		}
	}
	sentDiv := make(map[string]float64, len(allAgents))
	mostDiv := ""
	maxDiv := 0.0
	for name, vals := range allAgents {
		minV, maxV := vals[0], vals[0]
		for _, v := range vals[1:] {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		d := roundF(maxV-minV, 4)
		sentDiv[name] = d
		if d > maxDiv {
			maxDiv = d
			mostDiv = name
		}
	}

	return DivergenceStats{
		MaxPolarizationDelta: roundF(maxP-minP, 4),
		SentimentDivergence:  sentDiv,
		MostDivergentAgent:   mostDiv,
		ActionCountRange:     [2]int{minA, maxA},
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func cloneProfiles(base []*agents.OasisAgentProfile, patches []AgentPatch) []*agents.OasisAgentProfile {
	out := make([]*agents.OasisAgentProfile, len(base))
	for i, p := range base {
		clone := *p
		out[i] = &clone
	}
	for _, patch := range patches {
		for _, p := range out {
			if p.Name == patch.AgentName {
				if patch.StanceOverride != "" {
					p.Stance = patch.StanceOverride
				}
				if patch.SentimentDelta != 0 {
					p.SentimentBias = clampF(p.SentimentBias+patch.SentimentDelta, -1.0, 1.0)
				}
			}
		}
	}
	return out
}

func dominantStance(stanceCount map[string]int) string {
	best, bestN := "neutral", 0
	for s, n := range stanceCount {
		if n > bestN {
			best, bestN = s, n
		}
	}
	return best
}

func extractKeywords(topic string) []string {
	// Very simple: split on spaces, return unique words > 3 chars, sorted
	seen := make(map[string]bool)
	var words []string
	start := 0
	for i, c := range topic + " " {
		if c == ' ' || c == ',' || c == '.' {
			w := topic[start:i]
			start = i + 1
			if len(w) > 3 && !seen[w] {
				seen[w] = true
				words = append(words, w)
			}
		}
	}
	sort.Strings(words)
	if len(words) > 8 {
		words = words[:8]
	}
	return words
}

func clampF(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func roundF(f float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(f*pow) / pow
}

// simple LCG-based RNG that doesn't require crypto/rand
type rng struct{ state int64 }

func newRNG(seed int64) *rng {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &rng{state: seed}
}

func (r *rng) Float64() float64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return math.Abs(float64(r.state)) / float64(math.MaxInt64)
}
