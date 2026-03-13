//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// pipePath converts a socket-style path to a Windows named pipe path.
func pipePath(socketPath string) string {
	// Use the socket filename as the pipe name for simplicity.
	return `\\.\pipe\devlog`
}

// Listen creates a Windows named pipe listener.
func Listen(socketPath string) (net.Listener, error) {
	return winio.ListenPipe(pipePath(socketPath), nil)
}

// Dial connects to a Windows named pipe with a timeout.
func Dial(socketPath string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(pipePath(socketPath), &timeout)
}

// IsAvailable checks whether the named pipe is connectable.
func IsAvailable(socketPath string) bool {
	conn, err := winio.DialPipe(pipePath(socketPath), nil)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Cleanup is a no-op on Windows; named pipes are cleaned up by the OS.
func Cleanup(socketPath string) {}
