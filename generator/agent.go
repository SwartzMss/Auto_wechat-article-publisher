package generator

import (
	"context"
	"errors"
)

// Agent 负责根据 Spec 和历史/反馈生成或修订稿件。
type Agent struct {
	llm LLMClient
}

func NewAgent(llm LLMClient) (*Agent, error) {
	if llm == nil {
		return nil, errors.New("llm client is required")
	}
	return &Agent{llm: llm}, nil
}

// Generate 根据是否存在 prevDraft 决定首稿或修订流程。
func (a *Agent) Generate(ctx context.Context, spec Spec, prevDraft *Draft, history []Turn, comment string) (Draft, error) {
	var prompt Prompt
	if prevDraft == nil {
		prompt = BuildInitialPrompt(spec)
	} else {
		prompt = BuildRevisionPrompt(spec, *prevDraft, comment, history)
	}

	raw, err := a.llm.Complete(ctx, prompt)
	if err != nil {
		return Draft{}, err
	}
	return PostProcess(raw, spec)
}
