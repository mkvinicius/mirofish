// memory.go implements temporal episodic memory and belief tracking per agent.
// Provides dynamic belief updates, contradiction detection, and semantic retrieval.
package agents

import (
	"context"
	"time"

	"picofish/services/llm"
)

// EpisodicEvent records a single witnessed event for an agent.
type EpisodicEvent struct {
	Content   string    `json:"content"`
	Topic     string    `json:"topic"`
	Embedding []float64 `json:"embedding,omitempty"`
	SimHour   int       `json:"sim_hour"`
	Round     int       `json:"round"`
	CreatedAt string    `json:"created_at"`
}

// BeliefEntry stores the strength of an agent's belief about a topic.
type BeliefEntry struct {
	Topic        string  `json:"topic"`
	Strength     float64 `json:"strength"`     // 0.0 to 1.0
	LastEvidence string  `json:"last_evidence"`
	LastUpdated  int     `json:"last_updated"` // simulated hour
}

// AgentMemoryManager provides temporal episodic memory and belief tracking.
// Individual agents get full episodic memory with belief decay.
// Group agents use a simplified version (capped at 10 episodes, no decay).
type AgentMemoryManager struct {
	AgentID   string                 `json:"agent_id"`
	ProjectID string                 `json:"project_id"`
	Episodes  []EpisodicEvent        `json:"episodes"`
	Beliefs   map[string]BeliefEntry `json:"beliefs"`
}

// NewAgentMemoryManager creates a fresh memory manager for an agent.
func NewAgentMemoryManager(agentID, projectID string) *AgentMemoryManager {
	return &AgentMemoryManager{
		AgentID:   agentID,
		ProjectID: projectID,
		Episodes:  []EpisodicEvent{},
		Beliefs:   make(map[string]BeliefEntry),
	}
}

// AddEpisode records a new witnessed event with a best-effort semantic embedding.
func (m *AgentMemoryManager) AddEpisode(ctx context.Context, content, topic string, round, simHour int) {
	event := EpisodicEvent{
		Content:   content,
		Topic:     topic,
		SimHour:   simHour,
		Round:     round,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if emb, err := llm.Embed(ctx, content); err == nil {
		event.Embedding = emb
	}
	m.Episodes = append(m.Episodes, event)
	// Individual agents keep up to 100 episodes; group agents capped externally to 10.
	if len(m.Episodes) > 100 {
		m.Episodes = m.Episodes[len(m.Episodes)-100:]
	}
}

// DecayBeliefs reduces belief strength by 0.05 per simulated hour of inactivity
// since the belief was last updated.
func (m *AgentMemoryManager) DecayBeliefs(currentHour int) {
	for topic, belief := range m.Beliefs {
		hoursSince := currentHour - belief.LastUpdated
		if hoursSince <= 0 {
			continue
		}
		belief.Strength -= 0.05 * float64(hoursSince)
		if belief.Strength < 0 {
			belief.Strength = 0
		}
		m.Beliefs[topic] = belief
	}
}

// UpdateBelief updates or creates a belief about a topic.
// If new evidence contradicts existing belief (cosine similarity < 0.3),
// triggers belief revision by averaging the two strengths.
func (m *AgentMemoryManager) UpdateBelief(ctx context.Context, topic, newEvidence string, strength float64, simHour int) {
	existing, exists := m.Beliefs[topic]
	if exists && existing.LastEvidence != "" {
		existingEmb, err1 := llm.Embed(ctx, existing.LastEvidence)
		newEmb, err2 := llm.Embed(ctx, newEvidence)
		if err1 == nil && err2 == nil {
			if llm.CosineSimilarity(existingEmb, newEmb) < 0.3 {
				// Contradiction detected — belief revision: average the strengths
				strength = (existing.Strength + strength) / 2.0
			}
		}
	}
	m.Beliefs[topic] = BeliefEntry{
		Topic:        topic,
		Strength:     clamp(strength, 0.0, 1.0),
		LastEvidence: newEvidence,
		LastUpdated:  simHour,
	}
}

// GetRelevantMemories returns topK most semantically similar past episodes.
// Falls back to most recent if embedding is unavailable.
func (m *AgentMemoryManager) GetRelevantMemories(ctx context.Context, query string, topK int) []EpisodicEvent {
	if len(m.Episodes) == 0 {
		return nil
	}
	queryEmb, err := llm.Embed(ctx, query)
	if err != nil || len(queryEmb) == 0 {
		// Fallback: return most recent
		n := topK
		if len(m.Episodes) < n {
			n = len(m.Episodes)
		}
		return m.Episodes[len(m.Episodes)-n:]
	}

	type candidate struct {
		event EpisodicEvent
		score float64
	}
	candidates := make([]candidate, 0, len(m.Episodes))
	for _, ep := range m.Episodes {
		s := 0.5 // default for entries without embeddings
		if len(ep.Embedding) > 0 {
			s = llm.CosineSimilarity(queryEmb, ep.Embedding)
		}
		candidates = append(candidates, candidate{ep, s})
	}
	// Insertion sort descending
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].score > candidates[j-1].score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	n := topK
	if len(candidates) < n {
		n = len(candidates)
	}
	result := make([]EpisodicEvent, n)
	for i := range result {
		result[i] = candidates[i].event
	}
	return result
}
