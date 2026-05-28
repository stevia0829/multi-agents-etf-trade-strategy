package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAICompatibleClient 兼容 OpenAI Chat Completions 协议的通用客户端。
// DeepSeek / Moonshot / 豆包 / 通义千问 / 自建网关 等都可以复用。
type OpenAICompatibleClient struct {
	provider string
	apiKey   string
	baseURL  string
	model    string
	timeout  time.Duration
	http     *http.Client
}

type OpenAIClientOption func(*OpenAICompatibleClient)

func WithTimeout(d time.Duration) OpenAIClientOption {
	return func(c *OpenAICompatibleClient) { c.timeout = d }
}

func NewOpenAICompatibleClient(provider, apiKey, baseURL, model string, opts ...OpenAIClientOption) *OpenAICompatibleClient {
	c := &OpenAICompatibleClient{
		provider: provider,
		apiKey:   apiKey,
		baseURL:  baseURL,
		model:    model,
		timeout:  60 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.http = &http.Client{Timeout: c.timeout}
	return c
}

func (c *OpenAICompatibleClient) Name() string {
	return c.provider + "/" + c.model
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAICompatibleClient) Chat(ctx context.Context, system, user string, opts ...ChatOptions) (string, error) {
	opt := ChatOptions{Temperature: 0.3}
	if len(opts) > 0 {
		opt = opts[0]
		if opt.Temperature == 0 {
			opt.Temperature = 0.3
		}
	}

	req := chatRequest{
		Model: c.model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: opt.Temperature,
		MaxTokens:   opt.MaxTokens,
		Stream:      false,
	}
	buf, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("[%s] upstream %d: %s", c.provider, resp.StatusCode, string(body))
	}
	if resp.StatusCode == 429 {
		return "", fmt.Errorf("[%s] rate limited: %s", c.provider, string(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("[%s] decode response: %w, raw: %s", c.provider, err, string(body))
	}
	if cr.Error != nil {
		return "", fmt.Errorf("[%s] error: %s", c.provider, cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("[%s] empty response: %s", c.provider, string(body))
	}
	return cr.Choices[0].Message.Content, nil
}
