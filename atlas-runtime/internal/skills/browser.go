package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"atlas-runtime-go/internal/browser"
	"atlas-runtime-go/internal/creds"
	"github.com/pquerna/otp/totp"
)

// browserImagePrefix is prepended to base64-encoded PNG screenshots so that
// loop.go can detect and route them into vision content blocks.
const browserImagePrefix = "__ATLAS_IMAGE__:"

func (r *Registry) registerBrowser() {
	if r.browserMgr == nil {
		// Browser skill is unavailable when no manager is wired in.
		// This can happen in test environments without Chrome.
		return
	}

	// ── Phase 1: Read/observe ─────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.navigate",
			Description: "Navigate the controlled browser to a URL. Returns the page title, final URL, and whether a login wall was detected. Stored session cookies are automatically injected before navigation.",
			Properties: map[string]ToolParam{
				"url":          {Description: "Full URL to navigate to, including scheme (e.g. https://github.com)", Type: "string"},
				"wait_for":     {Description: "CSS selector to wait for after navigation before returning — optional", Type: "string"},
				"timeout_ms":   {Description: "Navigation timeout in milliseconds (default 15000)", Type: "integer"},
			},
			Required: []string{"url"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserNavigate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.screenshot",
			Description: "Capture a screenshot of the current browser page. Returns a base64-encoded PNG image that vision-capable models can analyse. Use this to visually inspect a page, verify a UI state, or debug a navigation issue.",
			Properties: map[string]ToolParam{
				"full_page": {Description: "Capture the full scrollable page instead of just the visible viewport (default false)", Type: "boolean"},
				"selector":  {Description: "CSS selector — capture only the matching element instead of the full page — optional", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserScreenshot,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.read_page",
			Description: "Extract the visible text content of the current page or a specific element. Use this to read page content, find form labels, or locate navigation items without a screenshot.",
			Properties: map[string]ToolParam{
				"selector":  {Description: "CSS selector to limit extraction to a specific element — optional, defaults to the full body", Type: "string"},
				"max_chars": {Description: "Maximum characters to return (default 5000)", Type: "integer"},
			},
			Required: []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserReadPage,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.find_element",
			Description: "Find a DOM element by CSS selector and return its text content or a specific attribute value.",
			Properties: map[string]ToolParam{
				"selector":  {Description: "CSS selector of the element to find", Type: "string"},
				"attribute": {Description: "Attribute name to return (e.g. href, value, src) — optional, defaults to text content", Type: "string"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserFindElement,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.scroll",
			Description: "Scroll the current page by a number of pixels in a direction, or scroll a specific element into view.",
			Properties: map[string]ToolParam{
				"direction": {Description: "Scroll direction", Type: "string", Enum: []string{"down", "up", "left", "right"}},
				"amount":    {Description: "Pixels to scroll (default 500)", Type: "integer"},
				"selector":  {Description: "CSS selector — scroll this element into view instead of scrolling the page — optional", Type: "string"},
			},
			Required: []string{"direction"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserScroll,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.session_check",
			Description: "Check whether a stored browser session exists for a given host. A session is considered fresh if it was used within the last 7 days.",
			Properties: map[string]ToolParam{
				"host": {Description: "Hostname to check, e.g. github.com", Type: "string"},
			},
			Required: []string{"host"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserSessionCheck,
	})

	// ── Phase 2: Interaction (draft) ──────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.click",
			Description: "Click a page element identified by a CSS selector. Use browser.screenshot or browser.read_page first to locate the correct selector.",
			Properties: map[string]ToolParam{
				"selector":      {Description: "CSS selector of the element to click", Type: "string"},
				"wait_after_ms": {Description: "Milliseconds to wait after clicking for the page to react (default 500)", Type: "integer"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "draft",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserClick,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.hover",
			Description: "Move the mouse pointer over an element — useful for revealing dropdown menus or tooltips.",
			Properties: map[string]ToolParam{
				"selector": {Description: "CSS selector of the element to hover", Type: "string"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "draft",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserHover,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.select",
			Description: "Select an option in a <select> dropdown element by its visible text or value.",
			Properties: map[string]ToolParam{
				"selector": {Description: "CSS selector of the <select> element", Type: "string"},
				"value":    {Description: "Visible text or value attribute of the option to select", Type: "string"},
			},
			Required: []string{"selector", "value"},
		},
		PermLevel:   "draft",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserSelect,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.wait_for_element",
			Description: "Wait for a CSS selector to appear on the current page. Useful after clicks or form submissions that trigger dynamic content.",
			Properties: map[string]ToolParam{
				"selector":   {Description: "CSS selector to wait for", Type: "string"},
				"timeout_ms": {Description: "Maximum wait in milliseconds (default 10000)", Type: "integer"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserWaitForElement,
	})

	// ── Phase 2: Interaction (execute) ────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.type_text",
			Description: "Type text into a form input field. By default clears any existing content first.",
			Properties: map[string]ToolParam{
				"selector":    {Description: "CSS selector of the input or textarea", Type: "string"},
				"text":        {Description: "Text to type into the field", Type: "string"},
				"clear_first": {Description: "Clear existing content before typing (default true)", Type: "boolean"},
			},
			Required: []string{"selector", "text"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserTypeText,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.fill_form",
			Description: "Fill multiple form fields at once and optionally submit the form. The fields parameter maps CSS selector to the value to type.",
			Properties: map[string]ToolParam{
				"fields":          {Description: "JSON object mapping CSS selector strings to values, e.g. {\"#email\": \"user@example.com\", \"#password\": \"secret\"}", Type: "string"},
				"submit_selector": {Description: "CSS selector of the submit button to click after filling — optional", Type: "string"},
			},
			Required: []string{"fields"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserFillForm,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.submit_form",
			Description: "Click a form submit button or submit a form by CSS selector.",
			Properties: map[string]ToolParam{
				"selector": {Description: "CSS selector of the submit button or form element", Type: "string"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserSubmitForm,
	})

	// ── Phase 3: Session / login ──────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.session_login",
			Description: "Attempt to log in to the site on the current page using credentials stored in the vault. Automatically handles the full login form fill and submit cycle. Returns success or a 2FA prompt if needed.",
			Properties: map[string]ToolParam{
				"url": {Description: "URL of the site to log in to (used to look up vault credentials by host)", Type: "string"},
			},
			Required: []string{"url"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserSessionLogin,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.session_store_credentials",
			Description: "Store login credentials for a site in the vault so they can be used by browser.session_login in future. Equivalent to vault.store but scoped to browser sessions.",
			Properties: map[string]ToolParam{
				"host":        {Description: "Hostname of the site, e.g. github.com", Type: "string"},
				"username":    {Description: "Username or email address", Type: "string"},
				"password":    {Description: "Password", Type: "string"},
				"totp_secret": {Description: "TOTP base32 seed for 2FA — optional", Type: "string"},
			},
			Required: []string{"host", "username", "password"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		Fn:          r.browserSessionStoreCredentials,
	})

	// ── Phase 4: 2FA ──────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.session_submit_2fa",
			Description: "Submit a 2FA verification code to the currently open 2FA form. Use vault.totp_generate to generate the code if a TOTP secret is stored, otherwise ask the user.",
			Properties: map[string]ToolParam{
				"code":     {Description: "The 2FA code to submit (typically 6 digits)", Type: "string"},
				"selector": {Description: "CSS selector of the 2FA code input — optional, auto-detected if omitted", Type: "string"},
			},
			Required: []string{"code"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserSessionSubmit2FA,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.session_clear",
			Description: "Clear stored browser session cookies for a host. The next browser.navigate to that site will start a fresh unauthenticated session.",
			Properties: map[string]ToolParam{
				"host": {Description: "Hostname to clear session for, e.g. github.com", Type: "string"},
			},
			Required: []string{"host"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn:          r.browserSessionClear,
	})

	// ── Tab management ────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.tabs_list",
			Description: "List all open browser tabs with their index, URL, title, and which is active.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserTabsList,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.tabs_new",
			Description: "Open a new browser tab, optionally navigating to a URL, and switch focus to it.",
			Properties: map[string]ToolParam{
				"url": {Description: "URL to open in the new tab — optional, defaults to about:blank", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserTabsNew,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.tabs_switch",
			Description: "Switch focus to a browser tab by its index (from browser.tabs_list).",
			Properties: map[string]ToolParam{
				"index": {Description: "Zero-based tab index to switch to", Type: "integer"},
			},
			Required: []string{"index"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserTabsSwitch,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.tabs_close",
			Description: "Close a browser tab by its index. Cannot close the last remaining tab.",
			Properties: map[string]ToolParam{
				"index": {Description: "Zero-based tab index to close", Type: "integer"},
			},
			Required: []string{"index"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn:          r.browserTabsClose,
	})

	// ── JavaScript evaluation ─────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.eval",
			Description: "Execute a JavaScript expression in the current page or iframe context and return the JSON-serialised result. Useful for reading complex DOM state, extracting data, or triggering JS APIs.",
			Properties: map[string]ToolParam{
				"expression": {Description: "JavaScript expression to evaluate, e.g. `document.title` or `() => window.location.href`", Type: "string"},
			},
			Required: []string{"expression"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserEval,
	})

	// ── File upload ───────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.upload_file",
			Description: "Set a file on a file input element (<input type=\"file\">). Use this to upload files via web forms.",
			Properties: map[string]ToolParam{
				"selector":  {Description: "CSS selector of the file input element", Type: "string"},
				"file_path": {Description: "Absolute path to the file to upload on the local filesystem", Type: "string"},
			},
			Required: []string{"selector", "file_path"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserUploadFile,
	})

	// ── Network idle ──────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.wait_network_idle",
			Description: "Wait for the current page to finish loading all resources. Use after actions that trigger navigation or heavy dynamic content loading.",
			Properties: map[string]ToolParam{
				"timeout_ms": {Description: "Maximum wait in milliseconds (default 30000)", Type: "integer"},
			},
			Required: []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserWaitNetworkIdle,
	})

	// ── iframe context ────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.switch_frame",
			Description: "Switch the interaction context into an iframe identified by a CSS selector. Subsequent element operations (click, type, find, etc.) will target elements inside this frame. Call browser.switch_main_frame to return to the main page.",
			Properties: map[string]ToolParam{
				"selector": {Description: "CSS selector of the <iframe> element to enter, e.g. iframe#content or iframe[name=app]", Type: "string"},
			},
			Required: []string{"selector"},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserSwitchFrame,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "browser.switch_main_frame",
			Description: "Exit any active iframe context and return interaction focus to the main page.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn:          r.browserSwitchMainFrame,
	})

	// ── CAPTCHA solver ────────────────────────────────────────────────────────

	r.register(SkillEntry{
		Def: ToolDef{
			Name: "browser.solve_captcha",
			Description: "Solve a visual CAPTCHA on the current page using the active vision model. " +
				"Screenshots the CAPTCHA element, sends it to the AI for text extraction, " +
				"and optionally types the answer and submits the form. " +
				"Works on text/character CAPTCHAs, math challenges, and distorted-text CAPTCHAs. " +
				"Does NOT solve reCAPTCHA v2/v3 or hCaptcha (those require server-side bypass). " +
				"If captcha_selector is omitted the page is inspected for common CAPTCHA patterns.",
			Properties: map[string]ToolParam{
				"captcha_selector": {Description: "CSS selector of the CAPTCHA image or canvas element — optional, auto-detected if omitted", Type: "string"},
				"input_selector":   {Description: "CSS selector of the input field to type the answer into — optional, auto-detected if omitted", Type: "string"},
				"submit_selector":  {Description: "CSS selector of the submit button to click after typing — optional, not clicked if omitted", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          r.browserSolveCaptcha,
	})
}

// ── Phase 1 skill functions ───────────────────────────────────────────────────

func (r *Registry) browserNavigate(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL        string `json:"url"`
		WaitFor    string `json:"wait_for"`
		TimeoutMs  int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.navigate: invalid args: %w", err)
	}
	if p.URL == "" {
		return "", fmt.Errorf("browser.navigate: url is required")
	}

	result, err := r.browserMgr.Navigate(ctx, p.URL, p.WaitFor, p.TimeoutMs)
	if err != nil {
		return "", fmt.Errorf("browser.navigate: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Navigated to: %s\nTitle: %s\n", result.URL, result.Title)

	if result.LoginWallDetected {
		fmt.Fprintln(&sb, "\nLogin wall detected.")
		if result.LoginWallInfo != nil && result.LoginWallInfo.UsernameInputSelector != "" {
			fmt.Fprintf(&sb, "Username field: %s\nPassword field: %s\n",
				result.LoginWallInfo.UsernameInputSelector,
				result.LoginWallInfo.PasswordInputSelector)
			fmt.Fprintln(&sb, "Call browser.session_login to attempt automatic login using vault credentials, or browser.fill_form to log in manually.")
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

func (r *Registry) browserScreenshot(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		FullPage bool   `json:"full_page"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.screenshot: invalid args: %w", err)
	}

	img, err := r.browserMgr.Screenshot(ctx, p.FullPage, p.Selector)
	if err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}

	// The __ATLAS_IMAGE__: prefix signals loop.go to build a vision content block.
	return browserImagePrefix + "data:image/png;base64," + base64.StdEncoding.EncodeToString(img), nil
}

func (r *Registry) browserReadPage(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.read_page: invalid args: %w", err)
	}

	text, err := r.browserMgr.ReadPage(ctx, p.Selector, p.MaxChars)
	if err != nil {
		return "", fmt.Errorf("browser.read_page: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "Page text is empty. Try browser.screenshot to inspect visually.", nil
	}
	return text, nil
}

func (r *Registry) browserFindElement(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector  string `json:"selector"`
		Attribute string `json:"attribute"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.find_element: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.find_element: selector is required")
	}

	text, err := r.browserMgr.FindElement(ctx, p.Selector, p.Attribute)
	if err != nil {
		return "", fmt.Errorf("browser.find_element: %w", err)
	}
	return text, nil
}

func (r *Registry) browserScroll(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
		Selector  string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.scroll: invalid args: %w", err)
	}
	if p.Direction == "" && p.Selector == "" {
		return "", fmt.Errorf("browser.scroll: direction or selector is required")
	}

	if err := r.browserMgr.Scroll(ctx, p.Direction, p.Amount, p.Selector); err != nil {
		return "", fmt.Errorf("browser.scroll: %w", err)
	}
	if p.Selector != "" {
		return fmt.Sprintf("Scrolled element %q into view.", p.Selector), nil
	}
	return fmt.Sprintf("Scrolled %s by %d pixels.", p.Direction, p.Amount), nil
}

func (r *Registry) browserSessionCheck(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.session_check: invalid args: %w", err)
	}
	if p.Host == "" {
		return "", fmt.Errorf("browser.session_check: host is required")
	}

	fresh, _, err := r.browserMgr.SessionCheck(ctx, p.Host)
	if err != nil {
		return "", fmt.Errorf("browser.session_check: %w", err)
	}

	if fresh {
		// A cookie record exists in the DB but we cannot confirm the cookies are
		// still valid without navigating — they may have expired server-side.
		return fmt.Sprintf(
			"A stored session exists for %s. Navigate to the site to confirm the cookies are still valid. "+
				"If a login wall appears, call browser.session_login to re-authenticate.",
			p.Host,
		), nil
	}
	return fmt.Sprintf("No stored session for %s. Call browser.navigate followed by browser.session_login to authenticate.", p.Host), nil
}

// ── Phase 2 skill functions ───────────────────────────────────────────────────

func (r *Registry) browserClick(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector    string `json:"selector"`
		WaitAfterMs int    `json:"wait_after_ms"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.click: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.click: selector is required")
	}
	if p.WaitAfterMs == 0 {
		p.WaitAfterMs = 500
	}

	if err := r.browserMgr.Click(ctx, p.Selector, p.WaitAfterMs); err != nil {
		return "", fmt.Errorf("browser.click: %w", err)
	}
	return fmt.Sprintf("Clicked %q.", p.Selector), nil
}

func (r *Registry) browserHover(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.hover: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.hover: selector is required")
	}

	if err := r.browserMgr.Hover(ctx, p.Selector); err != nil {
		return "", fmt.Errorf("browser.hover: %w", err)
	}
	return fmt.Sprintf("Hovered over %q.", p.Selector), nil
}

func (r *Registry) browserSelect(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
		Value    string `json:"value"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.select: invalid args: %w", err)
	}
	if p.Selector == "" || p.Value == "" {
		return "", fmt.Errorf("browser.select: selector and value are required")
	}

	if err := r.browserMgr.SelectOption(ctx, p.Selector, p.Value); err != nil {
		return "", fmt.Errorf("browser.select: %w", err)
	}
	return fmt.Sprintf("Selected %q in %q.", p.Value, p.Selector), nil
}

func (r *Registry) browserWaitForElement(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector  string `json:"selector"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.wait_for_element: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.wait_for_element: selector is required")
	}

	if err := r.browserMgr.WaitForElement(ctx, p.Selector, p.TimeoutMs); err != nil {
		return "", fmt.Errorf("browser.wait_for_element: %w", err)
	}
	return fmt.Sprintf("Element %q appeared.", p.Selector), nil
}

func (r *Registry) browserTypeText(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector   string `json:"selector"`
		Text       string `json:"text"`
		ClearFirst *bool  `json:"clear_first"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.type_text: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.type_text: selector is required")
	}

	clearFirst := true
	if p.ClearFirst != nil {
		clearFirst = *p.ClearFirst
	}

	if err := r.browserMgr.TypeText(ctx, p.Selector, p.Text, clearFirst); err != nil {
		return "", fmt.Errorf("browser.type_text: %w", err)
	}
	return fmt.Sprintf("Typed text into %q.", p.Selector), nil
}

func (r *Registry) browserFillForm(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Fields         string `json:"fields"`          // JSON object: selector → value
		SubmitSelector string `json:"submit_selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.fill_form: invalid args: %w", err)
	}
	if p.Fields == "" {
		return "", fmt.Errorf("browser.fill_form: fields is required")
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(p.Fields), &fields); err != nil {
		return "", fmt.Errorf("browser.fill_form: fields must be a JSON object mapping selector to value: %w", err)
	}
	if len(fields) == 0 {
		return "", fmt.Errorf("browser.fill_form: fields must contain at least one entry")
	}

	if err := r.browserMgr.FillForm(ctx, fields, p.SubmitSelector); err != nil {
		return "", fmt.Errorf("browser.fill_form: %w", err)
	}

	msg := fmt.Sprintf("Filled %d form field(s).", len(fields))
	if p.SubmitSelector != "" {
		msg += " Form submitted."
	}
	return msg, nil
}

func (r *Registry) browserSubmitForm(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.submit_form: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.submit_form: selector is required")
	}

	if err := r.browserMgr.SubmitForm(ctx, p.Selector); err != nil {
		return "", fmt.Errorf("browser.submit_form: %w", err)
	}
	return fmt.Sprintf("Submitted form via %q.", p.Selector), nil
}

// ── Phase 3 skill functions ───────────────────────────────────────────────────

func (r *Registry) browserSessionLogin(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.session_login: invalid args: %w", err)
	}
	if p.URL == "" {
		return "", fmt.Errorf("browser.session_login: url is required")
	}

	host := browser.ExtractHost(p.URL)

	// Look up credentials in vault.
	entries, err := creds.VaultRead()
	if err != nil {
		return "", fmt.Errorf("browser.session_login: vault read: %w", err)
	}

	hostLower := strings.ToLower(host)
	var username, password string
	for _, e := range entries {
		svc := strings.ToLower(e.Service)
		// Exact match, or the vault entry is a parent domain of the host
		// (e.g. vault "github.com" matches host "gist.github.com").
		// Bidirectional substring is intentionally avoided — it would match
		// a "mail" entry against "mailicious.com".
		if svc == hostLower || strings.HasSuffix(hostLower, "."+svc) {
			username = e.Username
			password = e.Password
			break
		}
	}

	if username == "" {
		return fmt.Sprintf(
			"No credentials found in vault for %s. Call browser.session_store_credentials first to save login details, then retry.",
			host,
		), nil
	}

	result := r.browserMgr.AutoLogin(ctx, host, username, password)

	if result.TwoFARequired {
		// Check if vault has a TOTP secret for this host.
		for _, e := range entries {
			svc := strings.ToLower(e.Service)
			if e.TOTPSecret == "" {
				continue
			}
			if svc != hostLower && !strings.HasSuffix(hostLower, "."+svc) {
				continue
			}
			// TOTP secret available — generate the code directly (avoids fragile
			// string parsing of vaultTOTPGenerate's formatted output).
			secret := strings.ToUpper(strings.TrimSpace(e.TOTPSecret))
			code, totpErr := totp.GenerateCode(secret, time.Now())
			if totpErr != nil {
				continue
			}
			submitErr := r.browserMgr.Submit2FA(ctx, code, result.TwoFASelector)
			if submitErr == nil {
				return fmt.Sprintf("Logged in to %s and submitted TOTP 2FA code automatically.", host), nil
			}
		}
		// No TOTP available — return the 2FA prompt to the user.
		return result.Message, nil
	}

	return result.Message, nil
}

func (r *Registry) browserSessionStoreCredentials(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Host       string `json:"host"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		TOTPSecret string `json:"totp_secret"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.session_store_credentials: invalid args: %w", err)
	}
	if p.Host == "" || p.Username == "" || p.Password == "" {
		return "", fmt.Errorf("browser.session_store_credentials: host, username, and password are required")
	}

	storeArgs, err := json.Marshal(map[string]string{
		"service":     p.Host,
		"label":       p.Host + " – Browser Session",
		"username":    p.Username,
		"password":    p.Password,
		"totp_secret": p.TOTPSecret,
		"notes":       "Stored by browser.session_store_credentials",
	})
	if err != nil {
		return "", fmt.Errorf("browser.session_store_credentials: marshal: %w", err)
	}
	return vaultStore(ctx, storeArgs)
}

// ── Phase 4 skill functions ───────────────────────────────────────────────────

func (r *Registry) browserSessionSubmit2FA(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Code     string `json:"code"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.session_submit_2fa: invalid args: %w", err)
	}
	if p.Code == "" {
		return "", fmt.Errorf("browser.session_submit_2fa: code is required")
	}

	if err := r.browserMgr.Submit2FA(ctx, p.Code, p.Selector); err != nil {
		return "", fmt.Errorf("browser.session_submit_2fa: %w", err)
	}
	return "2FA code submitted.", nil
}

func (r *Registry) browserSessionClear(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.session_clear: invalid args: %w", err)
	}
	if p.Host == "" {
		return "", fmt.Errorf("browser.session_clear: host is required")
	}

	if err := r.browserMgr.ClearSession(p.Host); err != nil {
		return "", fmt.Errorf("browser.session_clear: %w", err)
	}
	return fmt.Sprintf("Browser session cleared for %s. Next navigation will start unauthenticated.", p.Host), nil
}

// ── Tab management functions ───────────────────────────────────────────────────

func (r *Registry) browserTabsList(ctx context.Context, args json.RawMessage) (string, error) {
	tabs, err := r.browserMgr.TabsList()
	if err != nil {
		return "", fmt.Errorf("browser.tabs_list: %w", err)
	}
	if len(tabs) == 0 {
		return "No tabs open. Call browser.navigate to open a page first.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Open tabs (%d):\n", len(tabs))
	for _, t := range tabs {
		active := ""
		if t.IsActive {
			active = " [active]"
		}
		fmt.Fprintf(&sb, "  [%d]%s %s — %s\n", t.Index, active, t.URL, t.Title)
	}
	return strings.TrimSpace(sb.String()), nil
}

func (r *Registry) browserTabsNew(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.tabs_new: invalid args: %w", err)
	}

	idx, err := r.browserMgr.TabsNew(ctx, p.URL)
	if err != nil {
		return "", fmt.Errorf("browser.tabs_new: %w", err)
	}

	msg := fmt.Sprintf("Opened new tab at index %d.", idx)
	if p.URL != "" {
		msg = fmt.Sprintf("Opened new tab at index %d and navigated to %s.", idx, p.URL)
	}
	return msg, nil
}

func (r *Registry) browserTabsSwitch(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.tabs_switch: invalid args: %w", err)
	}

	if err := r.browserMgr.TabsSwitch(p.Index); err != nil {
		return "", fmt.Errorf("browser.tabs_switch: %w", err)
	}
	return fmt.Sprintf("Switched to tab %d.", p.Index), nil
}

func (r *Registry) browserTabsClose(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.tabs_close: invalid args: %w", err)
	}

	if err := r.browserMgr.TabsClose(p.Index); err != nil {
		return "", fmt.Errorf("browser.tabs_close: %w", err)
	}
	return fmt.Sprintf("Closed tab %d.", p.Index), nil
}

// ── Eval / upload / network idle functions ────────────────────────────────────

func (r *Registry) browserEval(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.eval: invalid args: %w", err)
	}
	if p.Expression == "" {
		return "", fmt.Errorf("browser.eval: expression is required")
	}

	result, err := r.browserMgr.Eval(ctx, p.Expression)
	if err != nil {
		return "", fmt.Errorf("browser.eval: %w", err)
	}
	return result, nil
}

func (r *Registry) browserUploadFile(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.upload_file: invalid args: %w", err)
	}
	if p.Selector == "" || p.FilePath == "" {
		return "", fmt.Errorf("browser.upload_file: selector and file_path are required")
	}

	// Enforce the fs-roots allowlist — the agent must not be able to upload
	// arbitrary files (e.g. SSH keys, atlas.sqlite3) to a web form.
	roots, err := loadApprovedRoots(r.supportDir)
	if err != nil {
		return "", fmt.Errorf("browser.upload_file: could not load approved roots: %w", err)
	}
	if err := checkApproved(p.FilePath, roots); err != nil {
		return "", fmt.Errorf("browser.upload_file: %w", err)
	}

	// Validate file exists before handing to the browser — rod gives an opaque
	// protocol error if the path is missing.
	if _, err := os.Stat(p.FilePath); err != nil {
		return "", fmt.Errorf("browser.upload_file: file not found at %q: %w", p.FilePath, err)
	}

	if err := r.browserMgr.UploadFile(ctx, p.Selector, p.FilePath); err != nil {
		return "", fmt.Errorf("browser.upload_file: %w", err)
	}
	return fmt.Sprintf("File %q set on input %q.", p.FilePath, p.Selector), nil
}

func (r *Registry) browserWaitNetworkIdle(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		TimeoutMs int `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.wait_network_idle: invalid args: %w", err)
	}

	if err := r.browserMgr.WaitNetworkIdle(ctx, p.TimeoutMs); err != nil {
		return "", fmt.Errorf("browser.wait_network_idle: %w", err)
	}
	return "Page finished loading.", nil
}

// ── iframe functions ──────────────────────────────────────────────────────────

func (r *Registry) browserSwitchFrame(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.switch_frame: invalid args: %w", err)
	}
	if p.Selector == "" {
		return "", fmt.Errorf("browser.switch_frame: selector is required")
	}

	if err := r.browserMgr.SwitchFrame(ctx, p.Selector); err != nil {
		return "", fmt.Errorf("browser.switch_frame: %w", err)
	}
	return fmt.Sprintf("Switched interaction context into iframe %q. Element operations now target this frame.", p.Selector), nil
}

func (r *Registry) browserSwitchMainFrame(ctx context.Context, args json.RawMessage) (string, error) {
	r.browserMgr.SwitchMainFrame()
	return "Returned to main page context.", nil
}

// ── CAPTCHA solver ────────────────────────────────────────────────────────────

// captchaImageSelectors are common CSS selectors for CAPTCHA image/canvas elements,
// in priority order (most specific first).
var captchaImageSelectors = []string{
	`img[src*="captcha"]`,
	`img[id*="captcha"]`,
	`img[class*="captcha"]`,
	`img[alt*="captcha"]`,
	`canvas[id*="captcha"]`,
	`canvas[class*="captcha"]`,
	`.captcha img`,
	`#captcha img`,
	`[class*="captcha"] img`,
	`[id*="captcha"] img`,
}

// captchaInputSelectors are common CSS selectors for CAPTCHA answer input fields.
var captchaInputSelectors = []string{
	`input[name*="captcha"]`,
	`input[id*="captcha"]`,
	`input[placeholder*="captcha"]`,
	`input[name="answer"]`,
	`#captchaInput`,
	`#captcha_input`,
	`.captcha input[type="text"]`,
}

func (r *Registry) browserSolveCaptcha(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		CaptchaSel string `json:"captcha_selector"`
		InputSel   string `json:"input_selector"`
		SubmitSel  string `json:"submit_selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("browser.solve_captcha: invalid args: %w", err)
	}

	if r.visionFn == nil {
		return "", fmt.Errorf("browser.solve_captcha: vision model not available — ensure an AI provider with vision support is configured")
	}

	// ── Auto-detect CAPTCHA image selector ──────────────────────────────────
	captchaSel := p.CaptchaSel
	if captchaSel == "" {
		captchaSel = r.detectCaptchaSelector(ctx)
		if captchaSel == "" {
			return "", fmt.Errorf(
				"browser.solve_captcha: could not auto-detect a CAPTCHA image on the current page. " +
					"Try providing captcha_selector manually. Use browser.screenshot to inspect the page first.",
			)
		}
	}

	// ── Screenshot the CAPTCHA element ──────────────────────────────────────
	img, err := r.browserMgr.Screenshot(ctx, false, captchaSel)
	if err != nil {
		return "", fmt.Errorf("browser.solve_captcha: screenshot captcha element %q: %w", captchaSel, err)
	}
	if len(img) == 0 {
		return "", fmt.Errorf("browser.solve_captcha: CAPTCHA screenshot returned empty image")
	}

	// ── Call vision model ────────────────────────────────────────────────────
	const visionPrompt = `This is a CAPTCHA challenge image. Your job is to read it and return the answer.

Rules:
- Return ONLY the characters the user must type — nothing else.
- No punctuation, no spaces unless the CAPTCHA clearly shows them.
- If it is a math problem (e.g. "3 + 7 = ?"), return just the numeric answer (e.g. "10").
- If it is distorted text, return the characters as best you can read them.
- Do not explain your reasoning. Return just the answer string.`

	imageB64 := base64.StdEncoding.EncodeToString(img)
	answer, err := r.visionFn(ctx, imageB64, visionPrompt)
	if err != nil {
		return "", fmt.Errorf("browser.solve_captcha: vision inference failed: %w", err)
	}

	answer = cleanCaptchaAnswer(answer)
	if answer == "" {
		return "", fmt.Errorf("browser.solve_captcha: vision model returned an empty answer — the CAPTCHA may not be a supported type")
	}

	// ── Auto-detect input selector ───────────────────────────────────────────
	inputSel := p.InputSel
	if inputSel == "" {
		inputSel = r.detectCaptchaInputSelector(ctx)
	}

	// ── Type the answer ──────────────────────────────────────────────────────
	if inputSel != "" {
		if err := r.browserMgr.TypeText(ctx, inputSel, answer, true); err != nil {
			return fmt.Sprintf(
				"CAPTCHA answer: %q (could not type into input %q: %v). Type it manually.",
				answer, inputSel, err,
			), nil
		}
	}

	// ── Submit if requested ──────────────────────────────────────────────────
	if p.SubmitSel != "" {
		if err := r.browserMgr.Click(ctx, p.SubmitSel, 800); err != nil {
			return fmt.Sprintf(
				"CAPTCHA answered %q and typed into %q but could not click submit %q: %v",
				answer, inputSel, p.SubmitSel, err,
			), nil
		}
		return fmt.Sprintf("CAPTCHA solved: typed %q into %q and submitted.", answer, inputSel), nil
	}

	if inputSel != "" {
		return fmt.Sprintf("CAPTCHA solved: typed %q into %q. Call browser.submit_form or browser.click to submit.", answer, inputSel), nil
	}
	return fmt.Sprintf("CAPTCHA answer: %q (no input selector found — use browser.type_text to enter it manually).", answer), nil
}

// buildSelectorProbeScript builds a JS arrow function that returns the first
// matching selector from candidates, or "" if none matched.
// All selector strings are JSON-escaped to avoid injection via crafted values.
func buildSelectorProbeScript(selectors []string) string {
	parts := make([]string, len(selectors))
	for i, s := range selectors {
		parts[i] = `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return `() => { const c = [` + strings.Join(parts, ",") + `]; for (const s of c) { if (document.querySelector(s)) return s; } return ""; }`
}

// detectCaptchaSelector probes the page for common CAPTCHA image elements.
// Uses the caller's context (with a 3-second cap) so it respects turn cancellation.
// Returns the first matching selector, or "" if none found.
func (r *Registry) detectCaptchaSelector(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result, err := r.browserMgr.Eval(ctx, buildSelectorProbeScript(captchaImageSelectors))
	if err != nil || result == `""` || result == "" {
		return ""
	}
	return strings.Trim(result, `"`)
}

// detectCaptchaInputSelector probes the page for common CAPTCHA answer inputs.
// Uses the caller's context (with a 3-second cap) so it respects turn cancellation.
func (r *Registry) detectCaptchaInputSelector(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result, err := r.browserMgr.Eval(ctx, buildSelectorProbeScript(captchaInputSelectors))
	if err != nil || result == `""` || result == "" {
		return ""
	}
	return strings.Trim(result, `"`)
}

// cleanCaptchaAnswer strips surrounding whitespace and common noise from a
// vision model's CAPTCHA response (trailing periods, "Answer:", markdown, etc.).
func cleanCaptchaAnswer(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip leading labels the model may emit despite the prompt.
	for _, prefix := range []string{"Answer:", "answer:", "CAPTCHA:", "Text:", "Code:"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
		}
	}
	// Strip surrounding markdown code fences.
	s = strings.Trim(s, "`")
	// Strip surrounding quotes that some models add.
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}
