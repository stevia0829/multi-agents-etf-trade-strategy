package llm

import (
	"fmt"
	"time"
)

// ProviderConfig 单个 LLM 提供方的配置
type ProviderConfig struct {
	Name    string        // 标识名（如 deepseek / moonshot / doubao）
	APIKey  string        // 鉴权 key
	BaseURL string        // OpenAI 兼容端点
	Model   string        // 具体模型名
	Timeout time.Duration // 单次超时
	Enabled bool          // 是否启用
}

// MultiProviderConfig 多 provider 配置：第一个为主，其余为 fallback
type MultiProviderConfig struct {
	Primary    ProviderConfig
	Fallbacks  []ProviderConfig
	MaxRetries int
	BaseDelay  time.Duration
}

// Build 根据配置构建一个 Resilient Client。
// 任意 provider key 缺失时跳过，保证最少有 primary 可用。
// 如果 primary 不可用（无 key / baseURL / model），自动从 fallbacks 里挑第一个可用的提升为 primary，
// 让"用户只配了备模型 key"的场景也能跑通；全都没有时回到原行为返回 error。
func (m MultiProviderConfig) Build(static StaticFallback) (Client, error) {
	primaryCfg := m.Primary
	fallbacks := append([]ProviderConfig(nil), m.Fallbacks...)

	if primaryCfg.APIKey == "" || primaryCfg.BaseURL == "" || primaryCfg.Model == "" {
		// primary 不可用：尝试把第一个 enabled 的 fallback 提升为 primary
		promoted := -1
		for i, f := range fallbacks {
			if f.Enabled && f.APIKey != "" && f.BaseURL != "" && f.Model != "" {
				primaryCfg = f
				promoted = i
				break
			}
		}
		if promoted >= 0 {
			fallbacks = append(fallbacks[:promoted], fallbacks[promoted+1:]...)
		}
	}

	primary, err := buildOne(primaryCfg)
	if err != nil {
		return nil, fmt.Errorf("build primary llm (no available provider, please set at least one of *_API_KEY): %w", err)
	}

	fbs := make([]Client, 0, len(fallbacks))
	for _, f := range fallbacks {
		if !f.Enabled || f.APIKey == "" {
			continue
		}
		c, err := buildOne(f)
		if err != nil {
			continue
		}
		fbs = append(fbs, c)
	}

	opts := []ResilientOption{
		WithFallbacks(fbs...),
		WithMaxRetries(m.MaxRetries),
		WithBaseDelay(m.BaseDelay),
	}
	if static != nil {
		opts = append(opts, WithStaticFallback(static))
	}
	return NewResilient(primary, opts...), nil
}

func buildOne(p ProviderConfig) (Client, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("provider %s missing api key", p.Name)
	}
	if p.BaseURL == "" {
		return nil, fmt.Errorf("provider %s missing base url", p.Name)
	}
	if p.Model == "" {
		return nil, fmt.Errorf("provider %s missing model", p.Name)
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return NewOpenAICompatibleClient(p.Name, p.APIKey, p.BaseURL, p.Model, WithTimeout(timeout)), nil
}
