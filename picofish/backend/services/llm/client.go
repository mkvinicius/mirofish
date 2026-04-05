// Package llm provides an OpenAI-compatible HTTP client supporting
// both chat completions and text embeddings.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"picofish/config"
)

// ── Types ─────────────────────────────────────────────────────────────────

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func System(c string) Message    { return Message{"system", c} }
func User(c string) Message      { return Message{"user", c} }
func Assistant(c string) Message { return Message{"assistant", c} }

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

type embedRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string for batch
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// ── Options ───────────────────────────────────────────────────────────────

type options struct {
	temperature float64
	maxTokens   int
}

type Option func(*options)

func defaults() *options { return &options{temperature: 0.7, maxTokens: 4096} }

func WithTemperature(t float64) Option { return func(o *options) { o.temperature = t } }
func WithMaxTokens(n int) Option       { return func(o *options) { o.maxTokens = n } }

// ── Embedding cache ───────────────────────────────────────────────────────

type embedCache struct {
	mu    sync.RWMutex
	store map[string][]float64
}

var cache = &embedCache{store: make(map[string][]float64)}

func (c *embedCache) get(key string) ([]float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *embedCache) set(key string, v []float64) {
	c.mu.Lock()
	c.store[key] = v
	c.mu.Unlock()
}

// ── HTTP client with exponential backoff retry ────────────────────────────

var httpClient = &http.Client{Timeout: 180 * time.Second}

func post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	delays := []time.Duration{0, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	var lastErr error

	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		data, err := doPost(ctx, path, b)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Don't retry client errors (4xx)
		if strings.Contains(err.Error(), "status 4") {
			return nil, err
		}
		if attempt < len(delays)-1 {
			fmt.Printf("[llm] retry %d after: %v\n", attempt+1, err)
		}
	}
	return nil, fmt.Errorf("llm: all retries failed: %w", lastErr)
}

func doPost(ctx context.Context, path string, b []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		config.Global.LLMBaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.Global.LLMAPIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	return data, nil
}

// ── Chat ──────────────────────────────────────────────────────────────────

func Chat(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	o := defaults()
	for _, fn := range opts {
		fn(o)
	}
	req := chatRequest{
		Model:       config.Global.LLMModel,
		Messages:    messages,
		Temperature: o.temperature,
		MaxTokens:   o.maxTokens,
	}
	data, err := post(ctx, "/chat/completions", req)
	if err != nil {
		return "", err
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("decode chat: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	return stripThinkTags(cr.Choices[0].Message.Content), nil
}

// ── Embeddings ────────────────────────────────────────────────────────────

// Embed returns a vector embedding for the given text (cached).
func Embed(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("embed: empty text")
	}
	key := cacheKey(text)
	if v, ok := cache.get(key); ok {
		return v, nil
	}
	req := embedRequest{Model: embedModel(), Input: text}
	data, err := post(ctx, "/embeddings", req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	var er embedResponse
	if err := json.Unmarshal(data, &er); err != nil {
		return nil, fmt.Errorf("decode embed: %w", err)
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	emb := er.Data[0].Embedding
	cache.set(key, emb)
	return emb, nil
}

// EmbedBatch returns embeddings for multiple texts in a single API call.
// Cached texts are returned immediately; uncached texts are batched (max 100/request).
func EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([][]float64, len(texts))

	var uncachedIdx []int
	var uncachedTexts []string
	for i, t := range texts {
		if t == "" {
			continue
		}
		if v, ok := cache.get(cacheKey(t)); ok {
			results[i] = v
		} else {
			uncachedIdx = append(uncachedIdx, i)
			uncachedTexts = append(uncachedTexts, t)
		}
	}
	if len(uncachedTexts) == 0 {
		return results, nil
	}

	const batchSize = 100
	for start := 0; start < len(uncachedTexts); start += batchSize {
		end := start + batchSize
		if end > len(uncachedTexts) {
			end = len(uncachedTexts)
		}
		batch := uncachedTexts[start:end]

		req := embedRequest{Model: embedModel(), Input: batch}
		data, err := post(ctx, "/embeddings", req)
		if err != nil {
			// Fallback: embed one by one
			for j, t := range batch {
				if emb, e2 := Embed(ctx, t); e2 == nil {
					results[uncachedIdx[start+j]] = emb
				}
			}
			continue
		}
		var er embedResponse
		if err := json.Unmarshal(data, &er); err != nil {
			continue
		}
		for _, d := range er.Data {
			if d.Index < len(batch) {
				idx := uncachedIdx[start+d.Index]
				results[idx] = d.Embedding
				cache.set(cacheKey(batch[d.Index]), d.Embedding)
			}
		}
	}
	return results, nil
}

func embedModel() string {
	if m := config.Global.EmbedModel; m != "" {
		return m
	}
	return config.Global.LLMModel
}

func cacheKey(text string) string {
	if len(text) > 200 {
		return text[:200]
	}
	return text
}

// ── Validation ────────────────────────────────────────────────────────────

// Validate pings the LLM API to confirm connectivity on startup.
func Validate(ctx context.Context) error {
	_, err := Chat(ctx, []Message{User("ping")}, WithMaxTokens(5), WithTemperature(0))
	if err != nil {
		return fmt.Errorf("LLM API unreachable (%s): %w", config.Global.LLMBaseURL, err)
	}
	return nil
}

// ── Vector math ───────────────────────────────────────────────────────────

func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// ── JSON extraction ───────────────────────────────────────────────────────

func ExtractJSON(s string) string {
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
	return strings.TrimSpace(s)
}

func ParseJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(ExtractJSON(s)), v)
}

func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s, "</think>")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+8:]
	}
	return strings.TrimSpace(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
