package summarize

import "context"

type Summarizer interface {
	Summarize(ctx context.Context, prompt, transcript string) (string, error)
}
