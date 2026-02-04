package generator

import (
	"fmt"
	"strings"
)

// Prompt 表示发送给 LLM 的消息集合。
type Prompt struct {
	System  string
	User    string
	History []Message
}

// Message 用于少量历史（可选）。
type Message struct {
	Role    string
	Content string
}

// BuildInitialPrompt 生成首稿提示词。
func BuildInitialPrompt(spec Spec) Prompt {
	var sb strings.Builder
	sb.WriteString("你是一名专业中文内容创作者，请直接输出 Markdown，不要额外解释。\n")
	sb.WriteString("要求：\n")
	if spec.Words > 0 {
		sb.WriteString(fmt.Sprintf("- 目标字数约 %d 字（允许 ±15%%）。\n", spec.Words))
	}
	if spec.Tone != "" {
		sb.WriteString(fmt.Sprintf("- 语气：%s。\n", spec.Tone))
	}
	if spec.Audience != "" {
		sb.WriteString(fmt.Sprintf("- 受众：%s。\n", spec.Audience))
	}
	for _, c := range spec.Constraints {
		sb.WriteString(fmt.Sprintf("- %s\n", c))
	}
	sb.WriteString("- 必须包含一级标题作为文章标题。\n")
	sb.WriteString("- 开头给出 80~140 字的摘要（Digest），用段落呈现。\n")
	if len(spec.Outline) > 0 {
		sb.WriteString("- 按以下大纲组织内容：\n")
		for i, item := range spec.Outline {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, item))
		}
	}

	user := fmt.Sprintf("主题：%s\n请输出符合上述要求的完整 Markdown。", spec.Topic)

	return Prompt{
		System:  "严守 Markdown 结构，禁止输出额外说明。",
		User:    user,
		History: nil,
	}
}

// BuildRevisionPrompt 生成修订提示词。
func BuildRevisionPrompt(spec Spec, prev Draft, comment string, history []Turn) Prompt {
	var sb strings.Builder
	sb.WriteString("你是一名专业编辑，基于用户反馈对稿件做最小必要改动，保持 Markdown 结构。\n")
	sb.WriteString("- 维持标题层级和列表格式。\n")
	sb.WriteString("- 保持摘要位置（开头段落）。\n")
	sb.WriteString("- 如果反馈无效或不合理，说明原因并保持原文。\n")
	for _, c := range spec.Constraints {
		sb.WriteString(fmt.Sprintf("- %s\n", c))
	}

	user := fmt.Sprintf("当前稿件：\n%s\n\n用户反馈：%s\n请输出修订后的完整 Markdown。", prev.Markdown, comment)

	// 记录近期 turn 摘要（可空）。
	var msgs []Message
	for _, t := range history {
		if t.Comment == "" {
			continue
		}
		msgs = append(msgs, Message{Role: "user", Content: t.Comment})
	}

	return Prompt{
		System:  sb.String(),
		User:    user,
		History: msgs,
	}
}
