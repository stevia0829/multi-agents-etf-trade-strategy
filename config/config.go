package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/eino-multi-etf-strategy/llm"
)

type Config struct {
	LLM llm.MultiProviderConfig
}

// secretsFile 本地不入库的密钥文件结构。
// 路径默认 config/secrets.json，可通过环境变量 SECRETS_FILE 覆盖。
// 字段为空表示未配置，会回落到同名 OS 环境变量。
type secretsFile struct {
	DeepSeekAPIKey string `json:"deepseek_api_key"`
	MoonshotAPIKey string `json:"moonshot_api_key"`
	DoubaoAPIKey   string `json:"doubao_api_key"`
	QwenAPIKey     string `json:"qwen_api_key"`
}

// loadSecrets 读取 config/secrets.json；文件不存在 / 解析失败时返回空结构（不算错误）。
func loadSecrets() secretsFile {
	path := os.Getenv("SECRETS_FILE")
	if path == "" {
		// 默认相对项目根目录的 config/secrets.json
		path = filepath.Join("config", "secrets.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return secretsFile{}
	}
	var s secretsFile
	if err := json.Unmarshal(data, &s); err != nil {
		return secretsFile{}
	}
	return s
}

// pickKey 优先取 secrets 文件中的值，缺失则回落到 OS 环境变量。
func pickKey(fromFile, envName string) string {
	if fromFile != "" {
		return fromFile
	}
	return os.Getenv(envName)
}

func Load() *Config {
	sec := loadSecrets()
	dsKey := pickKey(sec.DeepSeekAPIKey, "DEEPSEEK_API_KEY")
	msKey := pickKey(sec.MoonshotAPIKey, "MOONSHOT_API_KEY")
	dbKey := pickKey(sec.DoubaoAPIKey, "DOUBAO_API_KEY")
	qwKey := pickKey(sec.QwenAPIKey, "QWEN_API_KEY")

	return &Config{
		LLM: llm.MultiProviderConfig{
			Primary: llm.ProviderConfig{
				Name:    getEnv("LLM_PRIMARY_NAME", "deepseek"),
				APIKey:  dsKey,
				BaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
				Model:   getEnv("DEEPSEEK_MODEL", "deepseek-chat"),
				Timeout: getEnvDuration("DEEPSEEK_TIMEOUT", 60*time.Second),
				Enabled: true,
			},
			Fallbacks: []llm.ProviderConfig{
				{
					Name:    "moonshot",
					APIKey:  msKey,
					BaseURL: getEnv("MOONSHOT_BASE_URL", "https://api.moonshot.cn/v1"),
					Model:   getEnv("MOONSHOT_MODEL", "moonshot-v1-8k"),
					Timeout: 60 * time.Second,
					Enabled: msKey != "",
				},
				{
					Name:    "doubao",
					APIKey:  dbKey,
					BaseURL: getEnv("DOUBAO_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"),
					Model:   getEnv("DOUBAO_MODEL", "doubao-1-5-pro-32k"),
					Timeout: 60 * time.Second,
					Enabled: dbKey != "",
				},
				{
					Name:    "qwen",
					APIKey:  qwKey,
					BaseURL: getEnv("QWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
					Model:   getEnv("QWEN_MODEL", "qwen-plus"),
					Timeout: 60 * time.Second,
					Enabled: qwKey != "",
				},
			},
			MaxRetries: getEnvInt("LLM_MAX_RETRIES", 2),
			BaseDelay:  getEnvDuration("LLM_BASE_DELAY", 500*time.Millisecond),
		},
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getEnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
