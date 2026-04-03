package comms

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// credBundle mirrors the subset of AtlasCredentialBundle needed for communications.
// The JSON keys match the Swift Codable property names exactly.
type credBundle struct {
	OpenAIAPIKey     *string           `json:"openAIAPIKey"`
	TelegramBotToken *string           `json:"telegramBotToken"`
	DiscordBotToken  *string           `json:"discordBotToken"`
	SlackBotToken    *string           `json:"slackBotToken"`
	SlackAppToken    *string           `json:"slackAppToken"`
	CustomSecrets    map[string]string `json:"customSecrets,omitempty"`
}

const (
	bundleService = "com.projectatlas.credentials"
	bundleAccount = "bundle"
)

// readBundle fetches the credential bundle from the Keychain using the security CLI.
// Returns an empty bundle on any error (missing item, parse failure, etc.).
func readBundle() credBundle {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w",
	).Output()
	if err != nil {
		return credBundle{}
	}
	raw := strings.TrimSpace(string(out))
	var b credBundle
	json.Unmarshal([]byte(raw), &b) //nolint:errcheck — empty bundle on failure
	return b
}

// writeBundle persists comms credentials back to the Keychain using a
// read-modify-write on the raw JSON map so that non-comms fields
// (anthropicAPIKey, geminiAPIKey, lmStudioAPIKey, braveSearchAPIKey,
// finnhubAPIKey, custom keys, etc.) are never overwritten or lost.
func writeBundle(b credBundle) {
	// Read the existing full bundle as a generic map to preserve every field.
	var m map[string]interface{}
	if out, err := exec.Command(
		"security", "find-generic-password",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w",
	).Output(); err == nil {
		json.Unmarshal([]byte(strings.TrimSpace(string(out))), &m) //nolint:errcheck
	}
	if m == nil {
		m = map[string]interface{}{}
	}

	// Update only the fields managed by the comms package — leave everything else.
	setIfSet := func(key string, ptr *string) {
		if ptr != nil {
			m[key] = *ptr
		}
	}
	setIfSet("openAIAPIKey", b.OpenAIAPIKey)
	setIfSet("telegramBotToken", b.TelegramBotToken)
	setIfSet("discordBotToken", b.DiscordBotToken)
	setIfSet("slackBotToken", b.SlackBotToken)
	setIfSet("slackAppToken", b.SlackAppToken)
	if b.CustomSecrets != nil {
		m["customSecrets"] = b.CustomSecrets
	}

	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	exec.Command( //nolint:errcheck
		"security", "add-generic-password",
		"-U",
		"-s", bundleService,
		"-a", bundleAccount,
		"-w", string(data),
	).Run()
}
