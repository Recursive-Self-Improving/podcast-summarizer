package queue

import (
	"context"
	"errors"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcribe"
)

type MediaRepository interface {
	GetMedia(ctx context.Context, mediaItemID int64) (db.MediaItem, error)
}

type AudioPipeline interface {
	Process(ctx context.Context, source transcribe.AudioSource, consume func(context.Context, transcribe.AudioInput) error) error
}

type Transcriber interface {
	Transcribe(ctx context.Context, input transcribe.AudioInput) (transcribe.Transcript, error)
}

type TranscriptionProcessor struct {
	Repo          MediaRepository
	Pipeline      AudioPipeline
	Transcriber   Transcriber
	AudioResolver provider.AudioResolver
}

func (p TranscriptionProcessor) Process(ctx context.Context, job db.TranscriptionJob) (Transcript, error) {
	if err := p.validate(); err != nil {
		return Transcript{}, err
	}
	media, err := p.Repo.GetMedia(ctx, job.MediaItemID)
	if err != nil {
		return Transcript{}, err
	}
	audioRef, err := p.audioResolver().ResolveAudio(ctx, mediaRefFromItem(media))
	if err != nil {
		return Transcript{}, err
	}
	var transcript transcribe.Transcript
	err = p.Pipeline.Process(ctx, transcribe.AudioSource{URL: audioRef.URL, Direct: audioRef.Direct}, func(ctx context.Context, input transcribe.AudioInput) error {
		var err error
		transcript, err = p.Transcriber.Transcribe(ctx, input)
		return err
	})
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{Source: transcript.Source, Text: transcript.Text}, nil
}

func (p TranscriptionProcessor) validate() error {
	if p.Repo == nil {
		return errors.New("media repository is required")
	}
	if p.Pipeline == nil {
		return errors.New("audio pipeline is required")
	}
	if p.Transcriber == nil {
		return errors.New("transcriber is required")
	}
	return nil
}

func (p TranscriptionProcessor) audioResolver() provider.AudioResolver {
	if p.AudioResolver != nil {
		return p.AudioResolver
	}
	return provider.HTTPAudioResolver{}
}

func mediaRefFromItem(media db.MediaItem) provider.MediaRef {
	return provider.MediaRef{
		Provider:        media.Provider,
		ProviderMediaID: media.ProviderMediaID,
		CanonicalURL:    media.CanonicalURL,
	}
}
