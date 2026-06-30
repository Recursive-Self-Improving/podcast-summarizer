package provider

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type PodcastRef struct {
	Provider       string
	ProviderFeedID string
	CanonicalURL   string
	Title          string
}

type EpisodeRef struct {
	Provider          string
	ProviderEpisodeID string
	ProviderMediaID   string
	CanonicalURL      string
	Title             string
	PubDate           time.Time
	DurationSeconds   int64
}

type PodcastEpisodes struct {
	Podcast  PodcastRef
	Episodes []EpisodeRef
}

type PodcastProvider interface {
	Name() string
	MatchPodcast(rawURL string) bool
	NormalizePodcast(rawURL string) (PodcastRef, error)
	FetchEpisodes(ctx context.Context, ref PodcastRef) (PodcastEpisodes, error)
}

type PodcastRegistry struct {
	providers []PodcastProvider
}

func NewPodcastRegistry(providers ...PodcastProvider) PodcastRegistry {
	return PodcastRegistry{providers: providers}
}

func DefaultPodcastRegistry() PodcastRegistry {
	return NewPodcastRegistry(SoundOnFM{}, XiaoyuzhouFM{})
}

func (r PodcastRegistry) Find(rawURL string) (PodcastProvider, PodcastRef, error) {
	for _, provider := range r.providers {
		if provider.MatchPodcast(rawURL) {
			ref, err := provider.NormalizePodcast(rawURL)
			if err != nil {
				return nil, PodcastRef{}, fmt.Errorf("%w: %w", ErrInvalidURL, err)
			}
			return provider, ref, nil
		}
	}
	return nil, PodcastRef{}, fmt.Errorf("%w: no podcast provider matched URL", ErrInvalidURL)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sortEpisodesNewestFirst(episodes []EpisodeRef) {
	sort.SliceStable(episodes, func(i, j int) bool {
		left := episodes[i].PubDate
		right := episodes[j].PubDate
		if left.IsZero() && right.IsZero() {
			return false
		}
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})
}
