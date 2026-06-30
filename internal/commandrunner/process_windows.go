//go:build windows

package commandrunner

import (
	"errors"
	"os"
	"os/exec"
)

func configureCommand(_ *exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd, done <-chan error) error {
	var killErr error
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			killErr = err
		}
	}
	return errors.Join(MarkCleanupFailure(killErr), waitForCommandDone(done))
}
