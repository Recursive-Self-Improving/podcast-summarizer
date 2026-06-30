package display

import "strings"

type SummaryMetadata struct {
	PodcastTitle string
	PodcastURL   string
	EpisodeTitle string
	PubDate      string
	Link         string
}

func (m SummaryMetadata) Empty() bool {
	return strings.TrimSpace(m.PodcastTitle) == "" &&
		strings.TrimSpace(m.EpisodeTitle) == "" &&
		strings.TrimSpace(m.PubDate) == "" &&
		strings.TrimSpace(m.Link) == ""
}

func MergeSummaryMetadata(primary, fallback SummaryMetadata) SummaryMetadata {
	if strings.TrimSpace(primary.PodcastTitle) == "" {
		primary.PodcastTitle = fallback.PodcastTitle
	}
	if strings.TrimSpace(primary.PodcastURL) == "" {
		primary.PodcastURL = fallback.PodcastURL
	}
	if strings.TrimSpace(primary.EpisodeTitle) == "" {
		primary.EpisodeTitle = fallback.EpisodeTitle
	}
	if strings.TrimSpace(primary.PubDate) == "" {
		primary.PubDate = fallback.PubDate
	}
	if strings.TrimSpace(primary.Link) == "" {
		primary.Link = fallback.Link
	}
	return primary
}
