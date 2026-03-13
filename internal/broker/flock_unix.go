//go:build !windows

package broker

import (
	"fmt"
	"os"
	"syscall"
)

// acquireFlock opens the file at path and acquires an exclusive non-blocking
// flock. Returns the open file (which must stay open for the lock to hold).
// Returns an error if another process already holds the lock.
func acquireFlock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("broker: pid file locked — another broker may be running: %w", err)
	}

	return f, nil
}

// releaseFlock releases the flock and closes the file.
func releaseFlock(f *os.File) {
	if f == nil {
		return
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}
