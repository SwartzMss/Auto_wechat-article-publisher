package generator

import (
	"context"
	"time"
)

// Session 持有一次主题的多轮生成/修订上下文。
type Session struct {
	ID      string
	Spec    Spec
	Draft   Draft
	History []Turn
	agent   *Agent
}

// NewSession 创建 session，尚未生成稿件。
func NewSession(id string, spec Spec, agent *Agent) *Session {
	return &Session{
		ID:    id,
		Spec:  spec,
		agent: agent,
	}
}

// Propose 生成首稿。
func (s *Session) Propose(ctx context.Context) (Draft, error) {
	draft, err := s.agent.Generate(ctx, s.Spec, nil, s.History, "")
	if err != nil {
		return Draft{}, err
	}
	s.Draft = draft
	// 记录首稿，使用中文备注便于前端展示
	s.appendTurn("首稿", draft, "首稿")
	return draft, nil
}

// Revise 基于用户评论修订稿件。
func (s *Session) Revise(ctx context.Context, comment string) (Draft, error) {
	draft, err := s.agent.Generate(ctx, s.Spec, &s.Draft, s.History, comment)
	if err != nil {
		return Draft{}, err
	}
	s.Draft = draft
	s.appendTurn(comment, draft, "修订")
	return draft, nil
}

func (s *Session) appendTurn(comment string, draft Draft, summary string) {
	s.History = append(s.History, Turn{
		Comment:   comment,
		Draft:     draft,
		Summary:   summary,
		CreatedAt: time.Now(),
	})
}
