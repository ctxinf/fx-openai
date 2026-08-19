// Package openai is the outbound Chat Completions client.
// It talks to Ollama, KLIA, vLLM, or any /v1-compatible server.
// It must not know Gateway request or SSE types.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"fx-openai/internal/version"
)

// Config is the upstream OpenAI-compatible endpoint.
type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// APIKeyFromEnv prefers OPENAI_API_KEY, then OLLAMA_API_KEY.
func APIKeyFromEnv(getenv func(string) string) string {
	if v := getenv("OPENAI_API_KEY"); v != "" {
		return v
	}
	return getenv("OLLAMA_API_KEY")
}

// Client calls {BaseURL}/models and {BaseURL}/chat/completions.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		BaseURL: strings.TrimRight(cfg.BaseURL, "/"),
		APIKey:  cfg.APIKey,
		HTTP:    httpClient,
	}
}

type ChatRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []Tool        `json:"tools,omitempty"`
	ToolChoice any           `json:"tool_choice,omitempty"`
	MaxTokens  *int          `json:"max_tokens,omitempty"`
	Stream     bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function ToolSpec `json:"function"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func (c *Client) Models(ctx context.Context) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, c.BaseURL+"/models", nil, false)
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, c.BaseURL+"/chat/completions", body, req.Stream)
}

func (c *Client) do(ctx context.Context, method, url string, body []byte, stream bool) (*http.Response, error) {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	httpReq.Header.Set("User-Agent", "fx-openai/"+version.String)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream %s %s: %w", method, url, err)
	}
	return resp, nil
}
