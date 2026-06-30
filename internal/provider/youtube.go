package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type YouTube struct{}

func (YouTube) Name() string {
	return "youtube"
}

func (y YouTube) Match(rawURL string) bool {
	_, err := y.videoID(rawURL)
	return err == nil
}

func (y YouTube) Normalize(rawURL string) (MediaRef, error) {
	id, err := y.videoID(rawURL)
	if err != nil {
		return MediaRef{}, err
	}
	return MediaRef{
		Provider:        y.Name(),
		ProviderMediaID: id,
		CanonicalURL:    "https://www.youtube.com/watch?v=" + id,
	}, nil
}

func CanonicalizeYouTubeURL(rawURL string) (string, error) {
	ref, err := YouTube{}.Normalize(rawURL)
	if err != nil {
		return "", err
	}
	return ref.CanonicalURL, nil
}

func ValidateCanonicalYouTubeURL(canonicalURL string) error {
	normalizedURL, err := CanonicalizeYouTubeURL(canonicalURL)
	if err != nil {
		return err
	}
	if normalizedURL != canonicalURL {
		return fmt.Errorf("YouTube URL is not canonical")
	}
	return nil
}

func (YouTube) FetchTranscript(context.Context, MediaRef) (TranscriptResult, error) {
	return TranscriptResult{}, ErrUnsupported
}

func (YouTube) videoID(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid YouTube URL")
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimPrefix(parsed.EscapedPath(), "/")

	var id string
	switch host {
	case "www.youtube.com", "youtube.com", "m.youtube.com":
		if path == "watch" {
			id = parsed.Query().Get("v")
		} else if strings.HasPrefix(path, "shorts/") {
			id, err = unescapeYouTubePathID(strings.TrimPrefix(path, "shorts/"))
			if err != nil {
				return "", err
			}
		}
	case "youtu.be":
		id, err = unescapeYouTubePathID(path)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported YouTube host")
	}

	if !youtubeIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid YouTube video ID")
	}
	return id, nil
}

func unescapeYouTubePathID(path string) (string, error) {
	if path == "" || strings.Contains(path, "/") {
		return "", nil
	}
	id, err := url.PathUnescape(path)
	if err != nil {
		return "", fmt.Errorf("invalid YouTube video ID")
	}
	return id, nil
}
