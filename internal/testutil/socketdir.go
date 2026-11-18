package testutil

import (
	"os"
	"runtime"
	"testing"
)

// SocketDir returns a temp dir short enough to hold a Unix socket; macOS's
// default TMPDIR pushes paths past the 104-byte sun_path limit.
func SocketDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "devlog-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
