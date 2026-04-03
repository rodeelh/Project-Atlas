package onboarding

// AIProvider describes an AI model provider.
type AIProvider struct {
	ID      string
	Name    string
	CredKey string
	Prompt  string
}

// ChatProvider describes a chat/messaging provider.
type ChatProvider struct {
	ID      string
	Name    string
	CredKey string
	Prompt  string
}

// SkillProvider describes an optional skill with its own credential.
type SkillProvider struct {
	ID          string
	Name        string
	Description string
	CredKey     string
	Prompt      string
}

// Permission describes a system permission Atlas can request.
type Permission struct {
	ID          string
	Name        string
	Description string
	Dangerous   bool
}

var AIProviders = []AIProvider{
	{"anthropic", "Anthropic", "AnthropicAPIKey", "drop your Anthropic API key."},
	{"openai", "OpenAI", "OpenAIAPIKey", "drop your OpenAI API key."},
}

var ChatProviders = []ChatProvider{
	{"telegram", "Telegram", "TelegramBotToken", "your Telegram bot token."},
	{"discord", "Discord", "DiscordBotToken", "your Discord bot token."},
	{"slack", "Slack", "SlackBotToken", "your Slack bot token."},
}

var SkillProviders = []SkillProvider{
	{"brave", "Brave Search", "lets me search the web", "BraveAPIKey", "your Brave Search API key."},
	{"finnhub", "Finnhub", "lets me track markets and stocks", "FinnhubAPIKey", "your Finnhub API key."},
}

var Permissions = []Permission{
	{"files", "Files", "read and write to your filesystem", false},
	{"terminal", "Terminal", "run commands on your machine", true},
	{"browser", "Browser", "browse the web on your behalf", false},
}
