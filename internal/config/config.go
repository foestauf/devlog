package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all DevLog configuration.
type Config struct {
	DataDir      string          `yaml:"data_dir"`
	Retention    RetentionConfig `yaml:"retention"`
	DetectLevels bool            `yaml:"detect_levels"`
}

// RetentionConfig controls automatic log pruning.
type RetentionConfig struct {
	MaxAgeDays int `yaml:"max_age_days"`
	IntervalH  int `yaml:"interval_hours"`
}

// defaults returns a Config with sensible defaults.
func defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir:      filepath.Join(home, ".devlog"),
		DetectLevels: true,
		Retention: RetentionConfig{
			MaxAgeDays: 30,
			IntervalH:  1,
		},
	}
}

// Load resolves configuration using the priority order:
//  1. DEVLOG_DATA_DIR env var (data_dir only)
//  2. flagDataDir from --data-dir flag
//  3. .devlog.yaml in cwd (project-level)
//  4. ~/.devlog/config.yaml (user-level)
//  5. Built-in defaults
func Load(flagDataDir string) (*Config, error) {
	cfg := defaults()

	// Layer 4: user-level config (~/.devlog/config.yaml).
	home, _ := os.UserHomeDir()
	userCfgPath := filepath.Join(home, ".devlog", "config.yaml")
	if err := mergeFromFile(&cfg, userCfgPath); err != nil {
		return nil, err
	}

	// Layer 3: project-level config (.devlog.yaml in cwd).
	cwd, _ := os.Getwd()
	projectCfgPath := filepath.Join(cwd, ".devlog.yaml")
	if err := mergeFromFile(&cfg, projectCfgPath); err != nil {
		return nil, err
	}

	// Layer 2: --data-dir flag.
	if flagDataDir != "" {
		cfg.DataDir = flagDataDir
	}

	// Layer 1: DEVLOG_DATA_DIR env var (highest priority).
	if envDir := os.Getenv("DEVLOG_DATA_DIR"); envDir != "" {
		cfg.DataDir = envDir
	}

	// Ensure data dir exists.
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// mergeFromFile reads a YAML config file and merges non-zero values into cfg.
// Missing files are silently ignored.
func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return err
	}

	if fileCfg.DataDir != "" {
		cfg.DataDir = fileCfg.DataDir
	}
	if fileCfg.Retention.MaxAgeDays != 0 {
		cfg.Retention.MaxAgeDays = fileCfg.Retention.MaxAgeDays
	}
	if fileCfg.Retention.IntervalH != 0 {
		cfg.Retention.IntervalH = fileCfg.Retention.IntervalH
	}
	// DetectLevels: only override if explicitly set to false in file.
	// Since Go zero-value for bool is false, we need the yaml to actually
	// contain "detect_levels: false" — but we can't distinguish "absent"
	// from "false" with a plain bool. Use a pointer trick in a file-only struct.
	type fileCfgBool struct {
		DetectLevels *bool `yaml:"detect_levels"`
	}
	var fb fileCfgBool
	yaml.Unmarshal(data, &fb)
	if fb.DetectLevels != nil {
		cfg.DetectLevels = *fb.DetectLevels
	}

	return nil
}
