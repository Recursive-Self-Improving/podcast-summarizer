package transcribe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
)

func TestPipelineProcessesShortAudio(t *testing.T) {
	runner := &pipelineRunner{ffprobeStdout: "10.5\n"}
	tempRoot := t.TempDir()
	pipeline := Pipeline{Runner: runner, YTDLP: "yt-dlp", FFmpeg: "ffmpeg", FFprobe: "ffprobe", TempRoot: tempRoot, SegmentSeconds: 300}
	var input AudioInput

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, func(_ context.Context, got AudioInput) error {
		input = got
		if _, err := os.Stat(got.WAVPath); err != nil {
			t.Fatalf("wav missing during callback: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if input.WAVPath == "" || len(input.Segments) != 0 {
		t.Fatalf("input = %#v", input)
	}
	if _, err := os.Stat(input.WorkDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workdir was not cleaned up: %v", err)
	}
	assertCall(t, runner.calls[0], "yt-dlp", []string{"-f", "bestaudio[ext=m4a]/bestaudio"})
	assertCall(t, runner.calls[1], "ffmpeg", []string{"-ar", "16000", "-ac", "1"})
	assertCall(t, runner.calls[2], "ffprobe", []string{"format=duration"})
}

func TestPipelinePassesYTDLPArgs(t *testing.T) {
	runner := &pipelineRunner{}
	extraArgs := []string{"--extractor-args", "youtube:player_client=mweb"}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir(), YTDLPArgs: extraArgs}

	if err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, nil); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if !slices.Equal(runner.calls[0].Args[:2], extraArgs) {
		t.Fatalf("yt-dlp args = %#v", runner.calls[0].Args)
	}
}

func TestPipelineSplitsLongAudio(t *testing.T) {
	runner := &pipelineRunner{ffprobeStdout: "601\n"}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir(), SegmentSeconds: 300}
	var input AudioInput

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, func(_ context.Context, got AudioInput) error {
		input = got
		for _, segment := range got.Segments {
			if _, err := os.Stat(segment); err != nil {
				t.Fatalf("segment missing during callback: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(input.Segments) != 2 || !strings.HasSuffix(input.Segments[0], "part_000.wav") || !strings.HasSuffix(input.Segments[1], "part_001.wav") {
		t.Fatalf("segments = %#v", input.Segments)
	}
	assertCall(t, runner.calls[3], "ffmpeg", []string{"-f", "segment", "-segment_time", "300"})
}

func TestPipelineUsesWAVSizeDurationFallback(t *testing.T) {
	runner := &pipelineRunner{ffprobeErr: errors.New("ffprobe unavailable"), wavBytes: 16000 * 2 * 10}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir(), SegmentSeconds: 300}
	var input AudioInput

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, func(_ context.Context, got AudioInput) error {
		input = got
		return nil
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(input.Segments) != 0 {
		t.Fatalf("segments = %#v", input.Segments)
	}
}

func TestPipelinePassesSourceURLToYTDLP(t *testing.T) {
	runner := &pipelineRunner{}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir()}

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, nil)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(runner.calls) == 0 {
		t.Fatal("expected yt-dlp call")
	}
	if got, want := runner.calls[0].Args[len(runner.calls[0].Args)-1], "https://www.youtube.com/watch?v=abc12345678"; got != want {
		t.Fatalf("url arg = %q, want %q", got, want)
	}
}

func TestPipelineDownloadsDirectAudio(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()
	runner := &pipelineRunner{}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir(), HTTPClient: server.Client()}

	if err := pipeline.Process(context.Background(), AudioSource{URL: server.URL + "/audio.mp3", Direct: true}, nil); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(runner.calls) == 0 || runner.calls[0].Name != "ffmpeg" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestPipelineFailsWhenDownloadedAudioMissing(t *testing.T) {
	runner := &pipelineRunner{skipDownloadFile: true}
	pipeline := Pipeline{Runner: runner, TempRoot: t.TempDir()}

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, nil)
	if err == nil || !strings.Contains(err.Error(), "downloaded audio file not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestPipelineCleansUpAfterCommandFailure(t *testing.T) {
	runner := &pipelineRunner{convertErr: errors.New("ffmpeg failed")}
	tempRoot := t.TempDir()
	pipeline := Pipeline{Runner: runner, TempRoot: tempRoot}

	err := pipeline.Process(context.Background(), AudioSource{URL: "https://www.youtube.com/watch?v=abc12345678"}, nil)
	if err == nil {
		t.Fatal("Process returned nil error")
	}
	entries, readErr := os.ReadDir(tempRoot)
	if readErr != nil {
		t.Fatalf("ReadDir returned error: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temp files not cleaned up: %#v", entries)
	}
}

type pipelineRunner struct {
	calls            []commandrunner.Call
	ffprobeStdout    string
	ffprobeErr       error
	convertErr       error
	skipDownloadFile bool
	wavBytes         int
}

func (r *pipelineRunner) Run(_ context.Context, name string, args ...string) (commandrunner.Result, error) {
	r.calls = append(r.calls, commandrunner.Call{Name: name, Args: append([]string(nil), args...)})
	switch name {
	case "yt-dlp":
		if !r.skipDownloadFile {
			return commandrunner.Result{}, os.WriteFile(downloadPathFromArgs(args), []byte("audio"), 0o644)
		}
	case "ffmpeg":
		if slices.Contains(args, "segment") {
			return r.writeSegments(args)
		}
		if r.convertErr != nil {
			return commandrunner.Result{}, r.convertErr
		}
		return r.writeWAV(args)
	case "ffprobe":
		if r.ffprobeErr != nil {
			return commandrunner.Result{}, r.ffprobeErr
		}
		stdout := r.ffprobeStdout
		if stdout == "" {
			stdout = "1\n"
		}
		return commandrunner.Result{Stdout: stdout}, nil
	}
	return commandrunner.Result{}, nil
}

func (r *pipelineRunner) writeSegments(args []string) (commandrunner.Result, error) {
	pattern := args[len(args)-1]
	if err := os.MkdirAll(filepath.Dir(pattern), 0o755); err != nil {
		return commandrunner.Result{}, err
	}
	if err := os.WriteFile(strings.Replace(pattern, "%03d", "000", 1), []byte("part0"), 0o644); err != nil {
		return commandrunner.Result{}, err
	}
	return commandrunner.Result{}, os.WriteFile(strings.Replace(pattern, "%03d", "001", 1), []byte("part1"), 0o644)
}

func (r *pipelineRunner) writeWAV(args []string) (commandrunner.Result, error) {
	wavBytes := r.wavBytes
	if wavBytes == 0 {
		wavBytes = 100
	}
	return commandrunner.Result{}, os.WriteFile(args[len(args)-1], make([]byte, wavBytes), 0o644)
}

func downloadPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			return strings.Replace(args[i+1], "%(ext)s", "m4a", 1)
		}
	}
	return ""
}

func assertCall(t *testing.T, call commandrunner.Call, name string, args []string) {
	t.Helper()
	if call.Name != name {
		t.Fatalf("call name = %q, want %q", call.Name, name)
	}
	for _, arg := range args {
		if !slices.Contains(call.Args, arg) {
			t.Fatalf("call args = %#v, want %q", call.Args, arg)
		}
	}
}
