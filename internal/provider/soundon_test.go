package provider

import (
	"testing"
	"time"
)

const (
	soundOnPodcastID = "954689a5-3096-43a4-a80b-7810b219cef3"
	soundOnEpisodeID = "181f554e-900e-4261-bc3c-1a3fe53a902a"
)

func TestSoundOnNormalizeAcceptedURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
	} {
		t.Run(rawURL, func(t *testing.T) {
			ref, err := SoundOnFM{}.Normalize(rawURL)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if ref.Provider != "soundon" || ref.ProviderMediaID != soundOnPodcastID+"/"+soundOnEpisodeID {
				t.Fatalf("ref = %#v", ref)
			}
			if ref.CanonicalURL != "https://player.soundon.fm/p/"+soundOnPodcastID+"/episodes/"+soundOnEpisodeID {
				t.Fatalf("canonical URL = %q", ref.CanonicalURL)
			}
		})
	}
}

func TestSoundOnNormalizeRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"https://example.com/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"https://player.soundon.fm/p/" + soundOnPodcastID,
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/",
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID + "/extra",
		"https://player.soundon.fm/p/%2F" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"https://player.soundon.fm/p/not-a-uuid/episodes/" + soundOnEpisodeID,
		"not a url",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := (SoundOnFM{}).Normalize(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSoundOnPodcastNormalizeAcceptedURL(t *testing.T) {
	ref, err := SoundOnFM{}.NormalizePodcast("https://player.soundon.fm/p/" + soundOnPodcastID)
	if err != nil {
		t.Fatalf("NormalizePodcast returned error: %v", err)
	}
	if ref.Provider != "soundon" || ref.ProviderFeedID != soundOnPodcastID || ref.CanonicalURL != "https://player.soundon.fm/p/"+soundOnPodcastID {
		t.Fatalf("ref = %#v", ref)
	}
}

func TestSoundOnPodcastNormalizeRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://player.soundon.fm/p/" + soundOnPodcastID,
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"https://example.com/p/" + soundOnPodcastID,
		"https://player.soundon.fm/p/%2F" + soundOnPodcastID,
		"https://player.soundon.fm/p/not-a-uuid",
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/extra",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := (SoundOnFM{}).NormalizePodcast(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExtractSoundOnPodcastEpisodes(t *testing.T) {
	olderEpisodeID := "281f554e-900e-4261-bc3c-1a3fe53a902a"
	feed := `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
		<item><title>Older</title><guid>` + olderEpisodeID + `</guid><link>https://example.com</link><pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate><itunes:duration>01:02:03</itunes:duration><enclosure url="https://cdn.example.com/older.mp3" /></item>
		<item><title>Newest</title><guid>ignored</guid><link>https://player.soundon.fm/p/` + soundOnPodcastID + `/episodes/` + soundOnEpisodeID + `</link><pubDate>Tue, 02 Jan 2024 10:00:00 +0000</pubDate><itunes:duration>123</itunes:duration><enclosure url="https://cdn.example.com/new.mp3" /></item>
		<item><title>Malformed</title><guid>no-id</guid><link>https://example.com</link></item>
	</channel></rss>`
	episodes, err := extractSoundOnPodcastEpisodes(feed, soundOnPodcastID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episodes = %#v", episodes)
	}
	if episodes[0].ProviderEpisodeID != soundOnEpisodeID || episodes[0].ProviderMediaID != soundOnPodcastID+"/"+soundOnEpisodeID || episodes[0].Title != "Newest" {
		t.Fatalf("newest episode = %#v", episodes[0])
	}
	if episodes[0].CanonicalURL != "https://player.soundon.fm/p/"+soundOnPodcastID+"/episodes/"+soundOnEpisodeID {
		t.Fatalf("canonical URL = %q", episodes[0].CanonicalURL)
	}
	if episodes[0].DurationSeconds != 123 || episodes[1].DurationSeconds != 3723 {
		t.Fatalf("durations = %d %d", episodes[0].DurationSeconds, episodes[1].DurationSeconds)
	}
	if episodes[0].PubDate.Format(time.DateOnly) != "2024-01-02" || episodes[1].PubDate.Format(time.DateOnly) != "2024-01-01" {
		t.Fatalf("pub dates = %s %s", episodes[0].PubDate, episodes[1].PubDate)
	}
}

func TestExtractSoundOnPodcastEpisodesParsesEmbeddedGUIDUUID(t *testing.T) {
	feed := `<rss><channel><item><title>Episode</title><guid>soundon:` + soundOnPodcastID + `:episode:` + soundOnEpisodeID + `</guid><pubDate>bad date</pubDate></item></channel></rss>`
	episodes, err := extractSoundOnPodcastEpisodes(feed, soundOnPodcastID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if len(episodes) != 1 || episodes[0].ProviderEpisodeID != soundOnEpisodeID {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestExtractSoundOnPodcastEpisodesParsesGUIDEpisodeURL(t *testing.T) {
	feed := `<rss><channel><item><title>Episode</title><guid>https://player.soundon.fm/p/` + soundOnPodcastID + `/episodes/` + soundOnEpisodeID + `</guid><pubDate>bad date</pubDate></item></channel></rss>`
	episodes, err := extractSoundOnPodcastEpisodes(feed, soundOnPodcastID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if len(episodes) != 1 || episodes[0].ProviderEpisodeID != soundOnEpisodeID || !episodes[0].PubDate.IsZero() {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestExtractSoundOnPodcastEpisodesSortsUnparseableDatesLast(t *testing.T) {
	olderEpisodeID := "281f554e-900e-4261-bc3c-1a3fe53a902a"
	feed := `<rss><channel>
		<item><title>No date</title><guid>` + soundOnEpisodeID + `</guid><pubDate>bad date</pubDate></item>
		<item><title>Dated</title><guid>` + olderEpisodeID + `</guid><pubDate>Tue, 02 Jan 2024 10:00:00 +0000</pubDate></item>
	</channel></rss>`
	episodes, err := extractSoundOnPodcastEpisodes(feed, soundOnPodcastID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if len(episodes) != 2 || episodes[0].ProviderEpisodeID != olderEpisodeID || episodes[1].ProviderEpisodeID != soundOnEpisodeID {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestExtractSoundOnPodcastEpisodesErrorsOnMalformedRSS(t *testing.T) {
	if _, err := extractSoundOnPodcastEpisodes(`<rss>`, soundOnPodcastID); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractSoundOnAudioURLMatchesGUIDBeforeLink(t *testing.T) {
	feed := `<rss><channel>
		<item><guid>other</guid><link>https://player.soundon.fm/p/` + soundOnPodcastID + `/episodes/` + soundOnEpisodeID + `</link><enclosure url="https://cdn.example.com/link.mp3" /></item>
		<item><guid>` + soundOnEpisodeID + `</guid><link>https://example.com</link><enclosure url="https://cdn.example.com/guid.mp3" /></item>
	</channel></rss>`
	got, err := extractSoundOnAudioURL(feed, soundOnEpisodeID, "https://player.soundon.fm/p/"+soundOnPodcastID+"/episodes/"+soundOnEpisodeID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if got != "https://cdn.example.com/guid.mp3" {
		t.Fatalf("audio URL = %q", got)
	}
}

func TestExtractSoundOnAudioURLFallsBackToLink(t *testing.T) {
	feed := `<rss><channel><item><guid>other</guid><link>https://player.soundon.fm/p/` + soundOnPodcastID + `/episodes/` + soundOnEpisodeID + `</link><enclosure url="https://cdn.example.com/link.mp3" /></item></channel></rss>`
	got, err := extractSoundOnAudioURL(feed, soundOnEpisodeID, "https://player.soundon.fm/p/"+soundOnPodcastID+"/episodes/"+soundOnEpisodeID)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if got != "https://cdn.example.com/link.mp3" {
		t.Fatalf("audio URL = %q", got)
	}
}

func TestExtractSoundOnAudioURLErrorsWithoutEnclosure(t *testing.T) {
	feed := `<rss><channel><item><guid>` + soundOnEpisodeID + `</guid></item></channel></rss>`
	if _, err := extractSoundOnAudioURL(feed, soundOnEpisodeID, ""); err == nil {
		t.Fatal("expected error")
	}
}
