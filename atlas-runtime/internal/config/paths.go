package config

import (
	"os"
	"path/filepath"
)

// SupportDir returns ~/Library/Application Support/ProjectAtlas — the same
// directory used by the Swift runtime and its macOS app.
func SupportDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "ProjectAtlas")
}

// ConfigPath returns the canonical config file path.
// Matches DefaultPathProvider.configFileURL() in StorageInterfaces.swift.
func ConfigPath() string {
	return filepath.Join(SupportDir(), "config.json")
}

// LegacyConfigPath returns the old config path for migration compatibility.
func LegacyConfigPath() string {
	return filepath.Join(SupportDir(), "atlas-config.json")
}

// DBPath returns the SQLite database path.
// Matches MemoryStore's database path in the Swift runtime.
func DBPath() string {
	return filepath.Join(SupportDir(), "atlas.sqlite3")
}

// AtlasInstallDir returns the directory where the Atlas runtime binary,
// web assets, and the bundled engine (llama-server) are installed.
// Distinct from SupportDir() which holds user data (SQLite, config, MIND.md…).
func AtlasInstallDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Atlas")
}

// ModelsDir returns the directory where Engine LM model files are stored.
// Lives under SupportDir() so models are preserved across uninstalls.
func ModelsDir() string {
	return filepath.Join(SupportDir(), "models")
}
