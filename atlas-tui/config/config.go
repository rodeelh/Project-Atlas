package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL        string `json:"base_url"`
	Token          string `json:"token"`
	OnboardingDone bool   `json:"onboarding_done"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "atlas-tui", "config.json"), nil
}

func Load() (*Config, error) {
	cfg := &Config{BaseURL: "http://localhost:1984"}

	path, err := configPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	// Env overrides
	if v := os.Getenv("ATLAS_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("ATLAS_TOKEN"); v != "" {
		cfg.Token = v
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:1984"
	}

	return cfg, nil
}

func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".atlas-config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
