package generator

import "time"

// Spec describes the intended article before生成/修订。
type Spec struct {
	Topic       string
	Outline     []string
	Words       int
	Constraints []string
}

// Draft is the模型产出的稿件（Markdown 形式）。
type Draft struct {
	Title    string
	Digest   string
	Markdown string
	// 预留扩展字段（暂不处理图片）。
	CoverHint        string
	InlineImageHints []string
}

// Turn 记录一次评论驱动的修订。
type Turn struct {
	Comment   string
	Draft     Draft
	Summary   string
	CreatedAt time.Time
}
