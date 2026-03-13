package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/foestauf/devlog/internal/ipc"
)

// EnsureBroker checks whether the broker is reachable on its socket.
// If not, it starts the broker as a detached background process and waits
// for the socket to become available.
func EnsureBroker(dataDir string) error {
	socketPath := filepath.Join(dataDir, "devlog.sock")

	// If the socket is already live, we're golden.
	if ipc.IsAvailable(socketPath) {
		return nil
	}

	// Start the broker using the same binary with the "broker" sub-command.
	brokerCmd := exec.Command(os.Args[0], "broker")
	brokerCmd.Stdout = nil
	brokerCmd.Stderr = nil
	setProcAttr(brokerCmd)

	if err := brokerCmd.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	// Release the child so it outlives us.
	_ = brokerCmd.Process.Release()

	// Poll for the socket to appear, up to 2 seconds.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ipc.IsAvailable(socketPath) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	// One last attempt.
	if ipc.IsAvailable(socketPath) {
		return nil
	}

	return fmt.Errorf("broker did not start within 2 seconds")
}

// IsSocketAvailable checks whether the broker is reachable at socketPath.
func IsSocketAvailable(socketPath string) bool {
	return ipc.IsAvailable(socketPath)
}
