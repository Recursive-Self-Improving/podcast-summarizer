package commandrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

type ExecRunner struct{}

var (
	commandWaitDelay   = 5 * time.Second
	errCommandWaitDone = errors.New("command wait timed out")
	errCommandCleanup  = errors.New("command cleanup failed")
)

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return canceledResult(err), fmt.Errorf("run %s: %w", name, err)
	}

	cmd := exec.Command(name, args...)
	cmd.WaitDelay = commandWaitDelay
	configureCommand(cmd)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start %s: %w", name, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		select {
		case err = <-done:
		default:
			err = errors.Join(ctx.Err(), terminateProcessGroup(cmd, done))
		}
	}

	if errors.Is(err, errCommandWaitDone) {
		return canceledResult(ctx.Err()), fmt.Errorf("run %s: %w", name, err)
	}

	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
	}
	if err != nil {
		return result, fmt.Errorf("run %s: %w%s", name, err, outputSummary(result))
	}
	return result, nil
}

func canceledResult(err error) Result {
	return Result{ExitCode: 0, TimedOut: errors.Is(err, context.DeadlineExceeded)}
}

func HasCleanupFailure(err error) bool {
	return errors.Is(err, errCommandCleanup)
}

func MarkCleanupFailure(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errCommandCleanup, err)
}

func waitForCommandDone(done <-chan error) error {
	timer := time.NewTimer(commandWaitDelay)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		return MarkCleanupFailure(fmt.Errorf("%w after %s", errCommandWaitDone, commandWaitDelay))
	}
}

func outputSummary(result Result) string {
	parts := []string{}
	if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
		parts = append(parts, "stderr: "+truncate(stderr))
	}
	if stdout := strings.TrimSpace(result.Stdout); stdout != "" {
		parts = append(parts, "stdout: "+truncate(stdout))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

func truncate(value string) string {
	const maxOutput = 1000
	if len(value) <= maxOutput {
		return value
	}
	return value[:maxOutput] + "..."
}

type FakeRunner struct {
	Calls   []Call
	Results []FakeResult
}

type Call struct {
	Name string
	Args []string
}

type FakeResult struct {
	Result Result
	Err    error
}

func (f *FakeRunner) Run(_ context.Context, name string, args ...string) (Result, error) {
	copiedArgs := append([]string(nil), args...)
	f.Calls = append(f.Calls, Call{Name: name, Args: copiedArgs})
	if len(f.Results) == 0 {
		return Result{}, nil
	}
	result := f.Results[0]
	f.Results = f.Results[1:]
	return result.Result, result.Err
}
