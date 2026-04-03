// Package creds provides shared credential reading and writing via the macOS Keychain.
package creds

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Bundle holds all API credentials from the shared Keychain bundle.
type Bundle struct {
	OpenAIAPIKey      string            `json:"openAIAPIKey"`
	AnthropicAPIKey   string            `json:"anthropicAPIKey"`
	GeminiAPIKey      string            `json:"geminiAPIKey"`
	LMStudioAPIKey    string            `json:"lmStudioAPIKey"`
	BraveSearchAPIKey string            `json:"braveSearchAPIKey"`
	FinnhubAPIKey     string            `json:"finnhubAPIKey"`
	TelegramBotToken  string            `json:"telegramBotToken"`
	DiscordBotToken   string            `json:"discordBotToken"`
	SlackBotToken     string            `json:"slackBotToken"`
	SlackAppToken     string            `json:"slackAppToken"`
	CustomSecrets     map[string]string `json:"customSecrets,omitempty"`
}

// CustomSecret returns a custom key value by name, or "" if not found.
// Skills use this to look up Forge-installed or user-defined API keys.
func (b Bundle) CustomSecret(name string) string {
	return b.CustomSecrets[name]
}

const (
	bundleService = "com.projectatlas.credentials"
	bundleAccount = "bundle"
)

// mu serialises all read-modify-write Keychain operations.
// Reads (Read) do not hold the mutex — only Store/DeleteCustomKey do.
var mu sync.Mutex

// Read reads the credential bundle from the macOS Keychain via security CLI.
// Returns an empty Bundle (not an error) if the key is absent.
func Read() (Bundle, error) {
	out, err := execSecurity("find-generic-password",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w",
	)
	if err != nil {
		// Key not present is not an error at this level.
		return Bundle{}, nil
	}

	var b Bundle
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &b); err != nil {
		return Bundle{}, nil
	}
	return b, nil
}

// Store writes a single credential field into the Keychain bundle.
// provider values match the web UI's providerID strings.
// For custom/forge keys, provider is "custom" and name is the key name.
// This is the ONLY write path for credentials — never write partial structs.
func Store(provider, key, name string) error {
	mu.Lock()
	defer mu.Unlock()

	m, ok := readRaw()
	if !ok {
		// Bundle couldn't be read. Check whether the item exists at all.
		exists, existsErr := itemExists()
		if existsErr != nil || exists {
			return fmt.Errorf("credential bundle could not be read from Keychain — open Keychain Access and grant Atlas permission, then try again")
		}
		m = map[string]interface{}{} // first-time setup: start fresh
	}

	switch provider {
	case "openai":
		m["openAIAPIKey"] = key
	case "anthropic":
		m["anthropicAPIKey"] = key
	case "gemini":
		m["geminiAPIKey"] = key
	case "lm_studio":
		m["lmStudioAPIKey"] = key
	case "telegram":
		m["telegramBotToken"] = key
	case "discord":
		m["discordBotToken"] = key
	case "slack", "slackBot": // web UI sends "slackBot"
		m["slackBotToken"] = key
	case "slackApp":
		m["slackAppToken"] = key
	case "brave", "braveSearch":
		m["braveSearchAPIKey"] = key
	case "finnhub":
		m["finnhubAPIKey"] = key
	default:
		// Custom key — stored under customSecrets[name].
		keyName := name
		if keyName == "" {
			keyName = provider
		}
		customs, _ := m["customSecrets"].(map[string]interface{})
		if customs == nil {
			customs = map[string]interface{}{}
		}
		customs[keyName] = key
		m["customSecrets"] = customs
	}

	return writeRaw(m)
}

// DeleteCustomKey removes a custom key from the bundle's customSecrets map.
func DeleteCustomKey(name string) error {
	mu.Lock()
	defer mu.Unlock()

	m, ok := readRaw()
	if !ok {
		return fmt.Errorf("credential bundle could not be read from Keychain")
	}
	customs, _ := m["customSecrets"].(map[string]interface{})
	if customs != nil {
		delete(customs, name)
		m["customSecrets"] = customs
	}
	return writeRaw(m)
}

// MigrateCustomKeys moves keys that were previously stored under customSecrets
// (because of a provider-ID mismatch) into their proper top-level fields.
// Safe to call every startup — no-ops when already clean.
func MigrateCustomKeys() {
	mu.Lock()
	defer mu.Unlock()

	m, ok := readRaw()
	if !ok {
		return
	}
	customs, _ := m["customSecrets"].(map[string]interface{})
	if customs == nil {
		return
	}
	changed := false

	// braveSearch → braveSearchAPIKey
	if v, ok := customs["braveSearch"].(string); ok && v != "" {
		if existing, _ := m["braveSearchAPIKey"].(string); existing == "" {
			m["braveSearchAPIKey"] = v
			delete(customs, "braveSearch")
			changed = true
		}
	}
	// finnhub → finnhubAPIKey
	if v, ok := customs["finnhub"].(string); ok && v != "" {
		if existing, _ := m["finnhubAPIKey"].(string); existing == "" {
			m["finnhubAPIKey"] = v
			delete(customs, "finnhub")
			changed = true
		}
	}
	// slackBot → slackBotToken (web UI provider ID mismatch)
	if v, ok := customs["slackBot"].(string); ok && v != "" {
		if existing, _ := m["slackBotToken"].(string); existing == "" {
			m["slackBotToken"] = v
			delete(customs, "slackBot")
			changed = true
		}
	}

	if changed {
		m["customSecrets"] = customs
		_ = writeRaw(m)
	}
}

// ── internal helpers ──────────────────────────────────────────────────────────

// readRaw reads the bundle as a generic map so we can update individual fields
// without losing unrecognised keys. Returns ok=false when the item can't be read.
// Callers must hold mu when using this as part of a read-modify-write.
func readRaw() (map[string]interface{}, bool) {
	out, err := execSecurity("find-generic-password",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w",
	)
	if err != nil {
		return map[string]interface{}{}, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		return map[string]interface{}{}, false
	}
	return m, true
}

// writeRaw serialises the map and stores it in the Keychain.
// Callers must hold mu.
func writeRaw(m map[string]interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	_, err = execSecurity(
		"add-generic-password",
		"-U",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w", string(data),
	)
	return err
}

// itemExists returns true if the Keychain item exists (exit 0),
// false if not found (exit 44), and an error for any other failure.
func itemExists() (bool, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", bundleService, "-a", bundleAccount)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 44 {
			return false, nil
		}
	}
	return false, err
}

// execSecurity runs the macOS `security` CLI with the given arguments and
// returns stdout.
func execSecurity(args ...string) (string, error) {
	cmd := exec.Command("security", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("security %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
