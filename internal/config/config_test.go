package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.DetectLevels != true {
		t.Error("expected detect_levels default true")
	}
	if cfg.Retention.MaxAgeDays != 30 {
		t.Errorf("expected max_age_days 30, got %d", cfg.Retention.MaxAgeDays)
	}
	if cfg.Retention.IntervalH != 1 {
		t.Errorf("expected interval_hours 1, got %d", cfg.Retention.IntervalH)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_DATA_DIR", tmp)

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != tmp {
		t.Errorf("expected data dir %s, got %s", tmp, cfg.DataDir)
	}
}

func TestLoadFlagOverride(t *testing.T) {
	tmp := t.TempDir()
	// Clear env so it doesn't interfere.
	t.Setenv("DEVLOG_DATA_DIR", "")

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != tmp {
		t.Errorf("expected data dir %s, got %s", tmp, cfg.DataDir)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_DATA_DIR", "")

	// Write a project-level config in a temp dir and chdir there.
	cfgContent := []byte("retention:\n  max_age_days: 7\n")
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(tmp, ".devlog.yaml"), cfgContent, 0644)

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retention.MaxAgeDays != 7 {
		t.Errorf("expected max_age_days 7, got %d", cfg.Retention.MaxAgeDays)
	}
	// Other defaults should be preserved.
	if cfg.Retention.IntervalH != 1 {
		t.Errorf("expected interval_hours 1, got %d", cfg.Retention.IntervalH)
	}
}

func TestEnvOverridesFlag(t *testing.T) {
	envDir := t.TempDir()
	flagDir := t.TempDir()
	t.Setenv("DEVLOG_DATA_DIR", envDir)

	cfg, err := Load(flagDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != envDir {
		t.Errorf("env should override flag: expected %s, got %s", envDir, cfg.DataDir)
	}
}
