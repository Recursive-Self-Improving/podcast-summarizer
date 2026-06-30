package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	xiaoyuzhouEpisodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	xiaoyuzhouNextDataPattern  = regexp.MustCompile(`(?is)<script\b[^>]*\bid\s*=\s*["']__next_data__["'][^>]*>(.*?)</script>`)
)

type XiaoyuzhouFM struct{}

func (XiaoyuzhouFM) Name() string { return "xiaoyuzhou" }

func (x XiaoyuzhouFM) Match(rawURL string) bool {
	_, err := x.episodeID(rawURL)
	return err == nil
}

func (x XiaoyuzhouFM) Normalize(rawURL string) (MediaRef, error) {
	episodeID, err := x.episodeID(rawURL)
	if err != nil {
		return MediaRef{}, err
	}
	return MediaRef{
		Provider:        x.Name(),
		ProviderMediaID: episodeID,
		CanonicalURL:    "https://www.xiaoyuzhoufm.com/episode/" + url.PathEscape(episodeID),
	}, nil
}

func (XiaoyuzhouFM) FetchTranscript(context.Context, MediaRef) (TranscriptResult, error) {
	return TranscriptResult{}, ErrUnsupported
}

func (x XiaoyuzhouFM) MatchPodcast(rawURL string) bool {
	_, err := x.podcastID(rawURL)
	return err == nil
}

func (x XiaoyuzhouFM) NormalizePodcast(rawURL string) (PodcastRef, error) {
	podcastID, err := x.podcastID(rawURL)
	if err != nil {
		return PodcastRef{}, err
	}
	return PodcastRef{
		Provider:       x.Name(),
		ProviderFeedID: podcastID,
		CanonicalURL:   "https://www.xiaoyuzhoufm.com/podcast/" + url.PathEscape(podcastID),
	}, nil
}

func (x XiaoyuzhouFM) FetchEpisodes(ctx context.Context, ref PodcastRef) (PodcastEpisodes, error) {
	if ref.Provider != x.Name() || !xiaoyuzhouEpisodeIDPattern.MatchString(ref.ProviderFeedID) {
		return PodcastEpisodes{}, fmt.Errorf("invalid Xiaoyuzhou podcast ref")
	}
	fetchURL := "https://www.xiaoyuzhoufm.com/podcast/" + url.PathEscape(ref.ProviderFeedID)
	body, err := (HTTPAudioResolver{}).fetchTextWithUserAgent(ctx, fetchURL, "Mozilla/5.0 (compatible; podcast-summarizer/1.0)")
	if err != nil {
		return PodcastEpisodes{}, fmt.Errorf("fetch xiaoyuzhou podcast page: %w", err)
	}
	episodes, title, err := extractXiaoyuzhouPodcastEpisodes(body)
	if err != nil {
		return PodcastEpisodes{}, err
	}
	for i := range episodes {
		episodes[i].Provider = x.Name()
	}
	ref.Title = firstNonEmpty(strings.TrimSpace(title), ref.Title)
	return PodcastEpisodes{Podcast: ref, Episodes: episodes}, nil
}

func (XiaoyuzhouFM) podcastID(rawURL string) (string, error) {
	parsed, err := parseHTTPSOrBareURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid Xiaoyuzhou URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "xiaoyuzhoufm.com" && host != "www.xiaoyuzhoufm.com" {
		return "", fmt.Errorf("unsupported Xiaoyuzhou host")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "podcast" {
		return "", fmt.Errorf("invalid Xiaoyuzhou podcast path")
	}
	podcastID, err := unescapePathSegment(parts[1], "Xiaoyuzhou podcast ID")
	if err != nil {
		return "", err
	}
	if !xiaoyuzhouEpisodeIDPattern.MatchString(podcastID) {
		return "", fmt.Errorf("invalid Xiaoyuzhou podcast ID")
	}
	return podcastID, nil
}

type xiaoyuzhouNextData struct {
	Props struct {
		PageProps struct {
			Podcast struct {
				Title    string              `json:"title"`
				Episodes []xiaoyuzhouEpisode `json:"episodes"`
			} `json:"podcast"`
		} `json:"pageProps"`
	} `json:"props"`
}

type xiaoyuzhouEpisode struct {
	EID      string `json:"eid"`
	Title    string `json:"title"`
	PubDate  any    `json:"pubDate"`
	Duration any    `json:"duration"`
}

func extractXiaoyuzhouPodcastEpisodes(body string) ([]EpisodeRef, string, error) {
	data, err := extractNextDataJSON(body)
	if err != nil {
		return nil, "", err
	}
	var next xiaoyuzhouNextData
	if err := json.Unmarshal([]byte(data), &next); err != nil {
		return nil, "", fmt.Errorf("parse xiaoyuzhou podcast data: %w", err)
	}
	var episodes []EpisodeRef
	for _, item := range next.Props.PageProps.Podcast.Episodes {
		episodeID := strings.TrimSpace(item.EID)
		if !xiaoyuzhouEpisodeIDPattern.MatchString(episodeID) {
			continue
		}
		episodes = append(episodes, EpisodeRef{
			Provider:          XiaoyuzhouFM{}.Name(),
			ProviderEpisodeID: episodeID,
			ProviderMediaID:   episodeID,
			CanonicalURL:      "https://www.xiaoyuzhoufm.com/episode/" + url.PathEscape(episodeID),
			Title:             strings.TrimSpace(item.Title),
			PubDate:           parseXiaoyuzhouTime(item.PubDate),
			DurationSeconds:   parseXiaoyuzhouInt(item.Duration),
		})
	}
	if len(episodes) == 0 {
		return nil, next.Props.PageProps.Podcast.Title, fmt.Errorf("xiaoyuzhou podcast episodes not found")
	}
	sortEpisodesNewestFirst(episodes)
	return episodes, strings.TrimSpace(next.Props.PageProps.Podcast.Title), nil
}

func extractNextDataJSON(body string) (string, error) {
	matches := xiaoyuzhouNextDataPattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		return "", fmt.Errorf("xiaoyuzhou __NEXT_DATA__ not found")
	}
	return html.UnescapeString(matches[1]), nil
}

func parseXiaoyuzhouTime(value any) time.Time {
	switch v := value.(type) {
	case float64:
		return time.Unix(int64(v), 0)
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.DateTime, v); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseXiaoyuzhouInt(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case string:
		return parseDurationSeconds(v)
	}
	return 0
}

func (XiaoyuzhouFM) episodeID(rawURL string) (string, error) {
	parsed, err := parseHTTPSOrBareURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid Xiaoyuzhou URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "xiaoyuzhoufm.com" && host != "www.xiaoyuzhoufm.com" {
		return "", fmt.Errorf("unsupported Xiaoyuzhou host")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "episode" {
		return "", fmt.Errorf("invalid Xiaoyuzhou episode path")
	}
	episodeID, err := unescapePathSegment(parts[1], "Xiaoyuzhou episode ID")
	if err != nil {
		return "", err
	}
	if !xiaoyuzhouEpisodeIDPattern.MatchString(episodeID) {
		return "", fmt.Errorf("invalid Xiaoyuzhou episode ID")
	}
	return episodeID, nil
}
