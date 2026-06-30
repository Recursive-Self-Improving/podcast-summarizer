package provider

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	maxProviderFetchBytes = 10 << 20
	defaultHTTPTimeout    = 2 * time.Minute
)

type AudioRef struct {
	URL    string
	Direct bool
}

type AudioResolver interface {
	ResolveAudio(ctx context.Context, ref MediaRef) (AudioRef, error)
}

type HTTPAudioResolver struct {
	Client *http.Client
}

func SupportsSubtitleLookup(providerName string) bool {
	return providerName == YouTube{}.Name()
}

func (r HTTPAudioResolver) ResolveAudio(ctx context.Context, ref MediaRef) (AudioRef, error) {
	switch ref.Provider {
	case YouTube{}.Name():
		if err := ValidateCanonicalYouTubeURL(ref.CanonicalURL); err != nil {
			return AudioRef{}, err
		}
		return AudioRef{URL: ref.CanonicalURL}, nil
	case XiaoyuzhouFM{}.Name():
		return r.resolveXiaoyuzhou(ctx, ref)
	case SoundOnFM{}.Name():
		return r.resolveSoundOn(ctx, ref)
	default:
		return AudioRef{}, fmt.Errorf("%w: audio resolver for %s", ErrUnsupported, ref.Provider)
	}
}

func (r HTTPAudioResolver) resolveXiaoyuzhou(ctx context.Context, ref MediaRef) (AudioRef, error) {
	body, err := r.fetchText(ctx, ref.CanonicalURL)
	if err != nil {
		return AudioRef{}, fmt.Errorf("fetch xiaoyuzhou episode page: %w", err)
	}
	audioURL, err := extractXiaoyuzhouAudioURL(body)
	if err != nil {
		return AudioRef{}, err
	}
	return AudioRef{URL: audioURL, Direct: true}, nil
}

func (r HTTPAudioResolver) resolveSoundOn(ctx context.Context, ref MediaRef) (AudioRef, error) {
	podcastID, episodeID, err := parseSoundOnMediaID(ref.ProviderMediaID)
	if err != nil {
		return AudioRef{}, err
	}
	feedURL := "https://feeds.soundon.fm/podcasts/" + url.PathEscape(podcastID) + ".xml"
	body, err := r.fetchText(ctx, feedURL)
	if err != nil {
		return AudioRef{}, fmt.Errorf("fetch soundon feed: %w", err)
	}
	audioURL, err := extractSoundOnAudioURL(body, episodeID, ref.CanonicalURL)
	if err != nil {
		return AudioRef{}, err
	}
	return AudioRef{URL: audioURL, Direct: true}, nil
}

func (r HTTPAudioResolver) fetchText(ctx context.Context, rawURL string) (string, error) {
	return r.fetchTextWithUserAgent(ctx, rawURL, "podcast-summarizer/1.0")
}

func (r HTTPAudioResolver) fetchTextWithUserAgent(ctx context.Context, rawURL, userAgent string) (string, error) {
	if _, err := parseHTTPSURL(rawURL, "https URL"); err != nil {
		return "", err
	}
	client := r.httpClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderFetchBytes+1))
	if err != nil {
		return "", err
	}
	if len(contents) > maxProviderFetchBytes {
		return "", fmt.Errorf("response too large")
	}
	return string(contents), nil
}

var xiaoyuzhouAudioURLPattern = regexp.MustCompile(`https:(?:\\/\\/|//)(?:media\.xyzcdn\.net|dts-api\.xiaoyuzhoufm\.com)/[^"'<>\\\s]+?\.(?:m4a|mp3)(?:\?[^"'<>\\\s]*)?`)

func extractXiaoyuzhouAudioURL(body string) (string, error) {
	body = strings.ReplaceAll(body, `\/`, `/`)
	body = html.UnescapeString(body)
	match := xiaoyuzhouAudioURLPattern.FindString(body)
	if match == "" {
		return "", fmt.Errorf("xiaoyuzhou audio URL not found")
	}
	if err := validateDirectAudioURL(match); err != nil {
		return "", err
	}
	return match, nil
}

type soundOnRSS struct {
	Channel struct {
		Title string        `xml:"title"`
		Items []soundOnItem `xml:"item"`
	} `xml:"channel"`
}

type soundOnPodcastFeed struct {
	Title    string
	Episodes []EpisodeRef
}

type soundOnItem struct {
	Title       string `xml:"title"`
	GUID        string `xml:"guid"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	ITunesDur   string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	Description string `xml:"description"`
	Enclosure   struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
}

func extractSoundOnAudioURL(body, episodeID, canonicalURL string) (string, error) {
	var feed soundOnRSS
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		return "", fmt.Errorf("parse soundon feed: %w", err)
	}
	for _, item := range feed.Channel.Items {
		if strings.Contains(strings.TrimSpace(item.GUID), episodeID) {
			return soundOnItemAudioURL(item, episodeID)
		}
	}
	for _, item := range feed.Channel.Items {
		link := strings.TrimSpace(item.Link)
		if link == canonicalURL || strings.Contains(link, episodeID) {
			return soundOnItemAudioURL(item, episodeID)
		}
	}
	return "", fmt.Errorf("soundon episode %s not found in feed", episodeID)
}

func extractSoundOnPodcastFeed(body, podcastID string) (soundOnPodcastFeed, error) {
	var feed soundOnRSS
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		return soundOnPodcastFeed{}, fmt.Errorf("parse soundon feed: %w", err)
	}
	var episodes []EpisodeRef
	for _, item := range feed.Channel.Items {
		episodeID := soundOnEpisodeIDFromItem(item, podcastID)
		if episodeID == "" {
			continue
		}
		episodes = append(episodes, EpisodeRef{
			Provider:          SoundOnFM{}.Name(),
			ProviderEpisodeID: episodeID,
			ProviderMediaID:   podcastID + "/" + episodeID,
			CanonicalURL:      "https://player.soundon.fm/p/" + url.PathEscape(podcastID) + "/episodes/" + url.PathEscape(episodeID),
			Title:             strings.TrimSpace(item.Title),
			PubDate:           parseRSSDate(item.PubDate),
			DurationSeconds:   parseDurationSeconds(item.ITunesDur),
		})
	}
	sortEpisodesNewestFirst(episodes)
	return soundOnPodcastFeed{Title: strings.TrimSpace(feed.Channel.Title), Episodes: episodes}, nil
}

func extractSoundOnPodcastEpisodes(body, podcastID string) ([]EpisodeRef, error) {
	feed, err := extractSoundOnPodcastFeed(body, podcastID)
	return feed.Episodes, err
}

func soundOnEpisodeIDFromItem(item soundOnItem, podcastID string) string {
	if episodeID := soundOnEpisodeIDFromURL(strings.TrimSpace(item.Link), podcastID); episodeID != "" {
		return episodeID
	}
	guid := strings.TrimSpace(item.GUID)
	if episodeID := soundOnEpisodeIDFromURL(guid, podcastID); episodeID != "" {
		return episodeID
	}
	for _, match := range soundOnUUIDSearchPattern.FindAllString(strings.ToLower(guid), -1) {
		if match != podcastID {
			return match
		}
	}
	return ""
}

func soundOnEpisodeIDFromURL(rawURL, podcastID string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || strings.ToLower(parsed.Hostname()) != "player.soundon.fm" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "p" || parts[2] != "episodes" {
		return ""
	}
	parsedPodcastID, err1 := unescapePathSegment(parts[1], "SoundOn podcast ID")
	episodeID, err2 := unescapePathSegment(parts[3], "SoundOn episode ID")
	parsedPodcastID = strings.ToLower(parsedPodcastID)
	episodeID = strings.ToLower(episodeID)
	if err1 != nil || err2 != nil || parsedPodcastID != podcastID || !soundOnUUIDPattern.MatchString(episodeID) {
		return ""
	}
	return episodeID
}

func parseRSSDate(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseDurationSeconds(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return 0
	}
	var total int64
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0
		}
		var n int64
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return 0
			}
			n = n*10 + int64(ch-'0')
		}
		if len(parts) > 1 && i > 0 && n > 59 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func soundOnItemAudioURL(item soundOnItem, episodeID string) (string, error) {
	audioURL := strings.TrimSpace(item.Enclosure.URL)
	if audioURL == "" {
		return "", fmt.Errorf("soundon episode %s has no enclosure URL", episodeID)
	}
	if err := validateDirectAudioURL(audioURL); err != nil {
		return "", err
	}
	return audioURL, nil
}

func (r HTTPAudioResolver) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func validateDirectAudioURL(rawURL string) error {
	_, err := parseHTTPSURL(rawURL, "direct audio URL")
	return err
}

func parseHTTPSURL(rawURL, label string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid %s", label)
	}
	return parsed, nil
}
