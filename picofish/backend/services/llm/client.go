package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"picofish/config"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

type StreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

// Chat sends messages and returns the full response text.
func Chat(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}

	req := ChatRequest{
		Model:       config.Global.LLMModel,
		Messages:    messages,
		Temperature: o.temperature,
		MaxTokens:   o.maxTokens,
		Stream:      false,
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		config.Global.LLMBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.Global.LLMAPIKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, string(b))
	}

	var cr ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("llm decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// ChatStream sends messages and streams delta tokens to the out channel.
func ChatStream(ctx context.Context, messages []Message, out chan<- string, opts ...Option) error {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}

	req := ChatRequest{
		Model:       config.Global.LLMModel,
		Messages:    messages,
		Temperature: o.temperature,
		MaxTokens:   o.maxTokens,
		Stream:      true,
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		config.Global.LLMBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.Global.LLMAPIKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm stream request: %w", err)
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)
	for {
		var line json.RawMessage
		if err := decoder.Decode(&line); err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		// SSE lines start with "data: "
		var chunk StreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				select {
				case out <- c.Delta.Content:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if c.FinishReason == "stop" {
				return nil
			}
		}
	}
	return nil
}

// System is a convenience builder for system messages.
func System(content string) Message {
	return Message{Role: "system", Content: content}
}

// User is a convenience builder for user messages.
func User(content string) Message {
	return Message{Role: "user", Content: content}
}

// Assistant is a convenience builder for assistant messages.
func Assistant(content string) Message {
	return Message{Role: "assistant", Content: content}
}

type options struct {
	temperature float64
	maxTokens   int
}

type Option func(*options)

func defaultOptions() *options {
	return &options{temperature: 0.7, maxTokens: 4096}
}

func WithTemperature(t float64) Option {
	return func(o *options) { o.temperature = t }
}

func WithMaxTokens(n int) Option {
	return func(o *options) { o.maxTokens = n }
}
