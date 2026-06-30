package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
)

func TestHelperTranscribesWAVFile(t *testing.T) {
	runner := &helperRunner{transcript: "[00:00:00.000 --> 00:00:01.000] hello\n"}
	workdir := t.TempDir()
	wavPath := filepath.Join(workdir, "audio.wav")
	if err := os.WriteFile(wavPath, []byte("wav"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	helper := Helper{Runner: runner, OrphanWaiter: NoopOrphanWaiter{}, PythonPath: "python", ScriptPath: "helper.py", Model: "medium", Device: "cuda", Compute: "float16", SegmentSeconds: 600}

	transcript, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: workdir, WAVPath: wavPath})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if transcript.Source != WhisperSource || transcript.Text != runner.transcript {
		t.Fatalf("transcript = %#v", transcript)
	}
	call := runner.calls[0]
	if call.Name != "python" {
		t.Fatalf("call name = %q", call.Name)
	}
	wantPairs := []struct {
		name  string
		value string
	}{
		{name: "--input", value: wavPath},
		{name: "--model", value: "medium"},
		{name: "--device", value: "cuda"},
		{name: "--compute", value: "float16"},
		{name: "--segment-sec", value: "600"},
	}
	for _, pair := range wantPairs {
		if !containsArgPair(call.Args, pair.name, pair.value) {
			t.Fatalf("call args = %#v, want %s %s", call.Args, pair.name, pair.value)
		}
	}
}

func TestHelperTranscribesSegmentDirectory(t *testing.T) {
	runner := &helperRunner{transcript: "[00:05:00.000 --> 00:05:01.000] later\n"}
	workdir := t.TempDir()
	segmentDir := filepath.Join(workdir, "segments")
	segments := []string{filepath.Join(segmentDir, "part_000.wav"), filepath.Join(segmentDir, "part_001.wav")}
	helper := Helper{Runner: runner, OrphanWaiter: NoopOrphanWaiter{}}

	_, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: workdir, Segments: segments})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	call := runner.calls[0]
	if filepath.Base(call.Args[0]) != "faster_whisper_transcribe.py" {
		t.Fatalf("script path = %q", call.Args[0])
	}
	if _, err := os.Stat(call.Args[0]); err != nil {
		t.Fatalf("embedded helper script was not written: %v", err)
	}
	if !containsArgPair(call.Args, "--input", segmentDir) {
		t.Fatalf("call args = %#v", call.Args)
	}
	if !containsArgPair(call.Args, "--segment-sec", "300") {
		t.Fatalf("call args = %#v", call.Args)
	}
}

func TestHelperReturnsCommandStderr(t *testing.T) {
	runner := &helperRunner{err: errors.New("exit status 1"), stderr: "transcription failed: missing input"}
	workdir := t.TempDir()
	helper := Helper{Runner: runner, OrphanWaiter: NoopOrphanWaiter{}}

	_, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: workdir, WAVPath: filepath.Join(workdir, "audio.wav")})
	if err == nil || !strings.Contains(err.Error(), "transcription failed: missing input") {
		t.Fatalf("error = %v", err)
	}
}

func TestHelperRequiresAudioInput(t *testing.T) {
	helper := Helper{Runner: &helperRunner{}, OrphanWaiter: NoopOrphanWaiter{}}

	_, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "wav input is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestHelperRunsOrphanWaiterBeforeCommand(t *testing.T) {
	events := []string{}
	runner := &helperRunner{transcript: "hello", onRun: func() { events = append(events, "run") }}
	waiter := &fakeOrphanWaiter{onWait: func() { events = append(events, "wait") }}
	workdir := t.TempDir()
	helper := Helper{Runner: runner, OrphanWaiter: waiter, ScriptPath: "helper.py"}

	_, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: workdir, WAVPath: filepath.Join(workdir, "audio.wav")})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if waiter.calls != 1 {
		t.Fatalf("waiter calls = %d", waiter.calls)
	}
	if strings.Join(events, ",") != "wait,run" {
		t.Fatalf("events = %#v", events)
	}
}

func TestHelperDoesNotRunCommandWhenOrphanWaiterFails(t *testing.T) {
	runner := &helperRunner{transcript: "hello"}
	helper := Helper{Runner: runner, OrphanWaiter: &fakeOrphanWaiter{err: context.Canceled}, ScriptPath: "helper.py"}
	workdir := t.TempDir()

	_, err := helper.Transcribe(context.Background(), AudioInput{WorkDir: workdir, WAVPath: filepath.Join(workdir, "audio.wav")})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "wait for orphan faster-whisper process") {
		t.Fatalf("error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

type fakeOrphanWaiter struct {
	calls  int
	err    error
	onWait func()
}

func (w *fakeOrphanWaiter) Wait(context.Context) error {
	w.calls++
	if w.onWait != nil {
		w.onWait()
	}
	return w.err
}

type helperRunner struct {
	calls      []commandrunner.Call
	transcript string
	stderr     string
	err        error
	onRun      func()
}

func (r *helperRunner) Run(_ context.Context, name string, args ...string) (commandrunner.Result, error) {
	if r.onRun != nil {
		r.onRun()
	}
	r.calls = append(r.calls, commandrunner.Call{Name: name, Args: append([]string(nil), args...)})
	if r.err != nil {
		return commandrunner.Result{Stderr: r.stderr}, r.err
	}
	return commandrunner.Result{}, os.WriteFile(outputPathFromArgs(args), []byte(r.transcript), 0o644)
}

func outputPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--output" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func containsArgPair(args []string, name, value string) bool {
	for i, arg := range args {
		if arg == name && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}
