package features

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"atlas-runtime-go/internal/customskills"
	"atlas-runtime-go/internal/logstore"
)

// SkillAction matches the web UI's SkillRecord.actions element shape.
type SkillAction struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permissionLevel"`
	ApprovalPolicy  string `json:"approvalPolicy"`
	IsEnabled       bool   `json:"isEnabled"`
}

// SkillManifestInfo matches the web UI's SkillRecord.manifest shape.
type SkillManifestInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Description    string   `json:"description"`
	LifecycleState string   `json:"lifecycleState"`
	RiskLevel      string   `json:"riskLevel"`
	IsUserVisible  bool     `json:"isUserVisible"`
	Category       string   `json:"category,omitempty"`
	Source         string   `json:"source,omitempty"`
	Capabilities   []string `json:"capabilities"`
	Tags           []string `json:"tags"`
}

// SkillRecord matches the web UI SkillRecord interface.
type SkillRecord struct {
	Manifest   SkillManifestInfo `json:"manifest"`
	Actions    []SkillAction     `json:"actions"`
	Validation *SkillValidation  `json:"validation,omitempty"`
}

// skillStateFile is the Go-managed overlay for skill lifecycle states.
// Path: <supportDir>/go-skill-states.json
// Format: map[skillID]lifecycleState  ("enabled" | "disabled")
type skillStates map[string]string

var skillMu sync.Mutex

func loadSkillStates(supportDir string) skillStates {
	data, err := os.ReadFile(filepath.Join(supportDir, "go-skill-states.json"))
	if err != nil {
		return skillStates{}
	}
	var s skillStates
	if err := json.Unmarshal(data, &s); err != nil {
		logstore.Write("warn", "go-skill-states.json is malformed — ignoring overrides: "+err.Error(), nil)
		return skillStates{}
	}
	return s
}

func saveSkillStates(supportDir string, s skillStates) {
	skillMu.Lock()
	defer skillMu.Unlock()
	data, err := json.Marshal(s)
	if err != nil {
		logstore.Write("warn", "saveSkillStates: marshal failed: "+err.Error(), nil)
		return
	}
	path := filepath.Join(supportDir, "go-skill-states.json")
	tmp, err := os.CreateTemp(filepath.Dir(path), "go-skill-states-*.json")
	if err != nil {
		logstore.Write("warn", "saveSkillStates: create temp failed: "+err.Error(), nil)
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		logstore.Write("warn", "saveSkillStates: write failed: "+err.Error(), nil)
		return
	}
	tmp.Close()
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		logstore.Write("warn", "saveSkillStates: rename failed: "+err.Error(), nil)
	}
}

// loadActionPolicies reads action-policies.json (written by Swift ActionPolicyStore and Go approvals domain).
func loadActionPolicies(supportDir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(supportDir, "action-policies.json"))
	if err != nil {
		return map[string]string{}
	}
	var p map[string]string
	json.Unmarshal(data, &p) //nolint:errcheck
	return p
}

// builtInSkills returns the hardcoded catalog of built-in skills.
func builtInSkills() []SkillRecord {
	return []SkillRecord{
		{
			Manifest: SkillManifestInfo{
				ID: "weather", Name: "Weather", Version: "1.0",
				Description:    "Real-time weather data and forecasts using Open-Meteo.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "information", Capabilities: []string{"weather"}, Tags: []string{"weather"},
			},
			Actions: []SkillAction{
				{ID: "weather.current", Name: "Current Weather", Description: "Current conditions for a location.", PermissionLevel: "read", IsEnabled: true},
				{ID: "weather.forecast", Name: "Forecast", Description: "Multi-day weather forecast.", PermissionLevel: "read", IsEnabled: true},
				{ID: "weather.hourly", Name: "Hourly Forecast", Description: "Hour-by-hour forecast.", PermissionLevel: "read", IsEnabled: true},
				{ID: "weather.brief", Name: "Weather Brief", Description: "Compact weather summary.", PermissionLevel: "read", IsEnabled: true},
				{ID: "weather.dayplan", Name: "Day Plan", Description: "Weather-optimised daily schedule.", PermissionLevel: "read", IsEnabled: true},
				{ID: "weather.activity_window", Name: "Activity Window", Description: "Best time window for an outdoor activity.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "web-research", Name: "Web Research", Version: "1.0",
				Description:    "Search the web, fetch pages, and research topics.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "research", Capabilities: []string{"web_search", "web_fetch"}, Tags: []string{"web", "search"},
			},
			Actions: []SkillAction{
				{ID: "web.search", Name: "Web Search", Description: "Search the web.", PermissionLevel: "read", IsEnabled: true},
				{ID: "websearch.query", Name: "Brave Search", Description: "Search the web using Brave Search API.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.fetch_page", Name: "Fetch Page", Description: "Fetch and extract content from a URL.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.research", Name: "Deep Research", Description: "Multi-source research on a topic.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.news", Name: "News", Description: "Recent news on a topic.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.check_url", Name: "Check URL", Description: "Verify a URL is reachable.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.multi_search", Name: "Multi Search", Description: "Run multiple searches in parallel.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.extract_links", Name: "Extract Links", Description: "Extract links from a page.", PermissionLevel: "read", IsEnabled: true},
				{ID: "web.summarize_url", Name: "Summarize URL", Description: "Fetch and summarize a page.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "file-system", Name: "File System", Version: "1.1",
				Description:    "Read and write files and directories from approved locations. Writes require approval and return a unified diff.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "system", Capabilities: []string{"file_read", "file_write"}, Tags: []string{"files"},
			},
			Actions: []SkillAction{
				{ID: "fs.list_directory", Name: "List Directory", Description: "List files in a directory.", PermissionLevel: "read", IsEnabled: true},
				{ID: "fs.read_file", Name: "Read File", Description: "Read contents of a file.", PermissionLevel: "read", IsEnabled: true},
				{ID: "fs.search", Name: "Search Files", Description: "Search for files by name or pattern.", PermissionLevel: "read", IsEnabled: true},
				{ID: "fs.get_metadata", Name: "File Metadata", Description: "Get metadata for a file or directory.", PermissionLevel: "read", IsEnabled: true},
				{ID: "fs.content_search", Name: "Content Search", Description: "Search file contents.", PermissionLevel: "read", IsEnabled: true},
				{ID: "fs.write_file", Name: "Write File", Description: "Create or overwrite a file with new content.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "fs.patch_file", Name: "Patch File", Description: "Apply a unified diff patch to a file.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "fs.create_directory", Name: "Create Directory", Description: "Create a directory.", PermissionLevel: "draft", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "atlas.info", Name: "Atlas Info", Version: "1.0",
				Description:    "Runtime introspection — status, version, skills catalog, and capability summary.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "system", Capabilities: []string{"runtime_info"}, Tags: []string{"atlas"},
			},
			Actions: []SkillAction{
				{ID: "atlas.info", Name: "Atlas Info", Description: "Get runtime status and configuration info.", PermissionLevel: "read", IsEnabled: true},
				{ID: "atlas.list_skills", Name: "List Skills", Description: "List all registered skill actions.", PermissionLevel: "read", IsEnabled: true},
				{ID: "atlas.capabilities", Name: "Capabilities Summary", Description: "Get a grouped summary of all Atlas capabilities.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "info", Name: "Info", Version: "1.0",
				Description:    "Time, date, timezone, and currency utilities.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "information", Capabilities: []string{"time", "currency"}, Tags: []string{"time", "date", "currency", "timezone"},
			},
			Actions: []SkillAction{
				{ID: "info.current_time", Name: "Current Time", Description: "Get the current time for a timezone or location.", PermissionLevel: "read", IsEnabled: true},
				{ID: "info.current_date", Name: "Current Date", Description: "Get the current date for a timezone or location.", PermissionLevel: "read", IsEnabled: true},
				{ID: "info.timezone_convert", Name: "Timezone Convert", Description: "Convert a time between two timezones.", PermissionLevel: "read", IsEnabled: true},
				{ID: "info.currency_for_location", Name: "Currency for Location", Description: "Look up the currency used in a country or city.", PermissionLevel: "read", IsEnabled: true},
				{ID: "info.currency_convert", Name: "Currency Convert", Description: "Convert an amount between currencies using live rates.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "system-actions", Name: "System Actions", Version: "1.0",
				Description:    "Open apps, manage clipboard, and send notifications on macOS.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "system", Capabilities: []string{"system_control"}, Tags: []string{"macos", "system"},
			},
			Actions: []SkillAction{
				{ID: "system.open_app", Name: "Open App", Description: "Open a macOS application.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.open_file", Name: "Open File", Description: "Open a file with its default app.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.open_folder", Name: "Open Folder", Description: "Open a folder in Finder.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.reveal_in_finder", Name: "Reveal in Finder", Description: "Reveal a file in Finder.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.open_file_with_app", Name: "Open With App", Description: "Open a file with a specific app.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.open_url", Name: "Open URL", Description: "Open a URL in the default browser.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.copy_to_clipboard", Name: "Copy to Clipboard", Description: "Copy text to the clipboard.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "system.read_clipboard", Name: "Read Clipboard", Description: "Read current clipboard contents.", PermissionLevel: "read", IsEnabled: true},
				{ID: "system.send_notification", Name: "Send Notification", Description: "Send a macOS notification.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "system.schedule_notification", Name: "Schedule Notification", Description: "Schedule a future notification.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "system.running_apps", Name: "Running Apps", Description: "List running applications.", PermissionLevel: "read", IsEnabled: true},
				{ID: "system.frontmost_app", Name: "Frontmost App", Description: "Get the frontmost application.", PermissionLevel: "read", IsEnabled: true},
				{ID: "system.is_app_running", Name: "Is App Running", Description: "Check if an app is running.", PermissionLevel: "read", IsEnabled: true},
				{ID: "system.activate_app", Name: "Activate App", Description: "Bring an app to the foreground.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "system.quit_app", Name: "Quit App", Description: "Quit a running application.", PermissionLevel: "execute", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "terminal-control", Name: "Terminal Control", Version: "1.0",
				Description:    "Run shell commands, inspect and manage processes, and interact with the terminal environment.",
				LifecycleState: "enabled", RiskLevel: "high", IsUserVisible: true,
				Category: "system", Capabilities: []string{"shell_exec", "process_management"}, Tags: []string{"terminal", "shell", "processes"},
			},
			Actions: []SkillAction{
				{ID: "terminal.run_command", Name: "Run Command", Description: "Run a shell command by executable name and argument list. No shell expansion — injection-safe.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "terminal.run_script", Name: "Run Script", Description: "Execute a multi-line shell script via /bin/sh. Supports pipes and redirects.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "terminal.read_env", Name: "Read Environment", Description: "Read environment variable values by name.", PermissionLevel: "read", IsEnabled: true},
				{ID: "terminal.list_processes", Name: "List Processes", Description: "List running processes, optionally filtered by name.", PermissionLevel: "read", IsEnabled: true},
				{ID: "terminal.kill_process", Name: "Kill Process", Description: "Send a signal to a process by PID.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "terminal.get_working_directory", Name: "Get Working Directory", Description: "Return the runtime's current working directory.", PermissionLevel: "read", IsEnabled: true},
				{ID: "terminal.which", Name: "Which", Description: "Locate a command on PATH.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "applescript-automation", Name: "AppleScript Automation", Version: "1.0",
				Description:    "Control Calendar, Reminders, Contacts, Mail, Safari, Notes, and Music via AppleScript.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "automation", Capabilities: []string{"apple_apps"}, Tags: []string{"applescript", "macos"},
			},
			Actions: []SkillAction{
				{ID: "applescript.calendar_read", Name: "Calendar Read", Description: "Read Calendar events.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.calendar_write", Name: "Calendar Write", Description: "Create or update Calendar events.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.calendar_list_calendars", Name: "List Calendars", Description: "List available calendars.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.reminders_read", Name: "Reminders Read", Description: "Read Reminders.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.reminders_write", Name: "Reminders Write", Description: "Create or update Reminders.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.reminders_list_lists", Name: "List Reminder Lists", Description: "List reminder lists.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.contacts_read", Name: "Contacts Read", Description: "Read Contacts.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.mail_read", Name: "Mail Read", Description: "Read Mail messages.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.mail_wait_for_message", Name: "Mail Wait For Message", Description: "Wait for a new email matching a filter.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.mail_write", Name: "Mail Write", Description: "Compose and send Mail.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.safari_read", Name: "Safari Read", Description: "Read Safari tabs and content.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.safari_navigate", Name: "Safari Navigate", Description: "Navigate Safari to a URL.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.notes_read", Name: "Notes Read", Description: "Read Notes.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.notes_write", Name: "Notes Write", Description: "Create or update Notes.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.music_read", Name: "Music Read", Description: "Read Music library and playback state.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.music_control", Name: "Music Control", Description: "Control Music playback.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "applescript.system_info", Name: "System Info", Description: "Get macOS system information.", PermissionLevel: "read", IsEnabled: true},
				{ID: "applescript.run_custom", Name: "Run Custom Script", Description: "Execute a custom AppleScript.", PermissionLevel: "execute", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "finance", Name: "Finance", Version: "1.0",
				Description:    "Real-time stock and crypto quotes, price history, and portfolio lookups via Yahoo Finance.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "information", Capabilities: []string{"finance"}, Tags: []string{"finance", "stocks", "crypto"},
			},
			Actions: []SkillAction{
				{ID: "finance.quote", Name: "Quote", Description: "Get current price and info for a stock or crypto symbol.", PermissionLevel: "read", IsEnabled: true},
				{ID: "finance.history", Name: "Price History", Description: "Get historical daily closing prices.", PermissionLevel: "read", IsEnabled: true},
				{ID: "finance.portfolio", Name: "Portfolio", Description: "Batch quote lookup for multiple symbols.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "image-generation", Name: "Image Generation", Version: "1.0",
				Description:    "Generate and edit images using OpenAI DALL-E.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: true,
				Category: "creative", Capabilities: []string{"image_generation"}, Tags: []string{"image", "dalle", "openai"},
			},
			Actions: []SkillAction{
				{ID: "image.generate", Name: "Generate Image", Description: "Generate an image from a text prompt using DALL-E 3.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "image.edit", Name: "Edit Image", Description: "Edit an existing image with a text instruction using DALL-E 2.", PermissionLevel: "execute", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "gremlin-management", Name: "Gremlin Management", Version: "1.0",
				Description:    "Full lifecycle management of Atlas automations (Gremlins) — create, update, delete, enable/disable, run, and inspect history.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "automation", Capabilities: []string{"automation_management"}, Tags: []string{"gremlins", "automations"},
			},
			Actions: []SkillAction{
				{ID: "gremlin.create", Name: "Create Automation", Description: "Create a new scheduled automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.update", Name: "Update Automation", Description: "Update an existing automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.delete", Name: "Delete Automation", Description: "Delete an automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.list", Name: "List Automations", Description: "List all automations.", PermissionLevel: "read", IsEnabled: true},
				{ID: "gremlin.get", Name: "Get Automation", Description: "Get full details of a single automation.", PermissionLevel: "read", IsEnabled: true},
				{ID: "gremlin.enable", Name: "Enable Automation", Description: "Enable a disabled automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.disable", Name: "Disable Automation", Description: "Disable a running automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.run_now", Name: "Run Now", Description: "Immediately trigger an automation.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.run_history", Name: "Run History", Description: "Show recent run history for an automation.", PermissionLevel: "read", IsEnabled: true},
				{ID: "gremlin.next_run", Name: "Next Run", Description: "Calculate the next scheduled run time.", PermissionLevel: "read", IsEnabled: true},
				{ID: "gremlin.duplicate", Name: "Duplicate Automation", Description: "Clone an automation under a new name.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "gremlin.validate_schedule", Name: "Validate Schedule", Description: "Validate a schedule string.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "forge", Name: "Forge", Version: "1.4",
				Description:    "Atlas Forge — propose new API skill integrations directly from conversation. Runs 8-gate quality validation including live API pre-validation before persisting a proposal.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "development", Capabilities: []string{"skill_forge"}, Tags: []string{"forge", "api", "integration"},
			},
			Actions: []SkillAction{
				{ID: "forge.orchestration.propose", Name: "Propose Forge Skill", Description: "Propose a new Forge skill with full quality-gate validation. For API skills, provide contract_json capturing your API research.", PermissionLevel: "draft", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "diary", Name: "Diary", Version: "1.0",
				Description:    "Atlas Diary — records notable moments per day to shape long-term memory and user experience.",
				LifecycleState: "enabled", RiskLevel: "low", IsUserVisible: false,
				Category: "memory", Capabilities: []string{"diary"}, Tags: []string{"diary", "memory", "context"},
			},
			Actions: []SkillAction{
				{ID: "diary.record", Name: "Record Entry", Description: "Record a significant moment into the daily diary.", PermissionLevel: "read", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "browser-control", Name: "Browser Control", Version: "1.1",
				Description:    "Control a real browser — navigate, screenshot, fill forms, manage multi-tab sessions, run JS, upload files, and handle 2FA. Requires Google Chrome.",
				LifecycleState: "enabled", RiskLevel: "high", IsUserVisible: true,
				Category: "automation", Capabilities: []string{"browser_automation", "web_interaction", "session_management", "multi_tab", "iframe_support"}, Tags: []string{"browser", "web", "automation"},
			},
			Actions: []SkillAction{
				// Observe
				{ID: "browser.navigate", Name: "Navigate", Description: "Navigate to a URL.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.screenshot", Name: "Screenshot", Description: "Capture the current page.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.read_page", Name: "Read Page", Description: "Extract visible text from the page.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.find_element", Name: "Find Element", Description: "Find a DOM element by CSS selector.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.scroll", Name: "Scroll", Description: "Scroll the page.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.session_check", Name: "Session Check", Description: "Check if a session exists for a host.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.wait_for_element", Name: "Wait For Element", Description: "Wait for an element to appear.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.wait_network_idle", Name: "Wait Network Idle", Description: "Wait for the page to finish loading.", PermissionLevel: "read", IsEnabled: true},
				// Tabs
				{ID: "browser.tabs_list", Name: "Tabs List", Description: "List all open tabs.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.tabs_new", Name: "Tabs New", Description: "Open a new tab.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.tabs_switch", Name: "Tabs Switch", Description: "Switch focus to a tab by index.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.tabs_close", Name: "Tabs Close", Description: "Close a tab by index.", PermissionLevel: "execute", IsEnabled: true},
				// iframes
				{ID: "browser.switch_frame", Name: "Switch Frame", Description: "Enter an iframe context.", PermissionLevel: "read", IsEnabled: true},
				{ID: "browser.switch_main_frame", Name: "Switch Main Frame", Description: "Return to the main page context.", PermissionLevel: "read", IsEnabled: true},
				// Interact
				{ID: "browser.click", Name: "Click", Description: "Click a page element.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "browser.hover", Name: "Hover", Description: "Hover over an element.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "browser.select", Name: "Select", Description: "Select a dropdown option.", PermissionLevel: "draft", IsEnabled: true},
				{ID: "browser.type_text", Name: "Type Text", Description: "Type text into an input field.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.fill_form", Name: "Fill Form", Description: "Fill multiple form fields at once.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.submit_form", Name: "Submit Form", Description: "Submit a form.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.eval", Name: "Eval JS", Description: "Execute JavaScript in the page.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.upload_file", Name: "Upload File", Description: "Set a file on a file input element.", PermissionLevel: "execute", IsEnabled: true},
				// Session / login
				{ID: "browser.session_login", Name: "Session Login", Description: "Auto-login using vault credentials.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.session_store_credentials", Name: "Store Credentials", Description: "Store login credentials in the vault.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.session_submit_2fa", Name: "Submit 2FA", Description: "Submit a 2FA code.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.session_clear", Name: "Clear Session", Description: "Clear stored session cookies for a host.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "browser.solve_captcha", Name: "Solve CAPTCHA", Description: "Solve a visual CAPTCHA using the active vision model.", PermissionLevel: "execute", IsEnabled: true},
			},
		},
		{
			Manifest: SkillManifestInfo{
				ID: "vault", Name: "Vault", Version: "1.0",
				Description:    "Secure credential vault — store, retrieve, and manage passwords, tokens, and TOTP 2FA secrets created or discovered by the agent.",
				LifecycleState: "enabled", RiskLevel: "medium", IsUserVisible: true,
				Category: "security", Capabilities: []string{"credential_storage", "totp_generation"}, Tags: []string{"vault", "credentials", "2fa", "security"},
			},
			Actions: []SkillAction{
				{ID: "vault.store", Name: "Store Credential", Description: "Save a new credential to the vault.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "vault.lookup", Name: "Lookup Credential", Description: "Look up stored credentials for a service.", PermissionLevel: "read", IsEnabled: true},
				{ID: "vault.list", Name: "List Credentials", Description: "List all vault entries (no passwords exposed).", PermissionLevel: "read", IsEnabled: true},
				{ID: "vault.update", Name: "Update Credential", Description: "Update an existing vault entry.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "vault.delete", Name: "Delete Credential", Description: "Permanently delete a vault entry.", PermissionLevel: "execute", IsEnabled: true},
				{ID: "vault.totp_generate", Name: "Generate TOTP Code", Description: "Generate the current TOTP 2FA code for a vault entry.", PermissionLevel: "read", IsEnabled: true},
			},
		},
	}
}

// ListSkills returns all skills — built-in and custom — with lifecycle state from the
// Go state overlay and approval policies from action-policies.json.
func ListSkills(supportDir string) []SkillRecord {
	records := builtInSkills()
	states := loadSkillStates(supportDir)
	policies := loadActionPolicies(supportDir)

	for i := range records {
		// Apply state override if present.
		if state, ok := states[records[i].Manifest.ID]; ok {
			records[i].Manifest.LifecycleState = state
		}
		// Apply per-action approval policies.
		for j := range records[i].Actions {
			action := &records[i].Actions[j]
			if policy, ok := policies[action.ID]; ok {
				action.ApprovalPolicy = policy
			} else {
				// Default: read → auto_approve, others → always_ask
				if action.PermissionLevel == "read" {
					action.ApprovalPolicy = "auto_approve"
				} else {
					action.ApprovalPolicy = "always_ask"
				}
			}
		}
	}

	// Append user-installed custom skills.
	for _, manifest := range customskills.ListManifests(supportDir) {
		records = append(records, customManifestToRecord(manifest, states, policies))
	}

	return records
}

// customManifestToRecord converts a CustomSkillManifest to a SkillRecord, applying
// any state and policy overrides that may already be stored.
func customManifestToRecord(manifest customskills.CustomSkillManifest, states skillStates, policies map[string]string) SkillRecord {
	lifecycleState := "enabled"
	if state, ok := states[manifest.ID]; ok {
		lifecycleState = state
	}

	actions := make([]SkillAction, 0, len(manifest.Actions))
	for _, a := range manifest.Actions {
		permLevel := a.PermLevel
		if permLevel == "" {
			permLevel = "execute"
		}
		actionID := manifest.ID + "." + a.Name
		approvalPolicy := "always_ask"
		if policy, ok := policies[actionID]; ok {
			approvalPolicy = policy
		} else if permLevel == "read" {
			approvalPolicy = "auto_approve"
		}
		actions = append(actions, SkillAction{
			ID:              actionID,
			Name:            a.Name,
			Description:     a.Description,
			PermissionLevel: permLevel,
			ApprovalPolicy:  approvalPolicy,
			IsEnabled:       lifecycleState == "enabled",
		})
	}

	version := manifest.Version
	if version == "" {
		version = "1.0"
	}

	return SkillRecord{
		Manifest: SkillManifestInfo{
			ID:             manifest.ID,
			Name:           manifest.Name,
			Version:        version,
			Description:    manifest.Description,
			LifecycleState: lifecycleState,
			RiskLevel:      "medium",
			IsUserVisible:  true,
			Category:       "custom",
			Source:         "custom",
			Capabilities:   []string{},
			Tags:           []string{"custom"},
		},
		Actions: actions,
	}
}

// SkillValidation is the validation result embedded in a SkillRecord.
type SkillValidation struct {
	SkillID  string   `json:"skillID"`
	Status   string   `json:"status"`
	Summary  string   `json:"summary"`
	IsValid  bool     `json:"isValid"`
	Issues   []string `json:"issues"`
}

// ValidateSkill runs a lightweight validation check on the skill and returns the
// SkillRecord with a Validation field attached. For built-in read-only skills,
// validation always succeeds. For skills requiring external credentials
// (web research, finance, etc.) it checks whether the key is present in the
// Keychain credential bundle.
func ValidateSkill(supportDir, skillID string, keyCheck func(skillID string) (bool, string)) *SkillRecord {
	records := ListSkills(supportDir)
	var found *SkillRecord
	for i := range records {
		if records[i].Manifest.ID == skillID {
			found = &records[i]
			break
		}
	}
	if found == nil {
		return nil
	}

	valid, summary := true, "Skill is ready."
	issues := []string{}
	if keyCheck != nil {
		if ok, reason := keyCheck(skillID); !ok {
			valid = false
			summary = reason
			issues = append(issues, reason)
		}
	}

	status := "passed"
	if !valid {
		status = "failed"
	}

	result := *found
	result.Validation = &SkillValidation{
		SkillID: skillID,
		Status:  status,
		Summary: summary,
		IsValid: valid,
		Issues:  issues,
	}
	return &result
}

// SetForgeSkillState persists a lifecycle state for a forge-installed skill without
// requiring it to appear in builtInSkills(). Used by install, install-enable, and uninstall.
func SetForgeSkillState(supportDir, skillID, newState string) {
	states := loadSkillStates(supportDir)
	states[skillID] = newState
	saveSkillStates(supportDir, states)
}

// SetSkillState enables or disables a skill by ID and persists to go-skill-states.json.
// Works for both built-in skills and user-installed custom skills.
// Returns the updated SkillRecord, or nil if skillID is not found in either set.
func SetSkillState(supportDir, skillID, newState string) *SkillRecord {
	// Check built-in skills first.
	builtIns := builtInSkills()
	isBuiltIn := false
	for _, s := range builtIns {
		if s.Manifest.ID == skillID {
			isBuiltIn = true
			break
		}
	}

	// If not built-in, look for a matching custom skill.
	if !isBuiltIn {
		for _, manifest := range customskills.ListManifests(supportDir) {
			if manifest.ID == skillID {
				states := loadSkillStates(supportDir)
				states[skillID] = newState
				saveSkillStates(supportDir, states)
				rec := customManifestToRecord(manifest, states, loadActionPolicies(supportDir))
				return &rec
			}
		}
		return nil // Not found anywhere.
	}

	states := loadSkillStates(supportDir)
	states[skillID] = newState
	saveSkillStates(supportDir, states)

	// Return the full record with updated state and policies applied.
	records := ListSkills(supportDir)
	for i := range records {
		if records[i].Manifest.ID == skillID {
			return &records[i]
		}
	}
	return nil
}
