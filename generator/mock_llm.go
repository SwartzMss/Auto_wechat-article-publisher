package generator

import (
	"context"
	"strings"
)

// MockLLM 一个简单的占位实现，便于本地调试，不调用外部模型。
type MockLLM struct{}

func (m MockLLM) Complete(_ context.Context, prompt Prompt) (string, error) {
	// 很简单地把用户输入/大纲拼接成 Markdown。
	var sb strings.Builder
	sb.WriteString("# 自动生成示例标题\n\n")
	sb.WriteString("这里是一段自动生成的摘要，概述全文要点。\n\n")
	sb.WriteString("## 正文\n\n")
	sb.WriteString("根据提示生成的内容：\n\n")
	sb.WriteString("```\n")
	sb.WriteString(prompt.User)
	sb.WriteString("\n```\n")
	return sb.String(), nil
}
