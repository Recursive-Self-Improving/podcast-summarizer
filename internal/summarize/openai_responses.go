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
	if variant, ok := SummaryVariantByPromptHash(PromptHash(prompt)); ok {
		return s.summarizeStructured(ctx, variant, prompt, transcript)
	}
	return s.summarizeLegacy(ctx, prompt, transcript)
}

func (s OpenAIResponses) summarizeLegacy(ctx context.Context, prompt, transcript string) (string, error) {
	input := openAISummaryInput(prompt, transcript)
	resp, err := s.client.Responses.New(ctx, responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		Model: s.model,
	})
	if err != nil {
		return "", fmt.Errorf("create OpenAI response: %w", err)
	}
	return openAIOutputText(resp.OutputText())
}

func (s OpenAIResponses) summarizeStructured(ctx context.Context, variant SummaryVariant, prompt, transcript string) (string, error) {
	input := openAISummaryInput(prompt, transcript) + "\n\nStructured output requirements:\nReturn only JSON matching the supplied schema. JSON property names are implementation details and must not affect the language of values. All content string values must be written in " + variant.Code + ". Do not include Markdown section headings inside field values."
	format := responses.ResponseFormatTextConfigParamOfJSONSchema("podcast_investment_summary", structuredSummarySchema(variant))
	format.OfJSONSchema.Strict = openai.Bool(true)
	format.OfJSONSchema.Description = openai.String("Structured podcast investment summary with fixed Chinese-language section fields")
	resp, err := s.client.Responses.New(ctx, responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		Model: s.model,
		Text:  responses.ResponseTextConfigParam{Format: format},
	})
	if err != nil {
		return "", fmt.Errorf("create OpenAI structured response: %w", err)
	}
	text, err := openAIOutputText(resp.OutputText())
	if err != nil {
		return "", err
	}
	summary, err := parseStructuredSummary(text)
	if err != nil {
		return "", fmt.Errorf("parse structured summary: %w", err)
	}
	markdown, err := renderStructuredSummaryMarkdown(summary, variant)
	if err != nil {
		return "", fmt.Errorf("validate structured summary: %w", err)
	}
	return markdown, nil
}

func openAISummaryInput(prompt, transcript string) string {
	return "You are summarizing timestamped podcast/video transcripts.\n\nPrompt:\n" + prompt + "\n\nTranscript:\n" + transcript
}

func openAIOutputText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("OpenAI response text is empty")
	}
	return text, nil
}
