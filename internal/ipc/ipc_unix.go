//go:build !windows

package ipc

import (
	"net"
	"os"
	"time"
)

// Listen creates a Unix domain socket listener at the given path.
// Removes any stale socket file before binding.
func Listen(socketPath string) (net.Listener, error) {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", socketPath)
}

// Dial connects to a Unix domain socket with a timeout.
func Dial(socketPath string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath, timeout)
}

// IsAvailable checks whether a Unix socket at the given path is connectable.
func IsAvailable(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Cleanup removes the socket file. Called during shutdown.
func Cleanup(socketPath string) {
	os.Remove(socketPath)
}
