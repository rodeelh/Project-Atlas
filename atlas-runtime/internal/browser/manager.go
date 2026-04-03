// Package browser implements BrowserManager — the singleton that drives a
// Chrome/Chromium instance via the go-rod library.
//
// One Chrome process is shared across all agent turns. Multiple pages (tabs)
// are supported; the active tab is tracked by activeIdx. Cookie snapshots are
// persisted to SQLite so sessions survive Atlas restarts.
//
// Headless mode is the default. Set "browserShowWindow": true in
// go-runtime-config.json to open a visible Chrome window for debugging.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"

	"atlas-runtime-go/internal/storage"
)

// StoredCookie is a simplified cookie record safe for JSON serialisation
// and storage in SQLite. It captures the fields needed to restore a session.
type StoredCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	Expires  float64 `json:"expires"` // Unix timestamp; 0 = session cookie
}

// NavResult is returned by Navigate and contains page metadata plus login
// wall detection results.
type NavResult struct {
	URL               string
	Title             string
	LoginWallDetected bool
	LoginWallInfo     *LoginWallResult
}

// LoginResult is returned by AutoLogin.
type LoginResult struct {
	Success       bool
	TwoFARequired bool
	TwoFASelector string // CSS selector of the 2FA input if TwoFARequired is true
	Message       string
}

// TabInfo describes one open browser tab.
type TabInfo struct {
	Index    int    `json:"index"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	IsActive bool   `json:"isActive"`
}

// loginAttemptRecord tracks login attempt count and when the count was last reset.
type loginAttemptRecord struct {
	count   int
	resetAt time.Time
}

// Manager owns the singleton Chrome process and all open pages (tabs).
// All exported methods are safe for concurrent use.
type Manager struct {
	mu    sync.Mutex
	browser *rod.Browser
	// Multi-tab: pages holds all open tabs; activeIdx is the currently active one.
	pages     []*rod.Page
	activeIdx int
	// iframe context: when non-nil, interaction methods operate inside this frame.
	currentFrame *rod.Page
	db           *storage.DB
	headless     bool

	// Login attempt ceiling — prevents infinite login retry loops.
	// Counts reset automatically after loginAttemptTTL.
	loginAttempts map[string]loginAttemptRecord
}

// loginAttemptTTL is how long before the per-host login attempt counter resets.
const loginAttemptTTL = time.Hour

// New constructs a Manager. headless controls whether Chrome is launched
// without a visible window. Call Close() when the runtime shuts down.
func New(db *storage.DB, headless bool) *Manager {
	return &Manager{
		db:            db,
		headless:      headless,
		loginAttempts: make(map[string]loginAttemptRecord),
	}
}

// Close gracefully shuts down the Chrome process.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.browser != nil {
		_ = m.browser.Close()
		m.browser = nil
		m.pages = nil
		m.activeIdx = 0
		m.currentFrame = nil
	}
}

// ── Navigation ────────────────────────────────────────────────────────────────

// Navigate navigates the active page to rawURL and returns page metadata.
// If waitSelector is non-empty the call blocks until that element appears.
// Stored session cookies for the target host are injected before navigation
// and updated afterwards.
func (m *Manager) Navigate(ctx context.Context, rawURL, waitSelector string, timeoutMs int) (*NavResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return nil, err
	}
	m.currentFrame = nil // navigation always resets iframe context

	if timeoutMs <= 0 {
		timeoutMs = 15000
	}

	host := extractHost(rawURL)
	m.injectSessionCookies(page, host, rawURL)

	navCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	if err := page.Context(navCtx).Navigate(rawURL); err != nil {
		return nil, fmt.Errorf("browser: navigate to %s: %w", rawURL, err)
	}

	if err := page.Context(navCtx).WaitLoad(); err != nil {
		_ = err // WaitLoad timeout is non-fatal — page may still be usable.
	}

	if waitSelector != "" {
		if _, err := page.Context(navCtx).Element(waitSelector); err != nil {
			return nil, fmt.Errorf("browser: wait for %q: %w", waitSelector, err)
		}
	}

	info, err := page.Info()
	if err != nil {
		return nil, fmt.Errorf("browser: page info: %w", err)
	}

	lwr := DetectLoginWall(page)
	m.persistSessionCookies(page, host, rawURL)

	return &NavResult{
		URL:               info.URL,
		Title:             info.Title,
		LoginWallDetected: lwr.Detected,
		LoginWallInfo:     lwr,
	}, nil
}

// ── Tab management ────────────────────────────────────────────────────────────

// TabsList returns metadata for all open tabs.
func (m *Manager) TabsList() ([]TabInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]TabInfo, 0, len(m.pages))
	for i, page := range m.pages {
		tab := TabInfo{Index: i, IsActive: i == m.activeIdx}
		// Bound Info() so a stuck/crashed tab cannot hang the whole list.
		infoCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if info, err := page.Context(infoCtx).Info(); err == nil {
			tab.URL = info.URL
			tab.Title = info.Title
		} else {
			tab.URL = "(unavailable)"
			tab.Title = "(unavailable)"
		}
		cancel()
		result = append(result, tab)
	}
	return result, nil
}

// TabsNew opens a new tab, navigates it to url (or about:blank if empty),
// and switches focus to it. Returns the new tab index.
//
// Always creates with about:blank first so stealth JS is injected before any
// target-page JavaScript can run, then navigates to the actual URL.
func (m *Manager) TabsNew(ctx context.Context, url string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureBrowser(); err != nil {
		return 0, err
	}

	// Create with about:blank so stealth patches are in place before any
	// real page JS executes (same pattern as ensurePage).
	page, err := m.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return 0, fmt.Errorf("browser: new tab: %w", err)
	}
	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: 1280, Height: 800, DeviceScaleFactor: 1,
	})
	_, _ = page.Eval(stealth.JS) // inject before any real page loads

	if url != "" && url != "about:blank" {
		navCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := page.Context(navCtx).Navigate(url); err != nil {
			// Non-fatal — tab is open, navigation failed.
			_ = err
		}
		_ = page.Context(navCtx).WaitLoad()
		cancel()
	}

	m.pages = append(m.pages, page)
	m.activeIdx = len(m.pages) - 1
	m.currentFrame = nil

	return m.activeIdx, nil
}

// TabsSwitch switches focus to the tab at the given index.
func (m *Manager) TabsSwitch(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.pages) {
		return fmt.Errorf("browser: tab index %d out of range (have %d tabs)", index, len(m.pages))
	}
	m.activeIdx = index
	m.currentFrame = nil
	return nil
}

// TabsClose closes the tab at index. Cannot close the last remaining tab.
func (m *Manager) TabsClose(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.pages) {
		return fmt.Errorf("browser: tab index %d out of range (have %d tabs)", index, len(m.pages))
	}
	if len(m.pages) == 1 {
		return fmt.Errorf("browser: cannot close the last tab")
	}

	if err := m.pages[index].Close(); err != nil {
		log.Printf("browser: TabsClose[%d]: page.Close error (non-fatal, page may already be detached): %v", index, err)
	}
	m.pages = append(m.pages[:index], m.pages[index+1:]...)

	if m.activeIdx >= len(m.pages) {
		m.activeIdx = len(m.pages) - 1
	} else if m.activeIdx > index {
		m.activeIdx--
	}
	m.currentFrame = nil
	return nil
}

// ── Screenshot ────────────────────────────────────────────────────────────────

// Screenshot captures the current page as a PNG byte slice.
// If fullPage is true the full scrollable document is captured.
// If selector is non-empty only that element is captured.
func (m *Manager) Screenshot(ctx context.Context, fullPage bool, selector string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return nil, err
	}

	if selector != "" {
		el, err := page.Context(ctx).Element(selector)
		if err != nil {
			return nil, fmt.Errorf("browser: element %q not found: %w", selector, err)
		}
		img, err := el.Screenshot("png", 90)
		if err != nil {
			return nil, fmt.Errorf("browser: element screenshot: %w", err)
		}
		return img, nil
	}

	img, err := page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return nil, fmt.Errorf("browser: screenshot: %w", err)
	}
	return img, nil
}

// ── JavaScript evaluation ─────────────────────────────────────────────────────

// Eval executes a JavaScript expression in the current page/frame context and
// returns the JSON-serialised result.
func (m *Manager) Eval(ctx context.Context, jsExpr string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return "", err
	}

	res, err := pg.Context(ctx).Eval(jsExpr)
	if err != nil {
		return "", fmt.Errorf("browser: eval: %w", err)
	}

	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return res.Value.String(), nil
	}
	return string(raw), nil
}

// ── File upload ───────────────────────────────────────────────────────────────

// UploadFile sets the files on a file input element identified by selector.
// filePath must be an absolute path on the local filesystem.
func (m *Manager) UploadFile(ctx context.Context, selector, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: file input %q not found: %w", selector, err)
	}
	return el.SetFiles([]string{filePath})
}

// ── Network idle ──────────────────────────────────────────────────────────────

// WaitNetworkIdle waits for the page load event, which fires once all initial
// resources are loaded. Use after navigation or actions that trigger page loads.
func (m *Manager) WaitNetworkIdle(ctx context.Context, timeoutMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return err
	}

	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	if err := page.Context(waitCtx).WaitLoad(); err != nil {
		return fmt.Errorf("browser: wait_network_idle timed out after %dms", timeoutMs)
	}
	return nil
}

// ── iframe support ────────────────────────────────────────────────────────────

// SwitchFrame switches the interaction context to the iframe identified by
// selector. Subsequent element operations (click, type, find, etc.) target
// this frame until SwitchMainFrame is called.
func (m *Manager) SwitchFrame(ctx context.Context, selector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return err
	}

	el, err := page.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: iframe element %q not found: %w", selector, err)
	}

	frame, err := el.Frame()
	if err != nil {
		return fmt.Errorf("browser: switch to frame %q: %w", selector, err)
	}

	m.currentFrame = frame
	return nil
}

// SwitchMainFrame returns the interaction context to the main page, exiting
// any active iframe context.
func (m *Manager) SwitchMainFrame() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentFrame = nil
}

// ── Page content ──────────────────────────────────────────────────────────────

// ReadPage extracts the visible text of the current page or frame (or an element).
// maxChars limits the returned string length; 0 means default (5000).
func (m *Manager) ReadPage(ctx context.Context, selector string, maxChars int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return "", err
	}

	if maxChars <= 0 {
		maxChars = 5000
	}

	jsExpr := `() => document.body ? document.body.innerText : ""`
	if selector != "" {
		jsExpr = fmt.Sprintf(`() => { const el = document.querySelector(%q); return el ? el.innerText : ""; }`, selector)
	}

	res, err := pg.Context(ctx).Eval(jsExpr)
	if err != nil {
		return "", fmt.Errorf("browser: read page: %w", err)
	}

	text := res.Value.String()
	if len(text) > maxChars {
		text = text[:maxChars] + fmt.Sprintf("\n\n[truncated — %d total chars, use a narrower selector or increase max_chars]", len(text))
	}
	return text, nil
}

// ── Element interaction ───────────────────────────────────────────────────────

// FindElement finds an element and returns its text or a named attribute.
func (m *Manager) FindElement(ctx context.Context, selector, attribute string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return "", err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return "", fmt.Errorf("browser: element %q not found: %w", selector, err)
	}

	if attribute != "" {
		val, err := el.Attribute(attribute)
		if err != nil {
			return "", fmt.Errorf("browser: attribute %q on %q: %w", attribute, selector, err)
		}
		if val == nil {
			return "", nil
		}
		return *val, nil
	}

	return el.Text()
}

// Scroll scrolls the page by amount pixels in direction, or scrolls to an element.
func (m *Manager) Scroll(ctx context.Context, direction string, amount int, selector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	if selector != "" {
		el, err := pg.Context(ctx).Element(selector)
		if err != nil {
			return fmt.Errorf("browser: scroll target %q not found: %w", selector, err)
		}
		return el.ScrollIntoView()
	}

	if amount <= 0 {
		amount = 500
	}

	var x, y float64
	switch strings.ToLower(direction) {
	case "down":
		y = float64(amount)
	case "up":
		y = -float64(amount)
	case "right":
		x = float64(amount)
	case "left":
		x = -float64(amount)
	default:
		return fmt.Errorf("browser: unknown scroll direction %q — use down, up, left, or right", direction)
	}

	// Scroll uses the active page (not frame) since Mouse is a page-level concept.
	page, err := m.ensurePage()
	if err != nil {
		return err
	}
	return page.Mouse.Scroll(x, y, 1)
}

// Click clicks the element at selector.
// waitAfterMs is the millisecond delay after clicking (for page reactions).
func (m *Manager) Click(ctx context.Context, selector string, waitAfterMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: click target %q not found: %w", selector, err)
	}

	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("browser: click %q: %w", selector, err)
	}

	if waitAfterMs > 0 {
		time.Sleep(time.Duration(waitAfterMs) * time.Millisecond)
	}
	return nil
}

// Hover moves the mouse over an element.
func (m *Manager) Hover(ctx context.Context, selector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: hover target %q not found: %w", selector, err)
	}
	return el.Hover()
}

// SelectOption selects an option in a <select> element by visible text or value.
func (m *Manager) SelectOption(ctx context.Context, selector, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: select target %q not found: %w", selector, err)
	}

	if err := el.Select([]string{value}, true, rod.SelectorTypeText); err != nil {
		if err2 := el.Select([]string{value}, true, rod.SelectorTypeRegex); err2 != nil {
			return fmt.Errorf("browser: select %q in %q: %w", value, selector, err)
		}
	}
	return nil
}

// WaitForElement waits for selector to appear on the page.
func (m *Manager) WaitForElement(ctx context.Context, selector string, timeoutMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	if timeoutMs <= 0 {
		timeoutMs = 10000
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	_, err = pg.Context(waitCtx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: element %q did not appear within %dms: %w", selector, timeoutMs, err)
	}
	return nil
}

// TypeText types text into an input field identified by selector.
// If clearFirst is true the existing content is cleared first.
func (m *Manager) TypeText(ctx context.Context, selector, text string, clearFirst bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: type target %q not found: %w", selector, err)
	}

	if clearFirst {
		if err := el.SelectAllText(); err != nil {
			_ = err // Non-fatal — fall through and let Input overwrite.
		}
	}

	return el.Input(text)
}

// FillForm fills multiple form fields and optionally clicks a submit button.
// fields maps CSS selector → value to type.
func (m *Manager) FillForm(ctx context.Context, fields map[string]string, submitSelector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	cPage := pg.Context(ctx)
	for sel, val := range fields {
		el, err := cPage.Element(sel)
		if err != nil {
			return fmt.Errorf("browser: form field %q not found: %w", sel, err)
		}
		if err := el.Input(val); err != nil {
			return fmt.Errorf("browser: fill form field %q: %w", sel, err)
		}
	}

	if submitSelector != "" {
		el, err := cPage.Element(submitSelector)
		if err != nil {
			return fmt.Errorf("browser: submit button %q not found: %w", submitSelector, err)
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("browser: submit form: %w", err)
		}
	}
	return nil
}

// SubmitForm clicks a submit button or submits a form element.
func (m *Manager) SubmitForm(ctx context.Context, selector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pg, err := m.activePage()
	if err != nil {
		return err
	}

	el, err := pg.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: submit target %q not found: %w", selector, err)
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// ── Session management ────────────────────────────────────────────────────────

// SessionCheck returns whether a live session exists for the given host.
func (m *Manager) SessionCheck(ctx context.Context, host string) (fresh bool, lastUsed time.Time, err error) {
	_, found, dbErr := m.db.LoadBrowserSession(host)
	if dbErr != nil {
		return false, time.Time{}, dbErr
	}
	return found, time.Time{}, nil
}

// AutoLogin attempts to log in to the current page using provided credentials.
// Returns a LoginResult indicating success, 2FA requirement, or failure.
// A hard ceiling of 2 attempts per host per hour prevents infinite loops.
// The overall operation is bounded to 30 seconds so the mutex is never held
// longer than that regardless of how sub-steps behave.
func (m *Manager) AutoLogin(ctx context.Context, host, username, password string) *LoginResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return &LoginResult{Message: fmt.Sprintf("Browser not available: %v", err)}
	}

	rec := m.loginAttempts[host]
	if time.Since(rec.resetAt) >= loginAttemptTTL {
		rec = loginAttemptRecord{resetAt: time.Now()}
	}
	if rec.count >= 2 {
		return &LoginResult{
			Message: fmt.Sprintf(
				"Login for %s has already been attempted %d time(s) in the last hour. "+
					"Possible causes: wrong credentials, CAPTCHA, or bot detection. "+
					"Please log in manually in the browser window, then retry your task.",
				host, rec.count),
		}
	}
	rec.count++
	m.loginAttempts[host] = rec

	lwr := DetectLoginWall(page)
	if !lwr.Detected {
		return &LoginResult{Success: true, Message: "No login wall detected — may already be logged in."}
	}

	if lwr.UsernameInputSelector == "" || lwr.PasswordInputSelector == "" {
		return &LoginResult{Message: "Login wall detected but could not identify username/password fields. Try browser.fill_form manually."}
	}

	fillCtx, fillCancel := context.WithTimeout(ctx, 5*time.Second)
	boundPage := page.Context(fillCtx)
	if el, err := boundPage.Element(lwr.UsernameInputSelector); err == nil {
		_ = el.Input(username)
	}
	if el, err := boundPage.Element(lwr.PasswordInputSelector); err == nil {
		_ = el.Input(password)
	}
	fillCancel()

	submitSel := "button[type=submit], input[type=submit]"
	if lwr.FormSelector != "" {
		submitSel = lwr.FormSelector + " " + submitSel
	}
	submitCtx, submitCancel := context.WithTimeout(ctx, 5*time.Second)
	if el, err := page.Context(submitCtx).Element(submitSel); err == nil {
		_ = el.Click(proto.InputMouseButtonLeft, 1)
	}
	submitCancel()

	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	_ = page.Context(waitCtx).WaitLoad()
	waitCancel()
	// Do NOT sleep here — the mutex is held and any concurrent browser call would deadlock.
	// WaitLoad above already covers the post-submit page transition.

	tfa := Detect2FA(page)
	if tfa.Detected {
		return &LoginResult{
			TwoFARequired: true,
			TwoFASelector: tfa.InputSelector,
			Message:       tfa.Prompt,
		}
	}

	postLWR := DetectLoginWall(page)
	if postLWR.Detected {
		errMsg := extractPageErrorMessage(page)
		msg := fmt.Sprintf("Login failed for %s", host)
		if errMsg != "" {
			msg += ": " + errMsg
		}
		return &LoginResult{Message: msg}
	}

	info, _ := page.Info()
	m.persistSessionCookies(page, host, info.URL)

	return &LoginResult{
		Success: true,
		Message: fmt.Sprintf("Successfully logged in to %s.", host),
	}
}

// Submit2FA submits a 2FA code to the currently open 2FA form.
func (m *Manager) Submit2FA(ctx context.Context, code, selector string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.ensurePage()
	if err != nil {
		return err
	}

	if selector == "" {
		tfa := Detect2FA(page)
		if !tfa.Detected || tfa.InputSelector == "" {
			return fmt.Errorf("browser: no 2FA input detected on the current page")
		}
		selector = tfa.InputSelector
	}

	el, err := page.Context(ctx).Element(selector)
	if err != nil {
		return fmt.Errorf("browser: 2FA input %q not found: %w", selector, err)
	}
	if err := el.Input(code); err != nil {
		return fmt.Errorf("browser: type 2FA code: %w", err)
	}

	submitSel := "button[type=submit], input[type=submit]"
	if el, err := page.Context(ctx).Element(submitSel); err == nil {
		_ = el.Click(proto.InputMouseButtonLeft, 1)
		waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
		_ = page.Context(waitCtx).WaitLoad()
		waitCancel()
	}

	return nil
}

// ClearSession removes the stored cookie session for a host.
func (m *Manager) ClearSession(host string) error {
	return m.db.DeleteBrowserSession(host)
}

// ── Cookie persistence helpers ────────────────────────────────────────────────

// injectSessionCookies loads stored cookies for host and injects them into page.
// Must be called with m.mu held.
func (m *Manager) injectSessionCookies(page *rod.Page, host, pageURL string) {
	cookiesJSON, found, err := m.db.LoadBrowserSession(host)
	if err != nil || !found {
		return
	}
	var stored []StoredCookie
	if err := json.Unmarshal([]byte(cookiesJSON), &stored); err != nil {
		return
	}
	params := make([]*proto.NetworkCookieParam, 0, len(stored))
	for _, c := range stored {
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			URL:      pageURL,
		})
	}
	_ = page.SetCookies(params)
}

// persistSessionCookies saves current page cookies for host to the DB.
// Must be called with m.mu held.
func (m *Manager) persistSessionCookies(page *rod.Page, host, pageURL string) {
	cookies, err := page.Cookies([]string{pageURL})
	if err != nil || len(cookies) == 0 {
		return
	}
	stored := make([]StoredCookie, 0, len(cookies))
	for _, c := range cookies {
		sc := StoredCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   bool(c.Secure),
			HTTPOnly: bool(c.HTTPOnly),
			Expires:  float64(c.Expires),
		}
		stored = append(stored, sc)
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return
	}
	if err := m.db.SaveBrowserSession(host, string(data)); err != nil {
		log.Printf("browser: persistSessionCookies: failed to save session for %s (cookies will not survive restart): %v", host, err)
	}
}

// ── Browser lifecycle ─────────────────────────────────────────────────────────

// ensureBrowser lazily launches Chrome. Must be called with m.mu held.
func (m *Manager) ensureBrowser() error {
	if m.browser != nil {
		return nil
	}

	l := launcher.New().
		Headless(m.headless).
		Set("disable-blink-features", "AutomationControlled")

	u, err := l.Launch()
	if err != nil {
		return fmt.Errorf("browser: launch Chrome — install Google Chrome and try again: %w", err)
	}

	b := rod.New().ControlURL(u)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("browser: connect to Chrome: %w", err)
	}
	m.browser = b
	return nil
}

// ensurePage returns the active page, creating one if needed.
// Must be called with m.mu held.
func (m *Manager) ensurePage() (*rod.Page, error) {
	if err := m.ensureBrowser(); err != nil {
		return nil, err
	}
	if len(m.pages) == 0 {
		page, err := m.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("browser: create page: %w", err)
		}
		_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
			Width:             1280,
			Height:            800,
			DeviceScaleFactor: 1,
		})
		// Patch navigator.webdriver and other bot-detection signals.
		_, _ = page.Eval(stealth.JS)
		m.pages = append(m.pages, page)
		m.activeIdx = 0
	}
	return m.pages[m.activeIdx], nil
}

// activePage returns the current interaction target: the currentFrame when
// inside an iframe context, otherwise the active tab's page.
// Must be called with m.mu held.
func (m *Manager) activePage() (*rod.Page, error) {
	page, err := m.ensurePage()
	if err != nil {
		return nil, err
	}
	if m.currentFrame != nil {
		return m.currentFrame, nil
	}
	return page, nil
}

// ── URL helpers ───────────────────────────────────────────────────────────────

// ExtractHost is the exported version of extractHost for use by other packages.
func ExtractHost(rawURL string) string { return extractHost(rawURL) }

// extractHost parses the host from a URL string, stripping scheme, path, and port.
func extractHost(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	// Strip port.
	if i := strings.LastIndex(s, ":"); i >= 0 {
		port := s[i+1:]
		allDigits := len(port) > 0
		for _, ch := range port {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			s = s[:i]
		}
	}
	return strings.ToLower(s)
}

// extractPageErrorMessage looks for common login error messages on the page.
// Uses page.Has (non-blocking) to avoid retrying on missing elements.
func extractPageErrorMessage(page *rod.Page) string {
	errorSelectors := []string{
		".error-message", ".alert-danger", ".alert-error",
		"[role=alert]", ".notification-error", "#error-message",
		".flash-error", ".login-error",
	}
	for _, sel := range errorSelectors {
		if has, el, err := page.Has(sel); err == nil && has && el != nil {
			if text, err := el.Text(); err == nil && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}
