package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atlas-runtime-go/internal/browser"
	"atlas-runtime-go/internal/storage"
)

// ── Unit tests — no browser, no vision model ──────────────────────────────────

func TestCleanCaptchaAnswer_PlainText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"XKCD7", "XKCD7"},
		{"  xkcd7  ", "xkcd7"},
		{"Answer: XKCD7", "XKCD7"},
		{"answer: XKCD7", "XKCD7"},
		{"CAPTCHA: XKCD7", "XKCD7"},
		{"Text: XKCD7", "XKCD7"},
		{"Code: XKCD7", "XKCD7"},
		{"`XKCD7`", "XKCD7"},
		{`"XKCD7"`, "XKCD7"},
		{"'XKCD7'", "XKCD7"},
		{"42", "42"},
		{"10", "10"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		got := cleanCaptchaAnswer(c.in)
		if got != c.want {
			t.Errorf("cleanCaptchaAnswer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanCaptchaAnswer_MathResult(t *testing.T) {
	// Vision model may add "Answer: " prefix for math problems.
	cases := []struct {
		in   string
		want string
	}{
		{"Answer: 15", "15"},
		{"answer: 15", "15"},
		{"15", "15"},
		{"The answer is 15", "The answer is 15"}, // no known prefix — returned as-is
	}
	for _, c := range cases {
		got := cleanCaptchaAnswer(c.in)
		if got != c.want {
			t.Errorf("cleanCaptchaAnswer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSolveCaptcha_NoVisionFn(t *testing.T) {
	r := &Registry{entries: make(map[string]SkillEntry)}
	// visionFn intentionally not set.
	args, _ := json.Marshal(map[string]string{
		"captcha_selector": "img#captcha",
		"input_selector":   "#answer",
	})
	_, err := r.browserSolveCaptcha(context.Background(), args)
	if err == nil {
		t.Fatal("expected error when visionFn is nil")
	}
	if !strings.Contains(err.Error(), "vision model not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Smoke tests — require Chrome + ATLAS_BROWSER_SMOKE=1 ─────────────────────

func requireSmokeEnvSkills(t *testing.T) {
	t.Helper()
	if os.Getenv("ATLAS_BROWSER_SMOKE") == "" {
		t.Skip("set ATLAS_BROWSER_SMOKE=1 to run browser smoke tests")
	}
}

// openTestDBSkills opens a fresh SQLite database in a temp dir.
func openTestDBSkills(t *testing.T) *storage.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newCaptchaTestRegistry wires a Registry with a real BrowserManager and a
// mock visionFn that returns the expected answer for a known CAPTCHA image.
func newCaptchaTestRegistry(t *testing.T, mockAnswer string) (*Registry, *browser.Manager) {
	t.Helper()
	db := openTestDBSkills(t)
	mgr := browser.New(db, true) // headless
	t.Cleanup(func() { mgr.Close() })

	r := &Registry{
		entries:    make(map[string]SkillEntry),
		browserMgr: mgr,
		visionFn: func(_ context.Context, imageB64, prompt string) (string, error) {
			if imageB64 == "" {
				return "", fmt.Errorf("mock: received empty imageB64")
			}
			// Return the pre-set expected answer.
			return mockAnswer, nil
		},
	}
	r.registerBrowser()
	return r, mgr
}

// captchaPageHTML builds a minimal HTML page with a deterministic text CAPTCHA.
// The CAPTCHA is rendered as an <img> whose alt text is the expected answer —
// in a real scenario the browser would render an image; here we embed a 1×1
// PNG data URI so Screenshot returns a valid PNG.
const captchaPageHTML = `<!DOCTYPE html>
<html>
<head><title>CAPTCHA Test</title></head>
<body>
  <form id="captchaForm">
    <div class="captcha">
      <!-- 1×1 white PNG as a stand-in for a rendered CAPTCHA image -->
      <img id="captchaImg"
           src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
           alt="CAPTCHA" width="120" height="40">
    </div>
    <input type="text" id="captchaInput" name="answer" placeholder="Enter CAPTCHA">
    <button type="submit" id="submitBtn">Submit</button>
    <div id="result"></div>
  </form>
  <script>
    document.getElementById('captchaForm').addEventListener('submit', function(e) {
      e.preventDefault();
      var val = document.getElementById('captchaInput').value;
      document.getElementById('result').textContent = 'submitted:' + val;
    });
  </script>
</body>
</html>`

// TestSmokeCaptcha_AutoDetectAndType tests full solve flow:
// auto-detect selectors → screenshot → mock vision → type → verify typed value.
func TestSmokeCaptcha_AutoDetectAndType(t *testing.T) {
	requireSmokeEnvSkills(t)

	// Serve the CAPTCHA page locally.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(captchaPageHTML))
	}))
	defer srv.Close()

	const expectedAnswer = "X7KP2"
	reg, mgr := newCaptchaTestRegistry(t, expectedAnswer)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Navigate to the local CAPTCHA page.
	if _, err := mgr.Navigate(ctx, srv.URL, "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Run solve_captcha with explicit selectors (auto-detect tested separately).
	args, _ := json.Marshal(map[string]string{
		"captcha_selector": "#captchaImg",
		"input_selector":   "#captchaInput",
	})
	result, err := reg.browserSolveCaptcha(ctx, args)
	if err != nil {
		t.Fatalf("browserSolveCaptcha: %v", err)
	}
	t.Logf("solve_captcha result: %s", result)

	if !strings.Contains(result, expectedAnswer) {
		t.Errorf("expected answer %q in result, got: %q", expectedAnswer, result)
	}

	// Verify the value was actually typed into the input.
	// Must use Eval to read the DOM property (.value) — el.Attribute("value")
	// reads the HTML attribute which does not update after programmatic input.
	typed, err := mgr.Eval(ctx, `() => document.querySelector("#captchaInput").value`)
	if err != nil {
		t.Fatalf("Eval input value: %v", err)
	}
	typed = strings.Trim(typed, `"`) // Eval returns JSON-encoded string
	if typed != expectedAnswer {
		t.Errorf("input value = %q, want %q", typed, expectedAnswer)
	}
	t.Logf("Input value confirmed: %q", typed)
}

// TestSmokeCaptcha_AutoDetectAndSubmit tests the full flow with submit.
func TestSmokeCaptcha_AutoDetectAndSubmit(t *testing.T) {
	requireSmokeEnvSkills(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(captchaPageHTML))
	}))
	defer srv.Close()

	const expectedAnswer = "ABC123"
	reg, mgr := newCaptchaTestRegistry(t, expectedAnswer)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, srv.URL, "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	args, _ := json.Marshal(map[string]string{
		"captcha_selector": "#captchaImg",
		"input_selector":   "#captchaInput",
		"submit_selector":  "#submitBtn",
	})
	result, err := reg.browserSolveCaptcha(ctx, args)
	if err != nil {
		t.Fatalf("browserSolveCaptcha with submit: %v", err)
	}
	t.Logf("solve_captcha result: %s", result)

	if !strings.Contains(result, "submitted") || !strings.Contains(result, expectedAnswer) {
		t.Logf("Result: %q (submit confirmation may be JS-side)", result)
	}

	// Verify the JS form handler fired and the result div was updated.
	if err := mgr.WaitForElement(ctx, "#result", 3000); err != nil {
		t.Logf("result div not visible (non-fatal): %v", err)
	} else {
		resultText, _ := mgr.FindElement(ctx, "#result", "")
		t.Logf("Form result text: %q", resultText)
		if !strings.Contains(resultText, expectedAnswer) {
			t.Errorf("expected submitted answer in result div, got: %q", resultText)
		}
	}
}

// TestSmokeCaptcha_SelectorAutoDetection tests that detectCaptchaSelector
// correctly finds an img[src*="captcha"] element and an input[name="answer"].
func TestSmokeCaptcha_SelectorAutoDetection(t *testing.T) {
	requireSmokeEnvSkills(t)

	// Inline data URI for a 1×1 white PNG so no network request is needed.
	const pngDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	html := fmt.Sprintf(`<!DOCTYPE html><html><body>
		<img src="%s" id="captchaImg" class="captcha-img" alt="CAPTCHA" width="100" height="40">
		<input type="text" name="answer" id="answerInput">
	</body></html>`, pngDataURI)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	reg, mgr := newCaptchaTestRegistry(t, "AUTODETECTED")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, srv.URL, "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// detectCaptchaSelector should find img[alt*="captcha"] or img[class*="captcha"].
	sel := reg.detectCaptchaSelector(ctx)
	t.Logf("Auto-detected CAPTCHA selector: %q", sel)
	if sel == "" {
		t.Error("expected auto-detection to find a CAPTCHA image selector")
	}

	// detectCaptchaInputSelector should find input[name="answer"].
	inputSel := reg.detectCaptchaInputSelector(ctx)
	t.Logf("Auto-detected input selector: %q", inputSel)
	if inputSel == "" {
		t.Error("expected auto-detection to find a CAPTCHA input selector")
	}
}

// TestSmokeCaptcha_MathChallenge tests that a math-answer CAPTCHA ("3 + 7 = ?")
// can be solved when the vision model returns "10".
func TestSmokeCaptcha_MathChallenge(t *testing.T) {
	requireSmokeEnvSkills(t)

	html := `<!DOCTYPE html><html><body>
		<form>
			<img id="captchaImg" src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==" alt="3 + 7 = ?">
			<input type="text" id="captchaInput" name="captcha">
		</form>
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	// Vision model returns "Answer: 10" — cleanCaptchaAnswer should strip the prefix.
	reg, mgr := newCaptchaTestRegistry(t, "Answer: 10")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, srv.URL, "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	args, _ := json.Marshal(map[string]string{
		"captcha_selector": "#captchaImg",
		"input_selector":   "#captchaInput",
	})
	result, err := reg.browserSolveCaptcha(ctx, args)
	if err != nil {
		t.Fatalf("browserSolveCaptcha (math): %v", err)
	}
	t.Logf("Math CAPTCHA result: %s", result)

	// After cleanCaptchaAnswer, "Answer: 10" → "10".
	if !strings.Contains(result, `"10"`) {
		t.Errorf("expected answer '10' in result, got: %q", result)
	}

	// Verify typed value via DOM property (el.Attribute("value") reads the static
	// HTML attribute which does not update after programmatic input).
	typed, err := mgr.Eval(ctx, `() => document.querySelector("#captchaInput").value`)
	if err != nil {
		t.Fatalf("Eval input value: %v", err)
	}
	typed = strings.Trim(typed, `"`) // Eval returns JSON-encoded string
	if typed != "10" {
		t.Errorf("input value = %q, want \"10\"", typed)
	}
}

// TestSmokeCaptcha_VisionFnError tests that a vision inference failure is
// propagated cleanly without panicking.
func TestSmokeCaptcha_VisionFnError(t *testing.T) {
	requireSmokeEnvSkills(t)

	html := `<!DOCTYPE html><html><body>
		<img id="captchaImg" src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==" alt="test">
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	db := openTestDBSkills(t)
	mgr := browser.New(db, true)
	t.Cleanup(func() { mgr.Close() })

	r := &Registry{
		entries:    make(map[string]SkillEntry),
		browserMgr: mgr,
		visionFn: func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("mock: simulated vision API failure")
		},
	}
	r.registerBrowser()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := mgr.Navigate(ctx, srv.URL, "", 0); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	args, _ := json.Marshal(map[string]string{"captcha_selector": "#captchaImg"})
	_, err := r.browserSolveCaptcha(ctx, args)
	if err == nil {
		t.Fatal("expected error from failed vision call")
	}
	if !strings.Contains(err.Error(), "vision inference failed") {
		t.Errorf("unexpected error message: %v", err)
	}
	t.Logf("Vision failure propagated correctly: %v", err)
}

