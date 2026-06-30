//go:build !linux

package transcribe

func DefaultOrphanWaiter() OrphanWaiter {
	return NoopOrphanWaiter{}
}

func (w PythonProcessWaiter) hasActiveProcess() (bool, error) {
	return false, nil
}
