package provider

import (
	"errors"
	"testing"
)

func TestYouTubeNormalizeAcceptedURLs(t *testing.T) {
	const id = "abcDEF123_4"
	for _, rawURL := range []string{
		"https://www.youtube.com/watch?v=" + id,
		"https://youtube.com/watch?v=" + id,
		"https://m.youtube.com/watch?v=" + id,
		"https://youtu.be/" + id,
		"https://www.youtube.com/shorts/" + id,
	} {
		t.Run(rawURL, func(t *testing.T) {
			ref, err := YouTube{}.Normalize(rawURL)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if ref.Provider != "youtube" || ref.ProviderMediaID != id || ref.CanonicalURL != "https://www.youtube.com/watch?v="+id {
				t.Fatalf("ref = %#v", ref)
			}
		})
	}
}

func TestYouTubeNormalizeShortShareURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://youtu.be/Vdy7_ihnpJ4",
		"https://youtu.be/Vdy7%5FihnpJ4",
		"https://www.youtube.com/shorts/Vdy7%5FihnpJ4",
	} {
		t.Run(rawURL, func(t *testing.T) {
			ref, err := YouTube{}.Normalize(rawURL)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if ref.ProviderMediaID != "Vdy7_ihnpJ4" || ref.CanonicalURL != "https://www.youtube.com/watch?v=Vdy7_ihnpJ4" {
				t.Fatalf("ref = %#v", ref)
			}
		})
	}
}

func TestValidateCanonicalYouTubeURL(t *testing.T) {
	if err := ValidateCanonicalYouTubeURL("https://www.youtube.com/watch?v=abcDEF123_4"); err != nil {
		t.Fatalf("ValidateCanonicalYouTubeURL returned error: %v", err)
	}
	for _, rawURL := range []string{
		"https://youtu.be/abcDEF123_4",
		"https://youtube.com/watch?v=abcDEF123_4",
		"https://example.com/watch?v=abcDEF123_4",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := ValidateCanonicalYouTubeURL(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestYouTubeNormalizeRejectsInvalidURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://www.youtube.com/watch?v=abcDEF123_4",
		"https://example.com/watch?v=abcDEF123_4",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=tooLongVideo",
		"https://www.youtube.com/watch?v=bad*chars!!",
		"https://www.youtube.com/playlist?list=abcDEF123_4",
		"https://youtu.be/abcDEF123_4/extra",
		"https://youtu.be/%2FabcDEF123_4",
		"https://youtu.be/abcDEF123_4%2F",
		"https://www.youtube.com/shorts/%2FabcDEF123_4",
		"https://www.youtube.com/shorts/abcDEF123_4%2F",
		"not a url",
	} {
		t.Run(rawURL, func(t *testing.T) {
			youtube := YouTube{}
			if _, err := youtube.Normalize(rawURL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRegistryFindsSupportedProviders(t *testing.T) {
	registry := DefaultRegistry()
	for _, tc := range []struct {
		rawURL   string
		provider string
		mediaID  string
	}{
		{"https://youtu.be/abcDEF123_4", "youtube", "abcDEF123_4"},
		{"xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801", "xiaoyuzhou", "69ebf1d71d989496e7729801"},
		{"player.soundon.fm/p/954689a5-3096-43a4-a80b-7810b219cef3/episodes/181f554e-900e-4261-bc3c-1a3fe53a902a", "soundon", "954689a5-3096-43a4-a80b-7810b219cef3/181f554e-900e-4261-bc3c-1a3fe53a902a"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			provider, ref, err := registry.Find(tc.rawURL)
			if err != nil {
				t.Fatalf("Find returned error: %v", err)
			}
			if provider.Name() != tc.provider || ref.ProviderMediaID != tc.mediaID {
				t.Fatalf("provider=%s ref=%#v", provider.Name(), ref)
			}
		})
	}

	if _, _, err := registry.Find("https://example.com/episode/1"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("expected invalid URL error, got %v", err)
	}
}
