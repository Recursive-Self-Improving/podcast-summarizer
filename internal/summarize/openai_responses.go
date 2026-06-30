package summarize

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type OpenAIResponses struct {
	client openai.Client
	model  string
}

func NewOpenAIResponses(baseURL, apiKey, model string) OpenAIResponses {
	return OpenAIResponses{
		client: openai.NewClient(
			option.WithBaseURL(baseURL),
			option.WithAPIKey(apiKey),
		),
		model: model,
	}
}

func (s OpenAIResponses) Summarize(ctx context.Context, prompt, transcript string) (string, error) {
	input := "You are summarizing timestamped podcast/video transcripts.\n\nPrompt:\n" + prompt + "\n\nTranscript:\n" + transcript
	resp, err := s.client.Responses.New(ctx, responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		Model: s.model,
	})
	if err != nil {
		return "", fmt.Errorf("create OpenAI response: %w", err)
	}
	text := strings.TrimSpace(resp.OutputText())
	if text == "" {
		return "", fmt.Errorf("OpenAI response text is empty")
	}
	return text, nil
}
