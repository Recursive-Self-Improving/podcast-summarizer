package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	soundOnUUIDPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	soundOnUUIDSearchPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

type SoundOnFM struct{}

func (SoundOnFM) Name() string { return "soundon" }

func (s SoundOnFM) Match(rawURL string) bool {
	_, _, err := s.ids(rawURL)
	return err == nil
}

func (s SoundOnFM) Normalize(rawURL string) (MediaRef, error) {
	podcastID, episodeID, err := s.ids(rawURL)
	if err != nil {
		return MediaRef{}, err
	}
	return MediaRef{
		Provider:        s.Name(),
		ProviderMediaID: podcastID + "/" + episodeID,
		CanonicalURL:    "https://player.soundon.fm/p/" + url.PathEscape(podcastID) + "/episodes/" + url.PathEscape(episodeID),
	}, nil
}

func (SoundOnFM) FetchTranscript(context.Context, MediaRef) (TranscriptResult, error) {
	return TranscriptResult{}, ErrUnsupported
}

func (s SoundOnFM) MatchPodcast(rawURL string) bool {
	_, err := s.podcastID(rawURL)
	return err == nil
}

func (s SoundOnFM) NormalizePodcast(rawURL string) (PodcastRef, error) {
	podcastID, err := s.podcastID(rawURL)
	if err != nil {
		return PodcastRef{}, err
	}
	return PodcastRef{
		Provider:       s.Name(),
		ProviderFeedID: podcastID,
		CanonicalURL:   "https://player.soundon.fm/p/" + url.PathEscape(podcastID),
	}, nil
}

func (s SoundOnFM) FetchEpisodes(ctx context.Context, ref PodcastRef) (PodcastEpisodes, error) {
	if ref.Provider != s.Name() || !soundOnUUIDPattern.MatchString(ref.ProviderFeedID) {
		return PodcastEpisodes{}, fmt.Errorf("invalid SoundOn podcast ref")
	}
	feedURL := "https://feeds.soundon.fm/podcasts/" + url.PathEscape(ref.ProviderFeedID) + ".xml"
	body, err := (HTTPAudioResolver{}).fetchText(ctx, feedURL)
	if err != nil {
		return PodcastEpisodes{}, fmt.Errorf("fetch soundon feed: %w", err)
	}
	feed, err := extractSoundOnPodcastFeed(body, ref.ProviderFeedID)
	if err != nil {
		return PodcastEpisodes{}, err
	}
	ref.Title = firstNonEmpty(feed.Title, ref.Title)
	return PodcastEpisodes{Podcast: ref, Episodes: feed.Episodes}, nil
}

func (SoundOnFM) podcastID(rawURL string) (string, error) {
	parsed, err := parseHTTPSOrBareURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid SoundOn URL: %w", err)
	}
	if strings.ToLower(parsed.Hostname()) != "player.soundon.fm" {
		return "", fmt.Errorf("unsupported SoundOn host")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "p" {
		return "", fmt.Errorf("invalid SoundOn podcast path")
	}
	podcastID, err := unescapePathSegment(parts[1], "SoundOn podcast ID")
	if err != nil {
		return "", err
	}
	podcastID = strings.ToLower(podcastID)
	if !soundOnUUIDPattern.MatchString(podcastID) {
		return "", fmt.Errorf("invalid SoundOn UUID")
	}
	return podcastID, nil
}

func (SoundOnFM) ids(rawURL string) (string, string, error) {
	parsed, err := parseHTTPSOrBareURL(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid SoundOn URL: %w", err)
	}
	if strings.ToLower(parsed.Hostname()) != "player.soundon.fm" {
		return "", "", fmt.Errorf("unsupported SoundOn host")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "p" || parts[2] != "episodes" {
		return "", "", fmt.Errorf("invalid SoundOn episode path")
	}
	podcastID, err := unescapePathSegment(parts[1], "SoundOn podcast ID")
	if err != nil {
		return "", "", err
	}
	episodeID, err := unescapePathSegment(parts[3], "SoundOn episode ID")
	if err != nil {
		return "", "", err
	}
	podcastID = strings.ToLower(podcastID)
	episodeID = strings.ToLower(episodeID)
	if !soundOnUUIDPattern.MatchString(podcastID) || !soundOnUUIDPattern.MatchString(episodeID) {
		return "", "", fmt.Errorf("invalid SoundOn UUID")
	}
	return podcastID, episodeID, nil
}

func parseSoundOnMediaID(providerMediaID string) (string, string, error) {
	parts := strings.Split(providerMediaID, "/")
	if len(parts) != 2 || !soundOnUUIDPattern.MatchString(parts[0]) || !soundOnUUIDPattern.MatchString(parts[1]) {
		return "", "", fmt.Errorf("invalid SoundOn media ID")
	}
	return parts[0], parts[1], nil
}
