//go:build !windows

package aula

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockDevice takes an exclusive lock shared by all f108-pro processes, so a
// command never interleaves its reports with another one's (the watch
// service sends realtime frames every few seconds). It waits for the lock.
func lockDevice() (unlock func(), err error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.OpenFile(filepath.Join(dir, "f108-pro.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("device lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("device lock: %w", err)
	}
	return func() { f.Close() }, nil
}
