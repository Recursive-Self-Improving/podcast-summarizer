//go:build unix

package commandrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecRunnerTerminatesChildProcessOnCancellation(t *testing.T) {
	withCommandWaitDelay(t, 100*time.Millisecond)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "spawn-child")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsh -c 'trap \"exit 0\" TERM; while :; do sleep 1; done' &\necho $! > \"$1\"\nwait\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ExecRunner{}.Run(ctx, script, pidFile)
		done <- err
	}()

	pid := readPIDEventually(t, pidFile)
	cancel()

	assertCommandCanceled(t, done, pid)
}

func TestExecRunnerKillsTermIgnoringProcessOnCancellation(t *testing.T) {
	withCommandWaitDelay(t, 100*time.Millisecond)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "ignore-term")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap '' TERM\necho $$ > \"$1\"\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ExecRunner{}.Run(ctx, script, pidFile)
		done <- err
	}()

	pid := readPIDEventually(t, pidFile)
	cancel()

	assertCommandCanceled(t, done, pid)
}

func TestExecRunnerKillsSurvivingChildAfterParentHandlesTerm(t *testing.T) {
	withCommandWaitDelay(t, 100*time.Millisecond)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "parent-exits-child-ignores")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsh -c 'trap \"\" TERM; while :; do sleep 1; done' &\necho $! > \"$1\"\ntrap 'exit 0' TERM\nwait\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ExecRunner{}.Run(ctx, script, pidFile)
		done <- err
	}()

	pid := readPIDEventually(t, pidFile)
	cancel()

	assertCommandCanceled(t, done, pid)
}

func withCommandWaitDelay(t *testing.T, delay time.Duration) {
	t.Helper()

	previous := commandWaitDelay
	commandWaitDelay = delay
	t.Cleanup(func() { commandWaitDelay = previous })
}

func assertCommandCanceled(t *testing.T, done <-chan error, pid int) {
	t.Helper()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation error, got %v", err)
		}
		if HasCleanupFailure(err) {
			t.Fatalf("unexpected cleanup failure: %v", err)
		}
	case <-time.After(12 * time.Second):
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("command did not finish after cancellation")
	}
	if processExists(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("process %d still exists after command cancellation", pid)
	}
}

func readPIDEventually(t *testing.T, path string) int {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		pidBytes, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			if parseErr == nil {
				return pid
			}
		}
		select {
		case <-deadline:
			t.Fatalf("pid file %s was not written", path)
		case <-ticker.C:
		}
	}
}

func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
