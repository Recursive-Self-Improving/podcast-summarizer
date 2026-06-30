package summarize

import (
	"context"
	"fmt"
	"testing"
)

func TestServiceSummarizesOneChunk(t *testing.T) {
	fake := &fakeSummarizer{}
	service := Service{Summarizer: fake, MaxChars: 100}
	result, err := service.SummarizeTranscript(context.Background(), "prompt", "[1.0s -> 2.0s] hello")
	if err != nil {
		t.Fatalf("SummarizeTranscript returned error: %v", err)
	}
	if result != "summary-1" || len(fake.calls) != 1 {
		t.Fatalf("result=%q calls=%#v", result, fake.calls)
	}
	if fake.calls[0].prompt != "prompt" || fake.calls[0].transcript != "[1.0s -> 2.0s] hello" {
		t.Fatalf("call = %#v", fake.calls[0])
	}
}

func TestServiceSummarizesMultipleChunksAndSynthesizes(t *testing.T) {
	t.Skip("chunked summarization is disabled for now")

	fake := &fakeSummarizer{}
	service := Service{Summarizer: fake, MaxChars: 25}
	transcript := "[1.0s -> 2.0s] first\n[2.0s -> 3.0s] second"
	result, err := service.SummarizeTranscript(context.Background(), "prompt", transcript)
	if err != nil {
		t.Fatalf("SummarizeTranscript returned error: %v", err)
	}
	if result != "summary-3" || len(fake.calls) != 3 {
		t.Fatalf("result=%q calls=%#v", result, fake.calls)
	}
	if fake.calls[2].transcript != "summary-1\n\nsummary-2" {
		t.Fatalf("synthesis transcript = %q", fake.calls[2].transcript)
	}
}

func TestServiceRecursiveSynthesis(t *testing.T) {
	t.Skip("chunked summarization is disabled for now")

	fake := &fakeSummarizer{responses: []string{"chunk one summary is long", "chunk two summary is long", "reduced one", "reduced two", "final"}}
	service := Service{Summarizer: fake, MaxChars: 25}
	transcript := "[1.0s -> 2.0s] first\n[2.0s -> 3.0s] second"
	result, err := service.SummarizeTranscript(context.Background(), "prompt", transcript)
	if err != nil {
		t.Fatalf("SummarizeTranscript returned error: %v", err)
	}
	if result != "final" || len(fake.calls) != 5 {
		t.Fatalf("result=%q calls=%#v", result, fake.calls)
	}
}

func TestServiceHardSplitsOversizedSummaryParagraphs(t *testing.T) {
	t.Skip("chunked summarization is disabled for now")

	fake := &fakeSummarizer{responses: []string{"123456789012345678901234567890", "abc", "short"}}
	service := Service{Summarizer: fake, MaxChars: 25}
	transcript := "[1.0s -> 2.0s] first\n[2.0s -> 3.0s] second"
	result, err := service.SummarizeTranscript(context.Background(), "prompt", transcript)
	if err != nil {
		t.Fatalf("SummarizeTranscript returned error: %v", err)
	}
	if result != "summary-5" {
		t.Fatalf("result = %q", result)
	}
	for i, call := range fake.calls[2:] {
		if len(call.transcript) > 25 {
			t.Fatalf("synthesis call %d exceeded max: %q", i+3, call.transcript)
		}
	}
}

func TestServiceUsesDefaultPrompt(t *testing.T) {
	fake := &fakeSummarizer{}
	service := Service{Summarizer: fake, MaxChars: 100}
	_, err := service.SummarizeTranscript(context.Background(), "", "[1.0s -> 2.0s] hello")
	if err != nil {
		t.Fatalf("SummarizeTranscript returned error: %v", err)
	}
	if fake.calls[0].prompt != DefaultPrompt {
		t.Fatalf("prompt = %q", fake.calls[0].prompt)
	}
}

type fakeSummarizer struct {
	calls     []summarizeCall
	responses []string
}

type summarizeCall struct {
	prompt     string
	transcript string
}

func (f *fakeSummarizer) Summarize(_ context.Context, prompt, transcript string) (string, error) {
	f.calls = append(f.calls, summarizeCall{prompt: prompt, transcript: transcript})
	if len(f.responses) > 0 {
		response := f.responses[0]
		f.responses = f.responses[1:]
		return response, nil
	}
	return fmt.Sprintf("summary-%d", len(f.calls)), nil
}
