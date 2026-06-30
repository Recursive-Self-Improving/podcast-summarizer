package transcript

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
)

func TestDownloaderManualSuccess(t *testing.T) {
	runner := &subtitleRunner{mode: "manual"}
	result, err := Downloader{Runner: runner, YTDLP: "yt-dlp", TempRoot: t.TempDir()}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if result.Source != "manual_subtitle" || result.Text != "[1.0s -> 2.0s] manual" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "--write-subs" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDownloaderAutoFallbackSuccess(t *testing.T) {
	runner := &subtitleRunner{mode: "auto"}
	result, err := Downloader{Runner: runner, TempRoot: t.TempDir()}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if result.Source != "auto_subtitle" || result.Text != "[3.0s -> 4.0s] auto" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.calls) != 2 || runner.calls[0][0] != "--write-subs" || runner.calls[1][0] != "--write-auto-subs" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDownloaderUsesSubtitleDespiteCommandFailure(t *testing.T) {
	runner := &subtitleRunner{mode: "manual-error"}
	result, err := Downloader{Runner: runner, TempRoot: t.TempDir()}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if result.Source != "manual_subtitle" || result.Text != "[1.0s -> 2.0s] manual" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDownloaderStopsOnCleanupFailure(t *testing.T) {
	runner := &subtitleRunner{mode: "cleanup-error"}
	_, err := Downloader{Runner: runner, TempRoot: t.TempDir()}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if !commandrunner.HasCleanupFailure(err) {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDownloaderPassesYTDLPArgs(t *testing.T) {
	runner := &subtitleRunner{mode: "manual"}
	extraArgs := []string{"--extractor-args", "youtube:player_client=mweb"}

	_, err := Downloader{Runner: runner, TempRoot: t.TempDir(), YTDLPArgs: extraArgs}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if !slices.Equal(runner.calls[0][:2], extraArgs) {
		t.Fatalf("call args = %#v", runner.calls[0])
	}
}

func TestDownloaderUsesLanguagePreferenceOrder(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "subtitle")
	if err := writeSubtitle(prefix+".en.srt", "english", 1, 2); err != nil {
		t.Fatalf("write english subtitle: %v", err)
	}
	if err := writeSubtitle(prefix+".zh.srt", "chinese", 3, 4); err != nil {
		t.Fatalf("write chinese subtitle: %v", err)
	}

	text, err := parseFirstSubtitle(prefix)
	if err != nil {
		t.Fatalf("parseFirstSubtitle returned error: %v", err)
	}
	if text != "[3.0s -> 4.0s] chinese" {
		t.Fatalf("text = %q", text)
	}
}

func TestDownloaderRejectsNonCanonicalYouTubeURL(t *testing.T) {
	runner := &subtitleRunner{mode: "manual"}
	_, err := Downloader{Runner: runner, TempRoot: t.TempDir()}.Download(context.Background(), "https://youtu.be/abcDEF123_4")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDownloaderNoSubtitleFiles(t *testing.T) {
	runner := &subtitleRunner{mode: "none"}
	_, err := Downloader{Runner: runner, TempRoot: t.TempDir()}.Download(context.Background(), "https://www.youtube.com/watch?v=abcDEF123_4")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

type subtitleRunner struct {
	mode  string
	calls [][]string
}

func (r *subtitleRunner) Run(_ context.Context, _ string, args ...string) (commandrunner.Result, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	prefix := outputPrefix(args)
	if prefix == "" {
		return commandrunner.Result{}, nil
	}
	isAuto := slices.Contains(args, "--write-auto-subs")
	if r.mode == "manual" && !isAuto {
		return commandrunner.Result{}, writeSubtitle(prefix+".en.srt", "manual", 1, 2)
	}
	if r.mode == "manual-error" && !isAuto {
		if err := writeSubtitle(prefix+".en.srt", "manual", 1, 2); err != nil {
			return commandrunner.Result{}, err
		}
		return commandrunner.Result{}, errors.New("yt-dlp failed after writing subtitles")
	}
	if r.mode == "cleanup-error" && !isAuto {
		return commandrunner.Result{}, commandrunner.MarkCleanupFailure(errors.New("cleanup failed"))
	}
	if r.mode == "auto" && isAuto {
		return commandrunner.Result{}, writeSubtitle(prefix+".en.srt", "auto", 3, 4)
	}
	return commandrunner.Result{}, nil
}

func outputPrefix(args []string) string {
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func writeSubtitle(path, text string, start, end int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("1\n00:00:0"+itoa(start)+",000 --> 00:00:0"+itoa(end)+",000\n"+text+"\n"), 0o644)
}

func itoa(value int) string {
	return string(rune('0' + value))
}
