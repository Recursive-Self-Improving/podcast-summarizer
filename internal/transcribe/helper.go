package transcribe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	embeddedassets "github.com/Recursive-Self-Improving/podcast-summarizer"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
)

const (
	DefaultPythonPath     = "python3"
	DefaultWhisperModel   = "small"
	DefaultWhisperDevice  = "cpu"
	DefaultWhisperCompute = "int8"
	DefaultHelperScript   = "scripts/faster_whisper_transcribe.py"
	WhisperSource         = "whisper"
)

type Helper struct {
	Runner         commandrunner.Runner
	OrphanWaiter   OrphanWaiter
	PythonPath     string
	ScriptPath     string
	Model          string
	Device         string
	Compute        string
	SegmentSeconds int
}

type Transcript struct {
	Source string
	Text   string
}

func (h Helper) Transcribe(ctx context.Context, input AudioInput) (Transcript, error) {
	runner := h.Runner
	if runner == nil {
		runner = commandrunner.ExecRunner{}
	}
	audioInput, err := transcriptionInput(input)
	if err != nil {
		return Transcript{}, err
	}
	scriptPath, err := h.scriptPath(input.WorkDir)
	if err != nil {
		return Transcript{}, err
	}
	outputPath := filepath.Join(input.WorkDir, "transcript.txt")
	if err := h.orphanWaiter().Wait(ctx); err != nil {
		return Transcript{}, fmt.Errorf("wait for orphan faster-whisper process: %w", err)
	}
	result, err := runner.Run(
		ctx,
		defaultString(h.PythonPath, DefaultPythonPath),
		scriptPath,
		"--input", audioInput,
		"--output", outputPath,
		"--model", defaultString(h.Model, DefaultWhisperModel),
		"--device", defaultString(h.Device, DefaultWhisperDevice),
		"--compute", defaultString(h.Compute, DefaultWhisperCompute),
		"--segment-sec", strconv.Itoa(h.segmentSeconds()),
	)
	if err != nil {
		return Transcript{}, helperError(err, result.Stderr)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return Transcript{}, fmt.Errorf("read transcript output: %w", err)
	}
	text := string(content)
	if len(strings.TrimSpace(text)) == 0 {
		return Transcript{}, fmt.Errorf("transcript output is empty")
	}
	return Transcript{Source: WhisperSource, Text: text}, nil
}

func transcriptionInput(input AudioInput) (string, error) {
	if len(input.Segments) > 0 {
		return filepath.Dir(input.Segments[0]), nil
	}
	if input.WAVPath == "" {
		return "", fmt.Errorf("wav input is required")
	}
	return input.WAVPath, nil
}

func (h Helper) scriptPath(workDir string) (string, error) {
	if h.ScriptPath != "" {
		return h.ScriptPath, nil
	}
	contents, err := embeddedassets.HelperScripts.ReadFile(DefaultHelperScript)
	if err != nil {
		return "", fmt.Errorf("read embedded faster-whisper helper: %w", err)
	}
	path := filepath.Join(workDir, filepath.Base(DefaultHelperScript))
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return "", fmt.Errorf("write embedded faster-whisper helper: %w", err)
	}
	return path, nil
}

func (h Helper) segmentSeconds() int {
	if h.SegmentSeconds > 0 {
		return h.SegmentSeconds
	}
	return defaultSegmentSeconds
}

func (h Helper) orphanWaiter() OrphanWaiter {
	if h.OrphanWaiter != nil {
		return h.OrphanWaiter
	}
	return DefaultOrphanWaiter()
}

func helperError(err error, stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		return fmt.Errorf("run faster-whisper helper: %w", err)
	}
	return fmt.Errorf("run faster-whisper helper: %w: %s", err, message)
}
