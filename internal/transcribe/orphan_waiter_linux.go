//go:build linux

package transcribe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func DefaultOrphanWaiter() OrphanWaiter {
	return PythonProcessWaiter{ProcRoot: "/proc"}
}

func (w PythonProcessWaiter) hasActiveProcess() (bool, error) {
	procRoot := w.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false, fmt.Errorf("list processes: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isPID(entry.Name()) {
			continue
		}
		args, err := readProcessArgs(filepath.Join(procRoot, entry.Name(), "cmdline"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return false, fmt.Errorf("read process %s cmdline: %w", entry.Name(), err)
		}
		if isMatchingPythonProcess(args) {
			return true, nil
		}
	}
	return false, nil
}

func isPID(name string) bool {
	_, err := strconv.Atoi(name)
	return err == nil
}

func readProcessArgs(path string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(string(contents), "\x00")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\x00"), nil
}
