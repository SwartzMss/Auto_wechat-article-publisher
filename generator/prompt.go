package generator

import (
	"fmt"
	"log"
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

// 预设的风格提示词，按 key 选择。
var stylePresets = map[string]string{
	"life-rational": `你是一名内容写作者，面向没有专业背景的普通读者。

写作要求：
- 风格：生活化、理性、克制
- 语气：冷静、解释型，不煽动情绪
- 不使用营销号语言（如“震惊”“你一定不知道”）
- 不居高临下，不对读者进行道德评判
- 用日常生活场景引出问题
- 用简单的科学模型或研究结论进行解释
- 避免过多专业术语，如必须出现请顺带解释

文章结构建议：
1. 一个真实生活场景或普遍困惑
2. 人们常见的直觉理解
3. 科学上的解释或研究发现
4. 一个温和、开放的收束结论

目标：
让读者读完后觉得“原来是这样”，而不是“我被教育了”。`,
	"warm-healing": `你是一名温和的内容写作者，擅长用科学解释人的情绪和行为。

写作要求：
- 风格：温和、治愈、有同理心
- 语气：像一个理解人的朋友，而不是专家或老师
- 允许情绪表达，但不过度煽情
- 不指责、不批评、不下“你应该”的结论
- 科学内容作为解释工具，而不是说服工具

文章结构建议：
1. 描述一种常见的情绪或困扰
2. 明确告诉读者：这种状态并不罕见
3. 用心理学或行为科学解释为什么会这样
4. 给出一个宽松、非强制的理解视角

目标：
让读者读完后感觉“被理解”，而不是“被分析”。`,
	"novelistic": `你是一名内容写作者，使用“轻小说式叙事”来解释现象或原理。

写作方式：
- 以一个非常日常的生活场景开头
- 使用第三人称或模糊第一人称
- 场景真实、克制，不追求戏剧冲突
- 不写完整故事，只写一个生活切片
- 人物不需要名字和详细背景

解释要求：
- 小说只是引子，核心目的是解释原理
- 在中段自然引入心理学 / 认知科学解释
- 避免学术语言，用生活化比喻说明机制
- 不下结论式判断，不进行价值说教

文章目标：
让读者在“读故事”的过程中，理解一个科学概念。`,
}

// BuildInitialPrompt 生成首稿提示词。
func BuildInitialPrompt(spec Spec) Prompt {
	var sb strings.Builder
	sb.WriteString("你是一名专业中文内容创作者，请直接输出 Markdown，不要额外解释。\n")
	sb.WriteString("要求：\n")
	if spec.Words > 0 {
		sb.WriteString(fmt.Sprintf("- 目标字数约 %d 字（允许 ±15%%）。\n", spec.Words))
	}
	styleKey := spec.Style
	if styleKey == "" {
		styleKey = "life-rational"
	}
	stylePrompt := strings.TrimSpace(stylePresets[styleKey])
	if stylePrompt != "" {
		sb.WriteString("风格预设：\n")
		sb.WriteString(stylePrompt)
		sb.WriteString("\n")
	}

	if len(spec.Constraints) > 0 {
		sb.WriteString("额外约束：\n")
		for _, c := range spec.Constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	sb.WriteString("- 必须包含一级标题作为文章标题。\n")
	if len(spec.Outline) > 0 {
		sb.WriteString("- 结合以下背景信息进行写作：\n")
		for i, item := range spec.Outline {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, item))
		}
	}
	sb.WriteString("请严格遵守以上要求和 Markdown 结构，禁止额外说明。\n")

	user := fmt.Sprintf("主题：%s\n请输出符合上述要求的完整 Markdown。", spec.Topic)
	system := sb.String()
	log.Printf("[Prompt][initial] style=%s constraints=%d\nsystem:\n%s\nuser:\n%s\n", styleKey, len(spec.Constraints), system, user)

	return Prompt{
		System:  system,
		User:    user,
		History: nil,
	}
}

// BuildRevisionPrompt 生成修订提示词。
func BuildRevisionPrompt(spec Spec, prev Draft, comment string, history []Turn) Prompt {
	var sb strings.Builder
	sb.WriteString("你是一名专业编辑，基于用户反馈对稿件做最小必要改动，保持 Markdown 结构。\n")
	sb.WriteString("- 维持标题层级和列表格式。\n")
	sb.WriteString("- 如果反馈无效或不合理，说明原因并保持原文。\n")
	styleKey := spec.Style
	if styleKey == "" {
		styleKey = "life-rational"
	}
	stylePrompt := strings.TrimSpace(stylePresets[styleKey])
	if stylePrompt != "" {
		sb.WriteString("风格预设：\n")
		sb.WriteString(stylePrompt)
		sb.WriteString("\n")
	}
	if len(spec.Constraints) > 0 {
		sb.WriteString("额外约束：\n")
		for _, c := range spec.Constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
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
