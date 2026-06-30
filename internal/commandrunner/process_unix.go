//go:build unix

package commandrunner

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return waitForCommandDone(done)
	}

	pgid := -cmd.Process.Pid
	select {
	case err := <-done:
		return err
	default:
	}
	signalErr := signalProcessGroup(pgid, syscall.SIGTERM)
	timer := time.NewTimer(commandWaitDelay)
	defer timer.Stop()

	select {
	case err := <-done:
		if !waitForProcessGroupExit(pgid) {
			signalErr = errors.Join(signalErr, signalProcessGroup(pgid, syscall.SIGKILL))
			if !waitForProcessGroupExit(pgid) {
				signalErr = errors.Join(signalErr, fmt.Errorf("process group %d still exists after SIGKILL", -pgid))
			}
		}
		return errors.Join(err, MarkCleanupFailure(signalErr))
	case <-timer.C:
		signalErr = errors.Join(signalErr, signalProcessGroup(pgid, syscall.SIGKILL))
		err := waitForCommandDone(done)
		if !waitForProcessGroupExit(pgid) {
			signalErr = errors.Join(signalErr, fmt.Errorf("process group %d still exists after SIGKILL", -pgid))
		}
		return errors.Join(err, MarkCleanupFailure(signalErr))
	}
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if err := syscall.Kill(pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process group %d with %s: %w", -pgid, signal, err)
	}
	return nil
}

func waitForProcessGroupExit(pgid int) bool {
	deadline := time.After(commandWaitDelay)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := syscall.Kill(pgid, 0); err != nil {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}
