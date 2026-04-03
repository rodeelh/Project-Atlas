// smoke_test.go exercises BrowserManager end-to-end using a real Chrome process.
//
// Tests are skipped unless the ATLAS_BROWSER_SMOKE environment variable is set,
// so they do not run in normal CI or `go test ./...` invocations.
//
//	ATLAS_BROWSER_SMOKE=1 go test ./internal/browser/ -v -run TestSmoke
package browser

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atlas-runtime-go/internal/storage"
)

// requireSmokeEnv skips the test unless ATLAS_BROWSER_SMOKE=1.
func requireSmokeEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("ATLAS_BROWSER_SMOKE") == "" {
		t.Skip("set ATLAS_BROWSER_SMOKE=1 to run browser smoke tests")
	}
}

// openTestDB opens a fresh in-memory SQLite DB for the duration of a test.
func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ── ExtractHost unit tests (no browser required) ──────────────────────────────

func TestExtractHost_Basic(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/login", "github.com"},
		{"http://mail.google.com/mail/u/0/", "mail.google.com"},
		{"https://example.com:8080/path?q=1", "example.com"},
		{"https://GITHUB.COM/", "github.com"},
		{"ftp://files.example.org", "files.example.org"},
		{"example.com", "example.com"},
		{"", ""},
	}
	for _, c := range cases {
		got := ExtractHost(c.in)
		if got != c.want {
			t.Errorf("ExtractHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── Cookie persistence unit tests (no browser required) ──────────────────────

func TestCookiePersistence_SaveAndLoad(t *testing.T) {
	db := openTestDB(t)

	const host = "example.com"
	payload := `[{"name":"session","value":"abc123","domain":"example.com","path":"/","secure":true,"httpOnly":true,"expires":0}]`

	if err := db.SaveBrowserSession(host, payload); err != nil {
		t.Fatalf("SaveBrowserSession: %v", err)
	}

	got, found, err := db.LoadBrowserSession(host)
	if err != nil {
		t.Fatalf("LoadBrowserSession: %v", err)
	}
	if !found {
		t.Fatal("expected session to be found after save")
	}
	if got != payload {
		t.Errorf("loaded payload mismatch\n got: %s\nwant: %s", got, payload)
	}
}

func TestCookiePersistence_Upsert(t *testing.T) {
	db := openTestDB(t)
	const host = "example.com"

	_ = db.SaveBrowserSession(host, `[{"name":"old","value":"v1"}]`)
	_ = db.SaveBrowserSession(host, `[{"name":"new","value":"v2"}]`)

	got, found, _ := db.LoadBrowserSession(host)
	if !found {
		t.Fatal("session not found after upsert")
	}
	if !strings.Contains(got, "v2") {
		t.Errorf("upsert did not overwrite: %s", got)
	}
}

func TestCookiePersistence_Delete(t *testing.T) {
	db := openTestDB(t)
	const host = "example.com"

	_ = db.SaveBrowserSession(host, `[]`)
	if err := db.DeleteBrowserSession(host); err != nil {
		t.Fatalf("DeleteBrowserSession: %v", err)
	}
	_, found, err := db.LoadBrowserSession(host)
	if err != nil {
		t.Fatalf("LoadBrowserSession after delete: %v", err)
	}
	if found {
		t.Error("session should not be found after delete")
	}
}

func TestCookiePersistence_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, found, err := db.LoadBrowserSession("neverused.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected not found for missing host")
	}
}

// ── Browser smoke tests (require Chrome + ATLAS_BROWSER_SMOKE=1) ─────────────

func newSmokeManager(t *testing.T) *Manager {
	t.Helper()
	db := openTestDB(t)
	mgr := New(db, true) // headless=true for testing
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

func TestSmoke_Navigate(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.Title == "" {
		t.Error("expected non-empty page title")
	}
	if !strings.Contains(strings.ToLower(result.Title), "example") {
		t.Logf("title: %q (may not contain 'example', that's ok)", result.Title)
	}
	if result.LoginWallDetected {
		t.Error("example.com should not trigger login wall detection")
	}
	t.Logf("Navigated: URL=%s Title=%q", result.URL, result.Title)
}

func TestSmoke_Screenshot(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	img, err := mgr.Screenshot(ctx, false, "")
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(img) == 0 {
		t.Fatal("screenshot returned empty bytes")
	}
	// PNG magic bytes: 0x89 0x50 0x4E 0x47
	if !bytes.HasPrefix(img, []byte{0x89, 0x50, 0x4E, 0x47}) {
		t.Errorf("screenshot is not a PNG (first bytes: %x)", img[:min(4, len(img))])
	}
	t.Logf("Screenshot: %d bytes (PNG)", len(img))
}

func TestSmoke_ReadPage(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	text, err := mgr.ReadPage(ctx, "", 0)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(text) == 0 {
		t.Fatal("ReadPage returned empty text")
	}
	if !strings.Contains(text, "Example") {
		t.Errorf("expected 'Example' in page text, got: %q", text[:min(200, len(text))])
	}
	t.Logf("ReadPage: %d chars", len(text))
}

func TestSmoke_FindElement(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	text, err := mgr.FindElement(ctx, "h1", "")
	if err != nil {
		t.Fatalf("FindElement h1: %v", err)
	}
	if text == "" {
		t.Error("h1 text should not be empty")
	}
	t.Logf("h1 text: %q", text)

	// Test attribute retrieval.
	href, err := mgr.FindElement(ctx, "a", "href")
	if err != nil {
		t.Fatalf("FindElement a[href]: %v", err)
	}
	t.Logf("first link href: %q", href)
}

func TestSmoke_CookiePersistenceAcrossNavigation(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Navigate to a site — cookies are saved after navigation.
	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Confirm SessionCheck sees the saved session.
	fresh, _, err := mgr.SessionCheck(ctx, "example.com")
	if err != nil {
		t.Fatalf("SessionCheck: %v", err)
	}
	// example.com doesn't set real session cookies, so fresh may be false,
	// but the call itself should not error.
	t.Logf("SessionCheck fresh=%v", fresh)
}

func TestSmoke_LoginWallDetection_NoWall(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.LoginWallDetected {
		t.Error("example.com should not have a login wall")
	}
}

func TestSmoke_LoginWallDetection_WithWall(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// GitHub's login page is a reliable login wall target.
	result, err := mgr.Navigate(ctx, "https://github.com/login", "", 0)
	if err != nil {
		t.Fatalf("Navigate to github.com/login: %v", err)
	}
	if !result.LoginWallDetected {
		t.Error("github.com/login should be detected as a login wall")
	}
	if result.LoginWallInfo == nil {
		t.Fatal("LoginWallInfo should not be nil when login wall detected")
	}
	if result.LoginWallInfo.PasswordInputSelector == "" {
		t.Error("password input selector should be populated on login page")
	}
	t.Logf("Login wall detected: username=%q password=%q form=%q",
		result.LoginWallInfo.UsernameInputSelector,
		result.LoginWallInfo.PasswordInputSelector,
		result.LoginWallInfo.FormSelector)
}

func TestSmoke_Scroll(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := mgr.Scroll(ctx, "down", 200, ""); err != nil {
		t.Errorf("Scroll down: %v", err)
	}
	if err := mgr.Scroll(ctx, "up", 200, ""); err != nil {
		t.Errorf("Scroll up: %v", err)
	}
}

func TestSmoke_WaitForElement(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mgr.Navigate(ctx, "https://example.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	// h1 should already be present; WaitForElement should return immediately.
	if err := mgr.WaitForElement(ctx, "h1", 5000); err != nil {
		t.Errorf("WaitForElement h1: %v", err)
	}
	// Non-existent element should time out.
	if err := mgr.WaitForElement(ctx, "#definitely-does-not-exist-xyz", 1000); err == nil {
		t.Error("expected error waiting for non-existent element")
	}
}

func TestSmoke_TypeAndClick(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Use DuckDuckGo search as a safe typing + click target.
	_, err := mgr.Navigate(ctx, "https://duckduckgo.com", "", 0)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Type into the search box.
	if err := mgr.TypeText(ctx, `input[name="q"]`, "atlas agent test", true); err != nil {
		t.Fatalf("TypeText: %v", err)
	}
	t.Log("TypeText: OK")

	// Click the search button.
	if err := mgr.Click(ctx, `input[type="submit"], button[type="submit"]`, 1000); err != nil {
		t.Logf("Click submit (non-fatal, DDG may use different selector): %v", err)
	}
}

func TestSmoke_LoginAttemptCeiling(t *testing.T) {
	requireSmokeEnv(t)
	db := openTestDB(t)
	mgr := New(db, true)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	_, _ = mgr.Navigate(ctx, "https://github.com/login", "", 0)

	// First attempt.
	r1 := mgr.AutoLogin(ctx, "github.com", "testuser@example.com", "wrongpassword")
	t.Logf("Attempt 1: success=%v twoFA=%v msg=%q", r1.Success, r1.TwoFARequired, r1.Message)

	// Second attempt.
	r2 := mgr.AutoLogin(ctx, "github.com", "testuser@example.com", "wrongpassword")
	t.Logf("Attempt 2: success=%v twoFA=%v msg=%q", r2.Success, r2.TwoFARequired, r2.Message)

	// Third attempt — should be blocked by the ceiling.
	r3 := mgr.AutoLogin(ctx, "github.com", "testuser@example.com", "wrongpassword")
	if r3.Success {
		t.Error("third attempt with wrong credentials should not succeed")
	}
	if !strings.Contains(r3.Message, "already been attempted") {
		t.Errorf("expected ceiling message on third attempt, got: %q", r3.Message)
	}
	t.Logf("Attempt 3 (ceiling): %q", r3.Message)
}

// ── New feature smoke tests ───────────────────────────────────────────────────

func TestSmoke_TabsNewAndSwitch(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initial navigation creates first tab implicitly.
	if _, err := mgr.Navigate(ctx, "https://example.com", "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Open a second tab.
	idx, err := mgr.TabsNew(ctx, "https://example.org")
	if err != nil {
		t.Fatalf("TabsNew: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected new tab index 1, got %d", idx)
	}

	tabs, err := mgr.TabsList()
	if err != nil {
		t.Fatalf("TabsList: %v", err)
	}
	if len(tabs) != 2 {
		t.Errorf("expected 2 tabs, got %d", len(tabs))
	}
	if !tabs[1].IsActive {
		t.Error("new tab should be active")
	}
	t.Logf("Tab 0: %s | Tab 1: %s (active)", tabs[0].URL, tabs[1].URL)

	// Switch back to tab 0.
	if err := mgr.TabsSwitch(0); err != nil {
		t.Fatalf("TabsSwitch(0): %v", err)
	}
	tabs, _ = mgr.TabsList()
	if !tabs[0].IsActive {
		t.Error("tab 0 should be active after switch")
	}

	// Read page from tab 0 — should be example.com content.
	text, err := mgr.ReadPage(ctx, "", 0)
	if err != nil {
		t.Fatalf("ReadPage on tab 0: %v", err)
	}
	if !strings.Contains(text, "Example") {
		t.Errorf("expected Example Domain in tab 0 content, got: %q", text[:min(200, len(text))])
	}
}

func TestSmoke_TabsClose(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, "https://example.com", "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := mgr.TabsNew(ctx, ""); err != nil {
		t.Fatalf("TabsNew: %v", err)
	}

	// Cannot close last tab — but we have 2 so closing tab 1 should work.
	if err := mgr.TabsClose(1); err != nil {
		t.Fatalf("TabsClose(1): %v", err)
	}
	tabs, _ := mgr.TabsList()
	if len(tabs) != 1 {
		t.Errorf("expected 1 tab after close, got %d", len(tabs))
	}
	if !tabs[0].IsActive {
		t.Error("tab 0 should be active after closing tab 1")
	}

	// Attempting to close the last tab should error.
	if err := mgr.TabsClose(0); err == nil {
		t.Error("expected error closing last tab")
	}
}

func TestSmoke_Eval(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, "https://example.com", "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Evaluate a simple expression.
	result, err := mgr.Eval(ctx, `() => document.title`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if result == "" || result == "null" {
		t.Errorf("expected non-empty title from Eval, got %q", result)
	}
	t.Logf("Eval document.title: %s", result)

	// Evaluate arithmetic.
	num, err := mgr.Eval(ctx, `() => 6 * 7`)
	if err != nil {
		t.Fatalf("Eval arithmetic: %v", err)
	}
	if num != "42" {
		t.Errorf("expected '42' from 6*7, got %q", num)
	}
}

func TestSmoke_WaitNetworkIdle(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, "https://example.com", "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	// Page is already loaded — WaitNetworkIdle should return quickly.
	if err := mgr.WaitNetworkIdle(ctx, 5000); err != nil {
		t.Errorf("WaitNetworkIdle: %v", err)
	}
}

func TestSmoke_SwitchFrameAndMainFrame(t *testing.T) {
	requireSmokeEnv(t)
	mgr := newSmokeManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// w3schools.com has iframes on some pages; use a simpler data: URI with an
	// inline iframe for a reliable test without network dependencies.
	html := `data:text/html,<html><body><iframe id="f1" src="data:text/html,<p>inside frame</p>"></iframe></body></html>`
	if _, err := mgr.Navigate(ctx, html, "", 0); err != nil {
		t.Fatalf("Navigate to data URI: %v", err)
	}

	// Switching to the iframe should succeed.
	if err := mgr.SwitchFrame(ctx, "#f1"); err != nil {
		t.Fatalf("SwitchFrame: %v", err)
	}
	t.Log("SwitchFrame: OK")

	// Return to main frame.
	mgr.SwitchMainFrame()
	t.Log("SwitchMainFrame: OK")

	// Read from main frame should work normally.
	text, err := mgr.ReadPage(ctx, "", 0)
	if err != nil {
		t.Fatalf("ReadPage after SwitchMainFrame: %v", err)
	}
	t.Logf("Main frame text after return: %q", text[:min(100, len(text))])
}
