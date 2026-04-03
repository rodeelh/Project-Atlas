package comms

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// validateTelegram calls the Telegram Bot API getMe endpoint to verify the token.
// Returns (connected, botUsername, error).
func validateTelegram(token string) (bool, *string, error) {
	resp, err := httpClient.Get("https://api.telegram.org/bot" + token + "/getMe")
	if err != nil {
		return false, nil, fmt.Errorf("telegram API unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, nil, fmt.Errorf("telegram API: invalid response: %w", err)
	}
	if !result.OK {
		return false, nil, fmt.Errorf("telegram API error: %s", result.Description)
	}
	username := "@" + result.Result.Username
	return true, &username, nil
}

// validateDiscord calls the Discord API to verify the bot token.
// Returns (connected, botName, error).
func validateDiscord(token string) (bool, *string, error) {
	req, err := http.NewRequest("GET", "https://discord.com/api/v10/users/@me", nil)
	if err != nil {
		return false, nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("discord API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return false, nil, fmt.Errorf("discord: invalid bot token")
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil, fmt.Errorf("discord API returned status %d", resp.StatusCode)
	}

	var result struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, nil, fmt.Errorf("discord API: invalid response: %w", err)
	}
	name := result.Username
	return true, &name, nil
}

// validateSlack calls the Slack auth.test API to verify the bot token.
// Returns (connected, workspaceName, error).
func validateSlack(botToken string) (bool, *string, error) {
	req, err := http.NewRequest("GET", "https://slack.com/api/auth.test", nil)
	if err != nil {
		return false, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("slack API unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Team  string `json:"team"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, nil, fmt.Errorf("slack API: invalid response: %w", err)
	}
	if !result.OK {
		return false, nil, fmt.Errorf("slack API error: %s", result.Error)
	}
	team := result.Team
	return true, &team, nil
}
