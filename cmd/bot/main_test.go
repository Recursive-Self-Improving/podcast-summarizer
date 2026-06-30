package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/config"
)

func TestValidateExecutableDepsAcceptsConfiguredExecutables(t *testing.T) {
	executable := writeExecutable(t, "tool")
	cfg := config.Config{
		YTDLPPath:  executable,
		FFmpegPath: executable,
		PythonPath: executable,
	}

	if err := validateExecutableDeps(cfg); err != nil {
		t.Fatalf("validateExecutableDeps returned error: %v", err)
	}
}

func TestValidateExecutableDepsReportsMissingConfiguredExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-yt-dlp")
	cfg := config.Config{
		YTDLPPath:  missing,
		FFmpegPath: writeExecutable(t, "ffmpeg"),
		PythonPath: writeExecutable(t, "python"),
	}

	err := validateExecutableDeps(cfg)
	if err == nil {
		t.Fatal("validateExecutableDeps returned nil error")
	}
	message := err.Error()
	if !strings.Contains(message, "YT_DLP_PATH") || !strings.Contains(message, missing) {
		t.Fatalf("error = %q", message)
	}
}

func writeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}
