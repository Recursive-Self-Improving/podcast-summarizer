package queue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcribe"
)

func TestTranscriptionProcessorRunsPipelineAndTranscriber(t *testing.T) {
	repo := &processorRepo{media: db.MediaItem{ID: 1, Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"}}
	pipeline := &fakeAudioPipeline{input: transcribe.AudioInput{WorkDir: t.TempDir(), WAVPath: "audio.wav"}}
	resolver := &fakeAudioResolver{audio: provider.AudioRef{URL: repo.media.CanonicalURL}}
	transcriber := &fakeTranscriber{transcript: transcribe.Transcript{Source: transcribe.WhisperSource, Text: "transcript"}}
	processor := TranscriptionProcessor{Repo: repo, Pipeline: pipeline, Transcriber: transcriber, AudioResolver: resolver}

	transcript, err := processor.Process(context.Background(), db.TranscriptionJob{MediaItemID: repo.media.ID})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if transcript.Source != transcribe.WhisperSource || transcript.Text != "transcript" {
		t.Fatalf("transcript = %#v", transcript)
	}
	if resolver.ref.Provider != repo.media.Provider || resolver.ref.ProviderMediaID != repo.media.ProviderMediaID || resolver.ref.CanonicalURL != repo.media.CanonicalURL {
		t.Fatalf("resolver ref = %#v", resolver.ref)
	}
	if pipeline.source.URL != repo.media.CanonicalURL || pipeline.source.Direct {
		t.Fatalf("audio source = %#v", pipeline.source)
	}
	if transcriber.input.WAVPath != pipeline.input.WAVPath {
		t.Fatalf("transcriber input = %#v", transcriber.input)
	}
}

func TestTranscriptionProcessorReturnsPipelineError(t *testing.T) {
	processor := TranscriptionProcessor{
		Repo:          &processorRepo{media: db.MediaItem{ID: 1, Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"}},
		Pipeline:      &fakeAudioPipeline{err: errors.New("ffmpeg failed")},
		Transcriber:   &fakeTranscriber{},
		AudioResolver: &fakeAudioResolver{audio: provider.AudioRef{URL: "https://www.youtube.com/watch?v=abc12345678"}},
	}

	_, err := processor.Process(context.Background(), db.TranscriptionJob{MediaItemID: 1})
	if err == nil || !strings.Contains(err.Error(), "ffmpeg failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestTranscriptionProcessorRequiresDependencies(t *testing.T) {
	_, err := (TranscriptionProcessor{}).Process(context.Background(), db.TranscriptionJob{})
	if err == nil || !strings.Contains(err.Error(), "media repository is required") {
		t.Fatalf("error = %v", err)
	}
}

type processorRepo struct {
	media db.MediaItem
	err   error
}

func (r *processorRepo) GetMedia(_ context.Context, mediaItemID int64) (db.MediaItem, error) {
	if r.err != nil {
		return db.MediaItem{}, r.err
	}
	return r.media, nil
}

type fakeAudioPipeline struct {
	input  transcribe.AudioInput
	err    error
	source transcribe.AudioSource
}

func (p *fakeAudioPipeline) Process(ctx context.Context, source transcribe.AudioSource, consume func(context.Context, transcribe.AudioInput) error) error {
	p.source = source
	if p.err != nil {
		return p.err
	}
	return consume(ctx, p.input)
}

type fakeAudioResolver struct {
	audio provider.AudioRef
	err   error
	ref   provider.MediaRef
}

func (r *fakeAudioResolver) ResolveAudio(_ context.Context, ref provider.MediaRef) (provider.AudioRef, error) {
	r.ref = ref
	if r.err != nil {
		return provider.AudioRef{}, r.err
	}
	return r.audio, nil
}

type fakeTranscriber struct {
	transcript transcribe.Transcript
	err        error
	input      transcribe.AudioInput
}

func (t *fakeTranscriber) Transcribe(_ context.Context, input transcribe.AudioInput) (transcribe.Transcript, error) {
	t.input = input
	if t.err != nil {
		return transcribe.Transcript{}, t.err
	}
	return t.transcript, nil
}
