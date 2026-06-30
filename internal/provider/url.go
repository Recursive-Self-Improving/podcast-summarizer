package provider

import (
	"fmt"
	"net/url"
	"strings"
)

func parseHTTPSOrBareURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" && parsed.Host == "" {
		parsed, err = url.Parse("https://" + rawURL)
		if err != nil {
			return nil, err
		}
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL must use https")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("missing host")
	}
	return parsed, nil
}

func rejectEncodedPathSeparator(value string) error {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return fmt.Errorf("path segment contains encoded separator")
	}
	return nil
}

func unescapePathSegment(value, label string) (string, error) {
	if value == "" || strings.Contains(value, "/") {
		return "", fmt.Errorf("invalid %s", label)
	}
	if err := rejectEncodedPathSeparator(value); err != nil {
		return "", fmt.Errorf("invalid %s: %w", label, err)
	}
	unescaped, err := url.PathUnescape(value)
	if err != nil {
		return "", fmt.Errorf("invalid %s", label)
	}
	if unescaped == "" || strings.ContainsAny(unescaped, `/\\`) {
		return "", fmt.Errorf("invalid %s", label)
	}
	return unescaped, nil
}
