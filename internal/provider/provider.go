package provider

import (
	"context"
	"errors"
)

var (
	ErrInvalidURL  = errors.New("invalid media URL")
	ErrUnsupported = errors.New("provider unsupported")
)

type MediaRef struct {
	Provider        string
	ProviderMediaID string
	CanonicalURL    string
}

type TranscriptResult struct {
	Source string
	Text   string
}

type Provider interface {
	Name() string
	Match(rawURL string) bool
	Normalize(rawURL string) (MediaRef, error)
	FetchTranscript(ctx context.Context, ref MediaRef) (TranscriptResult, error)
}
