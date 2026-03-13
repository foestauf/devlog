package broker

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFlockPreventsDuplicateBroker(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "devlog.pid")

	// First lock should succeed.
	f1, err := acquireFlock(pidPath)
	if err != nil {
		t.Fatalf("first flock: %v", err)
	}
	defer releaseFlock(f1)

	// Second lock on the same path should fail.
	_, err = acquireFlock(pidPath)
	if err == nil {
		t.Fatal("expected second flock to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Errorf("expected 'locked' in error, got: %v", err)
	}
}

func TestFlockReleasedAfterRelease(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "devlog.pid")

	f1, err := acquireFlock(pidPath)
	if err != nil {
		t.Fatalf("first flock: %v", err)
	}
	releaseFlock(f1)

	// Should be able to acquire again after release.
	f2, err := acquireFlock(pidPath)
	if err != nil {
		t.Fatalf("second flock after release: %v", err)
	}
	releaseFlock(f2)
}
