package commandrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFakeRunnerPreservesArguments(t *testing.T) {
	runner := &FakeRunner{Results: []FakeResult{{Result: Result{Stdout: "ok", ExitCode: 0}}}}
	result, err := runner.Run(context.Background(), "yt-dlp", "--output", "$(touch pwned)", "https://example.com?a=b c")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Stdout != "ok" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("calls = %#v", runner.Calls)
	}
	call := runner.Calls[0]
	if call.Name != "yt-dlp" {
		t.Fatalf("name = %q", call.Name)
	}
	wantArgs := []string{"--output", "$(touch pwned)", "https://example.com?a=b c"}
	if len(call.Args) != len(wantArgs) {
		t.Fatalf("args = %#v", call.Args)
	}
	for i := range wantArgs {
		if call.Args[i] != wantArgs[i] {
			t.Fatalf("args = %#v", call.Args)
		}
	}
}

func TestExecRunnerDoesNotUseShellInterpolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "printargs")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\"\nprintf '%s\\n' \"$2\"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := ExecRunner{}.Run(context.Background(), script, "$(echo injected)", "a b")
	if err != nil {
		t.Fatalf("Run returned error: %v result=%#v", err, result)
	}
	if result.Stdout != "$(echo injected)\na b\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
}

func TestExecRunnerCapturesStderrAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fail")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho out\necho err >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := ExecRunner{}.Run(context.Background(), script)
	if err == nil {
		t.Fatal("expected error")
	}
	if result.Stdout != "out\n" || result.Stderr != "err\n" || result.ExitCode != 7 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(err.Error(), "stderr: err") || !strings.Contains(err.Error(), "stdout: out") {
		t.Fatalf("error missing command output: %v", err)
	}
}

func TestExecRunnerDoesNotStartWithCanceledContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	script := filepath.Join(dir, "mark-started")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ExecRunner{}.Run(ctx, script, marker)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if result.TimedOut {
		t.Fatalf("expected canceled result without timeout, got %#v", result)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("marker file was created or stat failed: %v", statErr)
	}
}

func TestExecRunnerReportsTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "sleep")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, err := ExecRunner{}.Run(ctx, script)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !result.TimedOut {
		t.Fatalf("expected TimedOut result, got %#v", result)
	}
}

func TestFakeRunnerReturnsConfiguredError(t *testing.T) {
	boom := errors.New("boom")
	runner := &FakeRunner{Results: []FakeResult{{Err: boom}}}
	_, err := runner.Run(context.Background(), "cmd")
	if !errors.Is(err, boom) {
		t.Fatalf("expected configured error, got %v", err)
	}
}
