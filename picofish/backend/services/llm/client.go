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
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
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

// ── HTTP client ───────────────────────────────────────────────────────────

var httpClient = &http.Client{Timeout: 180 * time.Second}

func post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
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

// Chat sends messages and returns the assistant's reply.
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
	// Strip <think>...</think> reasoning blocks
	return stripThinkTags(cr.Choices[0].Message.Content), nil
}

// ── Embeddings ────────────────────────────────────────────────────────────

// Embed returns a vector embedding for the given text.
// Uses the configured embedding model (falls back to LLMModel if not set).
func Embed(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("embed: empty text")
	}
	model := config.Global.EmbedModel
	if model == "" {
		model = config.Global.LLMModel
	}
	req := embedRequest{
		Model: model,
		Input: text,
	}
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
	return er.Data[0].Embedding, nil
}

// ── Vector math ───────────────────────────────────────────────────────────

// CosineSimilarity returns the cosine similarity between two vectors [0,1].
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

// ExtractJSON strips markdown code fences and returns the JSON content.
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

// ParseJSON extracts and unmarshals JSON from an LLM response.
func ParseJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(ExtractJSON(s)), v)
}

// stripThinkTags removes <think>...</think> reasoning blocks from LLM output.
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
