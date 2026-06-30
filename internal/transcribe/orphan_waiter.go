package transcribe

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

const defaultOrphanPollInterval = time.Minute

var orphanScriptNames = map[string]struct{}{
	"faster_whisper_transcribe.py": {},
	"transcribe.py":                {},
	"extract.py":                   {},
}

type OrphanWaiter interface {
	Wait(ctx context.Context) error
}

type NoopOrphanWaiter struct{}

func (NoopOrphanWaiter) Wait(ctx context.Context) error {
	return ctx.Err()
}

type PythonProcessWaiter struct {
	ProcRoot     string
	PollInterval time.Duration
}

func (w PythonProcessWaiter) Wait(ctx context.Context) error {
	interval := w.PollInterval
	if interval <= 0 {
		interval = defaultOrphanPollInterval
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		active, err := w.hasActiveProcess()
		if err != nil {
			return err
		}
		if !active {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isMatchingPythonProcess(args []string) bool {
	if len(args) == 0 || !isPythonExecutable(args[0]) {
		return false
	}
	scriptArg := pythonScriptArg(args[1:])
	if scriptArg == "" {
		return false
	}
	_, ok := orphanScriptNames[filepath.Base(scriptArg)]
	return ok
}

func pythonScriptArg(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if arg == "-c" || strings.HasPrefix(arg, "-c") || arg == "-m" || strings.HasPrefix(arg, "-m") {
			return ""
		}
		if arg == "-X" || arg == "-W" || arg == "--check-hash-based-pycs" {
			i++
		}
	}
	return ""
}

func isPythonExecutable(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if name == "python" || name == "python.exe" {
		return true
	}
	if !strings.HasPrefix(name, "python3") {
		return false
	}
	for _, r := range strings.TrimPrefix(name, "python3") {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
