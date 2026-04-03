package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Platform  string    `json:"platform"`
}

type StatusResponse struct {
	State       string `json:"state"`
	CurrentTask string `json:"current_task"`
	Model       string `json:"model"`
	Uptime      string `json:"uptime"`
	Version     string `json:"version"`
}

type RuntimeConfig struct {
	Model                         string `json:"defaultOpenAIModel"`
	MaxAgentIterations            int    `json:"maxAgentIterations"`
	EnableMultiAgentOrchestration bool   `json:"enableMultiAgentOrchestration"`
	MaxParallelAgents             int    `json:"maxParallelAgents"`
	WorkerMaxIterations           int    `json:"workerMaxIterations"`
	Port                          int    `json:"runtimePort"`
}

type CredentialBundle struct {
	AnthropicAPIKey  string `json:"anthropic_api_key,omitempty"`
	OpenAIAPIKey     string `json:"openai_api_key,omitempty"`
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`
	DiscordBotToken  string `json:"discord_bot_token,omitempty"`
	SlackBotToken    string `json:"slack_bot_token,omitempty"`
	BraveAPIKey      string `json:"brave_api_key,omitempty"`
	FinnhubAPIKey    string `json:"finnhub_api_key,omitempty"`
}

type PermissionBundle struct {
	Files    bool `json:"files"`
	Terminal bool `json:"terminal"`
	Browser  bool `json:"browser"`
}

type LogEntry struct {
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Timestamp string            `json:"timestamp"`
	Fields    map[string]string `json:"fields"`
}

type SSEEvent struct {
	Type           string `json:"type"`
	Content        string `json:"content"`
	Role           string `json:"role"`
	ConversationID string `json:"conversationID"`
	Error          string `json:"error"`
	Status         string `json:"status"`
	ToolName       string `json:"toolName"`
	ApprovalID     string `json:"approvalID"`
}

// BaseURL returns the daemon base URL (used in error messages).
func (c *Client) BaseURL() string { return c.baseURL }

func New(baseURL string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
		},
	}
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// No Origin header — localhost bypasses auth
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) doJSON(method, path string, body any, out any) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = string(data)
		}
		return &APIError{Code: resp.StatusCode, Message: msg}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) HealthCheck() error {
	var out any
	return c.doJSON("GET", "/status", nil, &out)
}

func (c *Client) Login(_ string) error {
	// Localhost requests bypass auth — no login needed.
	return c.HealthCheck()
}

func (c *Client) Logout() error { return nil }

// SendMessage posts a message. Returns the conversation ID and assistant response text.
func (c *Client) SendMessage(text string, convID string) (string, string, error) {
	body := map[string]any{
		"message":        text,
		"conversationId": convID,
		"platform":       "tui",
	}
	var out struct {
		Conversation struct {
			ID       string `json:"id"`
			Messages []struct {
				ID        string `json:"id"`
				Role      string `json:"role"`
				Content   string `json:"content"`
				Timestamp string `json:"timestamp"`
			} `json:"messages"`
		} `json:"conversation"`
		Response struct {
			AssistantMessage string `json:"assistantMessage"`
			Status           string `json:"status"`
			ErrorMessage     string `json:"errorMessage"`
		} `json:"response"`
	}
	// Use a client with a generous timeout — AI responses can take a while.
	jar := c.http.Jar
	jarClient := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Minute,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", c.baseURL+"/message", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := jarClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &errBody)
		return "", "", &APIError{Code: resp.StatusCode, Message: errBody.Error}
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", "", err
	}
	if out.Response.Status == "error" {
		return "", "", fmt.Errorf("%s", out.Response.ErrorMessage)
	}
	return out.Conversation.ID, out.Response.AssistantMessage, nil
}

// OpenSSEStream opens a raw SSE HTTP connection and returns the response + scanner.
// The caller must call cancel() when done to release the context and unblock the scanner.
func (c *Client) OpenSSEStream(ctx context.Context, convID string) (*http.Response, *bufio.Scanner, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/message/stream?conversationID="+convID, nil)
	if err != nil {
		return nil, nil, err
	}
	// No timeout for SSE — the context provides cancellation.
	sseClient := &http.Client{Jar: c.http.Jar}
	resp, err := sseClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	scanner := bufio.NewScanner(resp.Body)
	return resp, scanner, nil
}

// StreamSSE opens an SSE stream and calls onEvent for each event until done/error.
func (c *Client) StreamSSE(convID string, onEvent func(SSEEvent)) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, scanner, err := c.OpenSSEStream(ctx, convID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event SSEEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		onEvent(event)
		if event.Type == "done" || event.Type == "error" {
			return nil
		}
	}
	return scanner.Err()
}

func (c *Client) GetHistory(limit int) ([]Message, error) {
	// Get the most recent conversation, then fetch its messages.
	var convs []struct {
		ID string `json:"id"`
	}
	if err := c.doJSON("GET", fmt.Sprintf("/conversations?limit=%d", 1), nil, &convs); err != nil {
		return nil, err
	}
	if len(convs) == 0 {
		return nil, nil
	}

	var conv struct {
		Messages []struct {
			ID        string `json:"id"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		} `json:"messages"`
	}
	if err := c.doJSON("GET", "/conversations/"+convs[0].ID, nil, &conv); err != nil {
		return nil, err
	}

	msgs := make([]Message, 0, len(conv.Messages))
	for _, m := range conv.Messages {
		var ts time.Time
		ts, _ = time.Parse(time.RFC3339Nano, m.Timestamp)
		msgs = append(msgs, Message{
			ID:        m.ID,
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: ts,
		})
	}
	// Return at most `limit` messages from the end.
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

type statusRaw struct {
	State                string `json:"state"`
	CurrentTask          string `json:"currentTask"`
	Model                string `json:"model"`
	UptimeSeconds        int    `json:"uptimeSeconds"`
	Version              string `json:"version"`
	ConversationCount    int    `json:"conversationCount"`
	TotalTokensIn        int    `json:"totalTokensIn"`
	TotalTokensOut       int    `json:"totalTokensOut"`
	PendingApprovalCount int    `json:"pendingApprovalCount"`
}

func (c *Client) GetStatus() (*StatusResponse, error) {
	var raw statusRaw
	if err := c.doJSON("GET", "/status", nil, &raw); err != nil {
		return nil, err
	}
	uptime := fmt.Sprintf("%ds", raw.UptimeSeconds)
	if raw.UptimeSeconds >= 3600 {
		h := raw.UptimeSeconds / 3600
		m := (raw.UptimeSeconds % 3600) / 60
		uptime = fmt.Sprintf("%dh %dm", h, m)
	} else if raw.UptimeSeconds >= 60 {
		uptime = fmt.Sprintf("%dm %ds", raw.UptimeSeconds/60, raw.UptimeSeconds%60)
	}
	return &StatusResponse{
		State:       raw.State,
		CurrentTask: raw.CurrentTask,
		Model:       raw.Model,
		Uptime:      uptime,
		Version:     raw.Version,
	}, nil
}

func (c *Client) GetLogs(lines int) ([]string, error) {
	var entries []LogEntry
	if err := c.doJSON("GET", fmt.Sprintf("/logs?limit=%d", lines), nil, &entries); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, e := range entries {
		ts := e.Timestamp
		if len(ts) > 19 {
			ts = ts[:19]
		}
		result = append(result, fmt.Sprintf("[%s] %s  %s", ts, strings.ToUpper(e.Level), e.Message))
	}
	return result, nil
}

func (c *Client) GetConfig() (*RuntimeConfig, error) {
	var cfg RuntimeConfig
	if err := c.doJSON("GET", "/config", nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Client) UpdateConfig(patch map[string]any) error {
	return c.doJSON("PUT", "/config", patch, nil)
}

// credKey maps CredentialBundle field names to daemon api-key IDs.
var credKey = map[string]string{
	"AnthropicAPIKey":  "anthropic",
	"OpenAIAPIKey":     "openai",
	"TelegramBotToken": "telegram",
	"DiscordBotToken":  "discord",
	"SlackBotToken":    "slack",
	"BraveAPIKey":      "braveSearch",
	"FinnhubAPIKey":    "finnhub",
}

func (c *Client) SetCredentials(creds CredentialBundle) error {
	type apiKeyReq struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	pairs := []struct{ field, value string }{
		{"AnthropicAPIKey", creds.AnthropicAPIKey},
		{"OpenAIAPIKey", creds.OpenAIAPIKey},
		{"TelegramBotToken", creds.TelegramBotToken},
		{"DiscordBotToken", creds.DiscordBotToken},
		{"SlackBotToken", creds.SlackBotToken},
		{"BraveAPIKey", creds.BraveAPIKey},
		{"FinnhubAPIKey", creds.FinnhubAPIKey},
	}

	for _, p := range pairs {
		if p.value == "" {
			continue
		}
		key, ok := credKey[p.field]
		if !ok {
			continue
		}
		if err := c.doJSON("POST", "/api-keys", apiKeyReq{Key: key, Value: p.value}, nil); err != nil {
			return err
		}
	}
	return nil
}

// SetPermissions is not yet implemented — permissions selected during onboarding
// are not persisted to the daemon. TODO: POST to /config when the runtime supports it.
func (c *Client) SetPermissions(_ PermissionBundle) error {
	return nil
}
