package generator

import "context"

// LLMClient 抽象大模型客户端，便于替换/Mock。
type LLMClient interface {
	Complete(ctx context.Context, prompt Prompt) (string, error)
}

// LLMSettings 提供给具体实现的基础配置。
type LLMSettings struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}
