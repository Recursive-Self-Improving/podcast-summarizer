package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXiaoyuzhouNormalizeAcceptedURLs(t *testing.T) {
	const id = "69ebf1d71d989496e7729801"
	for _, rawURL := range []string{
		"https://www.xiaoyuzhoufm.com/episode/" + id,
		"https://xiaoyuzhoufm.com/episode/" + id,
		"www.xiaoyuzhoufm.com/episode/" + id,
		"xiaoyuzhoufm.com/episode/" + id,
	} {
		t.Run(rawURL, func(t *testing.T) {
			ref, err := XiaoyuzhouFM{}.Normalize(rawURL)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if ref.Provider != "xiaoyuzhou" || ref.ProviderMediaID != id || ref.CanonicalURL != "https://www.xiaoyuzhoufm.com/episode/"+id {
				t.Fatalf("ref = %#v", ref)
			}
		})
	}
}

func TestXiaoyuzhouNormalizeRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://www.xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801",
		"https://example.com/episode/69ebf1d71d989496e7729801",
		"https://www.xiaoyuzhoufm.com/podcast/69ebf1d71d989496e7729801",
		"https://www.xiaoyuzhoufm.com/episode/",
		"https://www.xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801/extra",
		"https://www.xiaoyuzhoufm.com/episode/%2F69ebf1d71d989496e7729801",
		"not a url",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := (XiaoyuzhouFM{}).Normalize(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestXiaoyuzhouPodcastNormalizeAcceptedURLs(t *testing.T) {
	const id = "5e2839ca418a84a0461fc5f4"
	for _, rawURL := range []string{
		"https://www.xiaoyuzhoufm.com/podcast/" + id,
		"https://xiaoyuzhoufm.com/podcast/" + id,
		"www.xiaoyuzhoufm.com/podcast/" + id,
		"xiaoyuzhoufm.com/podcast/" + id,
	} {
		t.Run(rawURL, func(t *testing.T) {
			ref, err := XiaoyuzhouFM{}.NormalizePodcast(rawURL)
			if err != nil {
				t.Fatalf("NormalizePodcast returned error: %v", err)
			}
			if ref.Provider != "xiaoyuzhou" || ref.ProviderFeedID != id || ref.CanonicalURL != "https://www.xiaoyuzhoufm.com/podcast/"+id {
				t.Fatalf("ref = %#v", ref)
			}
		})
	}
}

func TestXiaoyuzhouPodcastNormalizeRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://www.xiaoyuzhoufm.com/podcast/5e2839ca418a84a0461fc5f4",
		"https://example.com/podcast/5e2839ca418a84a0461fc5f4",
		"https://www.xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801",
		"https://www.xiaoyuzhoufm.com/podcast/",
		"https://www.xiaoyuzhoufm.com/podcast/5e2839ca418a84a0461fc5f4/extra",
		"https://www.xiaoyuzhoufm.com/podcast/%2F5e2839ca418a84a0461fc5f4",
		"https://www.xiaoyuzhoufm.com/podcast/not-valid!",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := (XiaoyuzhouFM{}).NormalizePodcast(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExtractXiaoyuzhouPodcastEpisodes(t *testing.T) {
	body := `<html><script type="application/json" data-extra="yes" id="__NEXT_DATA__">{"props":{"pageProps":{"podcast":{"title":"Podcast Title","episodes":[{"eid":"old123","title":"Old","pubDate":1704103200,"duration":3661},{"eid":"new456","title":"New","pubDate":1704189600,"duration":"02:03"},{"eid":"nodate789","title":"No Date","pubDate":"bad","duration":"99:99"}]}}}}</script></html>`
	episodes, title, err := extractXiaoyuzhouPodcastEpisodes(body)
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if title != "Podcast Title" || len(episodes) != 3 {
		t.Fatalf("title=%q episodes=%#v", title, episodes)
	}
	if episodes[0].ProviderEpisodeID != "new456" || episodes[0].ProviderMediaID != "new456" || episodes[0].CanonicalURL != "https://www.xiaoyuzhoufm.com/episode/new456" || episodes[0].DurationSeconds != 123 {
		t.Fatalf("newest episode = %#v", episodes[0])
	}
	if episodes[1].DurationSeconds != 3661 || episodes[2].ProviderEpisodeID != "nodate789" || !episodes[2].PubDate.IsZero() || episodes[2].DurationSeconds != 0 {
		t.Fatalf("remaining episodes = %#v", episodes[1:])
	}
}

func TestExtractXiaoyuzhouPodcastEpisodesErrorsOnMalformedResponses(t *testing.T) {
	for _, body := range []string{
		`no next data`,
		`<script id="__NEXT_DATA__" type="application/json">{</script>`,
		`<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"podcast":{"episodes":[]}}}}</script>`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, _, err := extractXiaoyuzhouPodcastEpisodes(body); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPodcastRegistryFindsOnlyPodcastProviders(t *testing.T) {
	registry := DefaultPodcastRegistry()
	_, ref, err := registry.Find("https://player.soundon.fm/p/" + soundOnPodcastID)
	if err != nil {
		t.Fatalf("Find SoundOn returned error: %v", err)
	}
	if ref.Provider != "soundon" {
		t.Fatalf("ref = %#v", ref)
	}
	_, ref, err = registry.Find("https://www.xiaoyuzhoufm.com/podcast/5e2839ca418a84a0461fc5f4")
	if err != nil {
		t.Fatalf("Find Xiaoyuzhou returned error: %v", err)
	}
	if ref.Provider != "xiaoyuzhou" {
		t.Fatalf("ref = %#v", ref)
	}
	for _, rawURL := range []string{
		"https://www.youtube.com/watch?v=abc12345678",
		"https://player.soundon.fm/p/" + soundOnPodcastID + "/episodes/" + soundOnEpisodeID,
		"https://www.xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, _, err := registry.Find(rawURL); !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("Find error = %v, want ErrInvalidURL", err)
			}
		})
	}
}

func TestFetchTextUsesConfiguredUserAgentAndBoundsResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "browser-like" {
			t.Fatalf("User-Agent = %q", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	body, err := (HTTPAudioResolver{Client: server.Client()}).fetchTextWithUserAgent(context.Background(), server.URL, "browser-like")
	if err != nil {
		t.Fatalf("fetchTextWithUserAgent returned error: %v", err)
	}
	if body != "ok" {
		t.Fatalf("body = %q", body)
	}

	largeServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxProviderFetchBytes+1)))
	}))
	defer largeServer.Close()
	if _, err := (HTTPAudioResolver{Client: largeServer.Client()}).fetchTextWithUserAgent(context.Background(), largeServer.URL, "browser-like"); err == nil {
		t.Fatal("expected large response error")
	}
}

func TestExtractXiaoyuzhouAudioURL(t *testing.T) {
	for _, body := range []string{
		`<script>"https://media.xyzcdn.net/foo/bar.m4a"</script>`,
		`{"url":"https:\/\/media.xyzcdn.net\/shows\/bar.mp3?token=abc"}`,
		`{"url":"https:\/\/dts-api.xiaoyuzhoufm.com\/track\/abc\/media.xyzcdn.net\/shows\/bar.m4a"}`,
	} {
		t.Run(body, func(t *testing.T) {
			got, err := extractXiaoyuzhouAudioURL(body)
			if err != nil {
				t.Fatalf("extract returned error: %v", err)
			}
			if got == "" {
				t.Fatal("expected audio URL")
			}
		})
	}
}

func TestExtractXiaoyuzhouAudioURLErrorsWhenMissing(t *testing.T) {
	if _, err := extractXiaoyuzhouAudioURL("no media here"); err == nil {
		t.Fatal("expected error")
	}
}
