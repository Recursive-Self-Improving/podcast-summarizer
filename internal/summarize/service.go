package summarize

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	Summarizer Summarizer
	MaxChars   int
}

func (s Service) Summarize(ctx context.Context, prompt, transcript string) (string, error) {
	return s.SummarizeTranscript(ctx, prompt, transcript)
}

func (s Service) SummarizeTranscript(ctx context.Context, prompt, transcript string) (string, error) {
	if s.Summarizer == nil {
		return "", fmt.Errorf("summarizer is required")
	}
	prompt = ResolvePrompt(prompt)
	maxChars := s.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkChars
	}
	// chunks := ChunkTranscript(transcript, maxChars)
	// if len(chunks) == 0 {
	// 	return "", fmt.Errorf("transcript is empty")
	// }
	// if len(chunks) == 1 {
	// 	return s.Summarizer.Summarize(ctx, prompt, chunks[0])
	// }

	// summaries, err := s.summarizeChunks(ctx, prompt, chunks, "summarize")
	// if err != nil {
	// 	return "", err
	// }
	// return s.synthesize(ctx, prompt, summaries, maxChars, 0)

	return s.Summarizer.Summarize(ctx, prompt, transcript)
}

func (s Service) synthesize(ctx context.Context, prompt string, summaries []string, maxChars, depth int) (string, error) {
	if depth > 20 {
		return "", fmt.Errorf("synthesis did not converge")
	}
	combined := strings.Join(summaries, "\n\n")
	if len(combined) <= maxChars {
		return s.Summarizer.Summarize(ctx, prompt, combined)
	}
	chunks := ChunkText(combined, maxChars)
	if len(chunks) == 1 && len(chunks[0]) > maxChars {
		return "", fmt.Errorf("synthesis chunk exceeds maximum size")
	}
	next, err := s.summarizeChunks(ctx, prompt, chunks, "synthesize")
	if err != nil {
		return "", err
	}
	return s.synthesize(ctx, prompt, next, maxChars, depth+1)
}

func (s Service) summarizeChunks(ctx context.Context, prompt string, chunks []string, operation string) ([]string, error) {
	summaries := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		summary, err := s.Summarizer.Summarize(ctx, prompt, chunk)
		if err != nil {
			return nil, fmt.Errorf("%s chunk %d: %w", operation, i+1, err)
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}
