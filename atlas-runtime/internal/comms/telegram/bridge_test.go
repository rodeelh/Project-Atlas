package telegram

import (
	"net/http"
	"os"
	"testing"

	"atlas-runtime-go/internal/config"
)

// ── markdownToHTML ────────────────────────────────────────────────────────────

func TestMarkdownToHTML_Basic(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bold", "**hello**", "<b>hello</b>"},
		{"italic star", "*hello*", "<i>hello</i>"},
		{"italic underscore", "_hello_", "<i>hello</i>"},
		{"strikethrough", "~~hello~~", "<s>hello</s>"},
		{"inline code", "`code`", "<code>code</code>"},
		{"html escaping", "a < b & c > d", "a &lt; b &amp; c &gt; d"},
		{
			"fenced code block lowercase lang",
			"```go\nfmt.Println()\n```",
			"<pre>fmt.Println()</pre>",
		},
		{
			// FIX #9: uppercase language tag
			"fenced code block uppercase lang",
			"```Go\nfmt.Println()\n```",
			"<pre>fmt.Println()</pre>",
		},
		{
			// FIX #9: mixed-case language tag
			"fenced code block mixed lang",
			"```JavaScript\nconsole.log(1)\n```",
			"<pre>console.log(1)</pre>",
		},
		{
			"fenced code block no lang",
			"```\ncode here\n```",
			"<pre>code here</pre>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownToHTML(tc.input)
			if got != tc.want {
				t.Errorf("markdownToHTML(%q)\n  got  %q\n  want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ── stripHTML ─────────────────────────────────────────────────────────────────

func TestStripHTML(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"<b>hello</b>", "hello"},
		{"a &lt; b &amp; c &gt; d", "a < b & c > d"},
		{"plain text", "plain text"},
		{"<pre>code</pre>", "code"},
	}
	for _, tc := range cases {
		got := stripHTML(tc.input)
		if got != tc.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── chunkText ─────────────────────────────────────────────────────────────────

func TestChunkText(t *testing.T) {
	// Short text — single chunk.
	chunks := chunkText("hello", 100)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("expected single chunk, got %v", chunks)
	}

	// Exactly maxLen — single chunk.
	runes := make([]rune, 3500)
	for i := range runes {
		runes[i] = 'x'
	}
	exact := string(runes)
	chunks = chunkText(exact, 3500)
	if len(chunks) != 1 {
		t.Errorf("3500-rune string should be 1 chunk, got %d", len(chunks))
	}

	// One rune over — two chunks.
	over := string(append(runes, 'y'))
	chunks = chunkText(over, 3500)
	if len(chunks) != 2 {
		t.Errorf("3501-rune string should be 2 chunks, got %d", len(chunks))
	}
	if len([]rune(chunks[0])) != 3500 {
		t.Errorf("first chunk should be 3500 runes, got %d", len([]rune(chunks[0])))
	}
	if chunks[1] != "y" {
		t.Errorf("second chunk should be 'y', got %q", chunks[1])
	}
}

// ── sanitizeFilename ──────────────────────────────────────────────────────────

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello.jpg", "hello.jpg"},
		{"my/file.png", "my_file.png"},
		{"a:b*c?d.txt", "a_b_c_d.txt"},
		{"", "file"},
	}
	for _, tc := range cases {
		got := sanitizeFilename(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── mimeToExt ─────────────────────────────────────────────────────────────────

func TestMimeToExt(t *testing.T) {
	cases := map[string]string{
		"image/jpeg":      ".jpg",
		"image/png":       ".png",
		"application/pdf": ".pdf",
		"text/plain":      ".txt",
		"unknown/type":    "",
	}
	for mime, want := range cases {
		got := mimeToExt(mime)
		if got != want {
			t.Errorf("mimeToExt(%q) = %q, want %q", mime, got, want)
		}
	}
}

// ── extractFilePaths ──────────────────────────────────────────────────────────

func TestExtractFilePaths_OnlyExistingFiles(t *testing.T) {
	// Create a real temp file so os.Stat passes.
	tmp, err := os.CreateTemp("", "tg_test_*.png")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	text := "Here is your image: " + tmp.Name() + " and a missing /Users/nobody/nope.jpg"
	paths := extractFilePaths(text)

	if len(paths) != 1 {
		t.Errorf("expected 1 path (existing file only), got %v", paths)
		return
	}
	if paths[0] != tmp.Name() {
		t.Errorf("expected %q, got %q", tmp.Name(), paths[0])
	}
}

func TestExtractFilePaths_Deduplication(t *testing.T) {
	tmp, err := os.CreateTemp("", "tg_dedup_*.png")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	text := tmp.Name() + " and again " + tmp.Name()
	paths := extractFilePaths(text)
	if len(paths) != 1 {
		t.Errorf("expected 1 deduplicated path, got %v", paths)
	}
}

// ── isAllowed ─────────────────────────────────────────────────────────────────

func TestIsAllowed(t *testing.T) {
	b := &Bridge{}

	// Empty allowlists → allow all.
	cfg := config.RuntimeConfigSnapshot{}
	if !b.isAllowed(nil, 123, cfg) {
		t.Error("empty allowlist should allow all")
	}

	// Chat ID on allowlist.
	cfg = config.RuntimeConfigSnapshot{TelegramAllowedChatIDs: []int64{100, 200}}
	if !b.isAllowed(nil, 100, cfg) {
		t.Error("chat 100 should be allowed")
	}
	if b.isAllowed(nil, 999, cfg) {
		t.Error("chat 999 should be rejected")
	}

	// User ID on allowlist.
	user := &tgUser{ID: 42}
	cfg = config.RuntimeConfigSnapshot{TelegramAllowedUserIDs: []int64{42}}
	if !b.isAllowed(user, 999, cfg) {
		t.Error("user 42 should be allowed")
	}
	other := &tgUser{ID: 99}
	if b.isAllowed(other, 999, cfg) {
		t.Error("user 99 should be rejected")
	}

	// Nil from-user with user-only allowlist → deny (can't verify).
	if b.isAllowed(nil, 999, cfg) {
		t.Error("nil user with user-only allowlist should be rejected")
	}
}

// ── handleCallbackQuery routing ───────────────────────────────────────────────

func TestHandleCallbackQuery_ApproveRoute(t *testing.T) {
	var gotID string
	var gotApproved bool

	b := &Bridge{
		approvalResolver: func(id string, approved bool) error {
			gotID = id
			gotApproved = approved
			return nil
		},
		client: newNullHTTPClient(),
		token:  "TESTTOKEN",
	}

	q := tgCallbackQuery{
		ID:   "cbq1",
		From: tgUser{ID: 1},
		Data: "approve:tool-call-xyz",
	}
	b.handleCallbackQuery(q)

	if gotID != "tool-call-xyz" {
		t.Errorf("expected toolCallID %q, got %q", "tool-call-xyz", gotID)
	}
	if !gotApproved {
		t.Error("expected approved=true")
	}
}

func TestHandleCallbackQuery_DenyRoute(t *testing.T) {
	var gotApproved bool

	b := &Bridge{
		approvalResolver: func(id string, approved bool) error {
			gotApproved = approved
			return nil
		},
		client: newNullHTTPClient(),
		token:  "TESTTOKEN",
	}

	q := tgCallbackQuery{
		ID:   "cbq2",
		From: tgUser{ID: 1},
		Data: "deny:tool-call-abc",
	}
	b.handleCallbackQuery(q)

	if gotApproved {
		t.Error("expected approved=false for deny callback")
	}
}

func TestHandleCallbackQuery_UnknownDataIgnored(t *testing.T) {
	called := false
	b := &Bridge{
		approvalResolver: func(_ string, _ bool) error {
			called = true
			return nil
		},
		client: newNullHTTPClient(),
		token:  "TESTTOKEN",
	}

	b.handleCallbackQuery(tgCallbackQuery{ID: "x", Data: "unknown:data"})

	if called {
		t.Error("resolver should not be called for unknown callback data")
	}
}

func TestHandleCallbackQuery_NoResolver_NoPanic(t *testing.T) {
	b := &Bridge{
		client: newNullHTTPClient(),
		token:  "TESTTOKEN",
	}
	q := tgCallbackQuery{
		ID:   "cbq3",
		From: tgUser{ID: 1},
		Data: "approve:some-id",
		Message: &tgMessage{
			MessageID: 1,
			Chat:      tgChat{ID: 42},
		},
	}
	b.handleCallbackQuery(q) // must not panic
}

// ── reaction heuristics ───────────────────────────────────────────────────────

func TestReactWithLove(t *testing.T) {
	yes := []string{"thank you", "Thanks!", "AWESOME work", "you're the best", "🙏", "❤"}
	no := []string{"what time is it", "search the web", "omg no way"}
	for _, s := range yes {
		if !reactWithLove(s) {
			t.Errorf("reactWithLove(%q) should be true", s)
		}
	}
	for _, s := range no {
		if reactWithLove(s) {
			t.Errorf("reactWithLove(%q) should be false", s)
		}
	}
}

func TestReactWithShock(t *testing.T) {
	yes := []string{"omg", "no way!", "this is wild", "whoa that's crazy", "unbelievable"}
	no := []string{"thank you", "find my document", "hello"}
	for _, s := range yes {
		if !reactWithShock(s) {
			t.Errorf("reactWithShock(%q) should be true", s)
		}
	}
	for _, s := range no {
		if reactWithShock(s) {
			t.Errorf("reactWithShock(%q) should be false", s)
		}
	}
}

func TestReactWithProcessing(t *testing.T) {
	yes := []string{
		"search the web for news",
		"create a document",
		"generate an image",
		"find the file",
		"schedule a meeting",
		"run the automation",
	}
	no := []string{
		"what is the capital of France",
		"thank you so much",
		"how are you",
		"search",     // verb but no target
		"document",   // target but no verb
	}
	for _, s := range yes {
		if !reactWithProcessing(s) {
			t.Errorf("reactWithProcessing(%q) should be true", s)
		}
	}
	for _, s := range no {
		if reactWithProcessing(s) {
			t.Errorf("reactWithProcessing(%q) should be false", s)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// newNullHTTPClient returns an HTTP client that responds 200 OK to everything
// without making real network calls, so tests that trigger sendMessage /
// sendReaction / answerCallbackQuery don't hit the network.
func newNullHTTPClient() *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       http.NoBody,
				Header:     make(http.Header),
			}, nil
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
