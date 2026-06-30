//go:build linux

package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPythonProcessWaiterFindsMatchingProcess(t *testing.T) {
	procRoot := t.TempDir()
	writeProcCmdline(t, procRoot, "123", []string{"python3", "/tmp/faster_whisper_transcribe.py", "--input", "audio.wav"})

	active, err := PythonProcessWaiter{ProcRoot: procRoot}.hasActiveProcess()
	if err != nil {
		t.Fatalf("hasActiveProcess returned error: %v", err)
	}
	if !active {
		t.Fatal("hasActiveProcess = false, want true")
	}
}

func TestPythonProcessWaiterIgnoresUnrelatedProcesses(t *testing.T) {
	procRoot := t.TempDir()
	writeProcCmdline(t, procRoot, "123", []string{"node", "transcribe.py"})
	writeProcCmdline(t, procRoot, "456", []string{"python3", "worker.py"})

	active, err := PythonProcessWaiter{ProcRoot: procRoot}.hasActiveProcess()
	if err != nil {
		t.Fatalf("hasActiveProcess returned error: %v", err)
	}
	if active {
		t.Fatal("hasActiveProcess = true, want false")
	}
}

func TestPythonProcessWaiterWaitReturnsWhenProcessDisappears(t *testing.T) {
	procRoot := t.TempDir()
	pidDir := writeProcCmdline(t, procRoot, "123", []string{"python3", "extract.py"})
	waiter := PythonProcessWaiter{ProcRoot: procRoot, PollInterval: time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- waiter.Wait(context.Background()) }()

	deadline := time.After(time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("Wait returned before process disappeared: %v", err)
		case <-deadline:
			t.Fatal("Wait did not observe matching process")
		default:
		}
		active, err := waiter.hasActiveProcess()
		if err != nil {
			t.Fatalf("hasActiveProcess returned error: %v", err)
		}
		if active {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if err := os.RemoveAll(pidDir); err != nil {
		t.Fatalf("remove proc entry: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after process disappeared")
	}
}

func TestPythonProcessWaiterWaitCancelsCleanly(t *testing.T) {
	procRoot := t.TempDir()
	writeProcCmdline(t, procRoot, "123", []string{"python3", "transcribe.py"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := PythonProcessWaiter{ProcRoot: procRoot, PollInterval: time.Millisecond}.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v", err)
	}
}

func writeProcCmdline(t *testing.T, procRoot, pid string, args []string) string {
	t.Helper()
	pidDir := filepath.Join(procRoot, pid)
	if err := os.Mkdir(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir proc entry: %v", err)
	}
	contents := []byte{}
	for _, arg := range args {
		contents = append(contents, arg...)
		contents = append(contents, 0)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), contents, 0o644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}
	return pidDir
}
