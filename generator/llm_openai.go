package generator

import (
	"context"
	"errors"
	"os"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAILLM implements LLMClient using the official openai-go SDK (chat completions).
type OpenAILLM struct {
	Model string
	Opts  []option.RequestOption
}

func NewOpenAILLMFromConfig(cfg *LLMSettings) (*OpenAILLM, error) {
	if cfg == nil {
		return nil, errors.New("llm config is nil")
	}
	apiEnv := cfg.APIKeyEnv
	if apiEnv == "" {
		apiEnv = "OPENAI_API_KEY"
	}
	key := getenv(apiEnv)
	if key == "" {
		return nil, errors.New("openai api key missing")
	}
	if cfg.Model == "" {
		return nil, errors.New("llm model is required")
	}
	opts := []option.RequestOption{option.WithAPIKey(key)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &OpenAILLM{Model: cfg.Model, Opts: opts}, nil
}

func (o *OpenAILLM) Complete(ctx context.Context, prompt Prompt) (string, error) {
	client := openai.NewClient(o.Opts...)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(prompt.System),
	}
	for _, h := range prompt.History {
		role := h.Role
		if role == "" {
			role = "user"
		}
		switch role {
		case "assistant":
			msgs = append(msgs, openai.ChatCompletionMessageParamOfAssistant(h.Content))
		default:
			msgs = append(msgs, openai.UserMessage(h.Content))
		}
	}
	msgs = append(msgs, openai.UserMessage(prompt.User))

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(o.Model),
		Messages: msgs,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openai: empty choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func getenv(k string) string {
	v, _ := os.LookupEnv(k)
	return v
}
