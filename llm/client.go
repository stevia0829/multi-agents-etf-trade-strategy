package llm

import "context"

// Message 通用对话消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatOptions 单次调用的可选参数
type ChatOptions struct {
	Temperature float64
	MaxTokens   int
}

// Client 是所有 LLM 提供方需要实现的统一接口。
// 后续接入其他厂商（豆包/通义/Kimi/OpenAI/Gemini ...）时，
// 只需新增一个实现该接口的 struct 即可，业务层不感知。
type Client interface {
	Name() string
	Chat(ctx context.Context, system, user string, opts ...ChatOptions) (string, error)
}
