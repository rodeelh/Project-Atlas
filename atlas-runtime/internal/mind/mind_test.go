package mind

import (
	"strings"
	"testing"
	"time"
)

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate_BelowLimit(t *testing.T) {
	s := "hello world"
	got := truncate(s, 100)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncate_AtLimit(t *testing.T) {
	s := "hello"
	got := truncate(s, 5)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncate_AboveLimit(t *testing.T) {
	s := "hello world"
	got := truncate(s, 5)
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestTruncate_MultibyteRune(t *testing.T) {
	// Each rune is 3 bytes in UTF-8; byte-level truncation would split them.
	s := "日本語テスト"
	got := truncate(s, 3)
	if got != "日本語" {
		t.Errorf("expected %q, got %q", "日本語", got)
	}
	if strings.Contains(got, "\xef") {
		t.Error("truncation split a multi-byte rune")
	}
}

// ── truncateSandwich ──────────────────────────────────────────────────────────

func TestTruncateSandwich_BelowLimit(t *testing.T) {
	s := "short text"
	got := truncateSandwich(s, 100)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncateSandwich_AboveLimit(t *testing.T) {
	// 20 runes of 'A' + 20 runes of 'B', cap at 10 → first 5 'A' + sep + last 5 'B'
	s := strings.Repeat("A", 20) + strings.Repeat("B", 20)
	got := truncateSandwich(s, 10)
	if !strings.HasPrefix(got, "AAAAA") {
		t.Errorf("expected prefix 'AAAAA', got %q", got[:10])
	}
	if !strings.HasSuffix(got, "BBBBB") {
		t.Errorf("expected suffix 'BBBBB', got %q", got[len(got)-10:])
	}
	if !strings.Contains(got, "truncated") {
		t.Error("expected ellipsis marker in output")
	}
}

// ── shortID ───────────────────────────────────────────────────────────────────

func TestShortID_Long(t *testing.T) {
	got := shortID("abcdefghij")
	if got != "abcdefgh" {
		t.Errorf("expected %q, got %q", "abcdefgh", got)
	}
}

func TestShortID_Short(t *testing.T) {
	got := shortID("abc")
	if got != "abc" {
		t.Errorf("expected %q, got %q", "abc", got)
	}
}

// ── validateMindContent ───────────────────────────────────────────────────────

func TestValidateMindContent_Valid(t *testing.T) {
	content := "# Mind of Atlas\n\nSome content here."
	if err := validateMindContent(content); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateMindContent_WrongHeader(t *testing.T) {
	content := "# Something Else\n\nContent."
	if err := validateMindContent(content); err == nil {
		t.Error("expected error for wrong header, got nil")
	}
}

func TestValidateMindContent_TooLarge(t *testing.T) {
	content := "# Mind of Atlas\n" + strings.Repeat("x", maxFileSize)
	if err := validateMindContent(content); err == nil {
		t.Error("expected error for oversized content, got nil")
	}
}

// ── validateSkillsContent ─────────────────────────────────────────────────────

func TestValidateSkillsContent_Valid(t *testing.T) {
	content := "# Skill Memory\n\nSome content here."
	if err := validateSkillsContent(content); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateSkillsContent_WrongHeader(t *testing.T) {
	content := "# Wrong Header\n\nContent."
	if err := validateSkillsContent(content); err == nil {
		t.Error("expected error for wrong header, got nil")
	}
}

// ── replaceTodaysRead ─────────────────────────────────────────────────────────

func TestReplaceTodaysRead_SectionPresent(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n\n## Today's Read\n\nOld read.\n"
	got := replaceTodaysRead(mind, "New read.")
	if !strings.Contains(got, "New read.") {
		t.Error("expected 'New read.' in output")
	}
	if strings.Contains(got, "Old read.") {
		t.Error("expected 'Old read.' to be replaced")
	}
	if !strings.Contains(got, "## Who I Am") {
		t.Error("expected other sections to be preserved")
	}
}

func TestReplaceTodaysRead_SectionMissing(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n"
	got := replaceTodaysRead(mind, "New read.")
	if !strings.Contains(got, "## Today's Read") {
		t.Error("expected '## Today's Read' to be appended")
	}
	if !strings.Contains(got, "New read.") {
		t.Error("expected 'New read.' in output")
	}
}

func TestReplaceTodaysRead_NoFalsePositive(t *testing.T) {
	// "## Today's Read" embedded in body text must NOT trigger a mis-splice.
	mind := "# Mind of Atlas\n\n## Patterns I've Noticed\n\nI mentioned ## Today's Read in a note.\n\n## Today's Read\n\nOld.\n"
	got := replaceTodaysRead(mind, "New.")
	// The Patterns section should still contain its body text.
	if !strings.Contains(got, "I mentioned ## Today's Read in a note.") {
		t.Error("embedded marker in body was incorrectly treated as a section heading")
	}
}

// ── extractTodaysRead ─────────────────────────────────────────────────────────

func TestExtractTodaysRead_Present(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Today's Read\n\nThe content here.\n\n## Next Section\n\nOther.\n"
	got := extractTodaysRead(mind)
	if got != "The content here." {
		t.Errorf("expected %q, got %q", "The content here.", got)
	}
}

func TestExtractTodaysRead_LastSection(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Today's Read\n\nFinal content.\n"
	got := extractTodaysRead(mind)
	if got != "Final content." {
		t.Errorf("expected %q, got %q", "Final content.", got)
	}
}

func TestExtractTodaysRead_Missing(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n"
	got := extractTodaysRead(mind)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ── updateReflectionDate ──────────────────────────────────────────────────────

func TestUpdateReflectionDate_Replaces(t *testing.T) {
	mind := "# Mind of Atlas\n\n_Last deep reflection: 2000-01-01_\n\n## Who I Am\n\nIdentity.\n"
	got := updateReflectionDate(mind)
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(got, today) {
		t.Errorf("expected today's date %q in output", today)
	}
	if strings.Contains(got, "2000-01-01") {
		t.Error("expected old date to be replaced")
	}
}

func TestUpdateReflectionDate_Missing(t *testing.T) {
	mind := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n"
	got := updateReflectionDate(mind)
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(got, today) {
		t.Errorf("expected today's date %q inserted into output", today)
	}
}

// ── updateSkillsDate ──────────────────────────────────────────────────────────

func TestUpdateSkillsDate_Replaces(t *testing.T) {
	skills := "# Skill Memory\n\n_Last updated: 2000-01-01_\n\n## Orchestration Principles\n\nRules.\n"
	got := updateSkillsDate(skills)
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(got, today) {
		t.Errorf("expected today's date %q in output", today)
	}
	if strings.Contains(got, "2000-01-01") {
		t.Error("expected old date to be replaced")
	}
}

// ── dedupeSkills ─────────────────────────────────────────────────────────────

func TestDedupeSkills_Order(t *testing.T) {
	input := []string{"web.search", "filesystem.read", "web.search", "system.info"}
	want := []string{"web.search", "filesystem.read", "system.info"}
	got := dedupeSkills(input)
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestDedupeSkills_Empty(t *testing.T) {
	got := dedupeSkills(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ── parseRoutines ─────────────────────────────────────────────────────────────

func TestParseRoutines_Basic(t *testing.T) {
	content := `# Skill Memory

## Learned Routines

### Morning Briefing
**Triggers:** morning briefing, daily update
**Steps:**
1. weather.current → get weather (location: home)
2. web.search → search news (query: top headlines)
**Learned:** 2026-01-01 — repeated pattern
`
	routines := parseRoutines(content)
	if len(routines) != 1 {
		t.Fatalf("expected 1 routine, got %d", len(routines))
	}
	r := routines[0]
	if r.name != "Morning Briefing" {
		t.Errorf("expected name 'Morning Briefing', got %q", r.name)
	}
	if len(r.triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d: %v", len(r.triggers), r.triggers)
	}
	if len(r.steps) != 2 {
		t.Errorf("expected 2 steps, got %d: %v", len(r.steps), r.steps)
	}
}

func TestParseRoutines_StepParserNoFalsePositive(t *testing.T) {
	// "e.g. something" and "web.search → result" must NOT be parsed as steps.
	content := `# Skill Memory

## Learned Routines

### Tricky Routine
**Triggers:** test
**Steps:**
1. real.step → action (param)
e.g. this is not a step
web.search → also not a step number
**Learned:** 2026-01-01 — test
`
	routines := parseRoutines(content)
	if len(routines) != 1 {
		t.Fatalf("expected 1 routine, got %d", len(routines))
	}
	r := routines[0]
	if len(r.steps) != 1 {
		t.Errorf("expected 1 step, got %d: %v", len(r.steps), r.steps)
	}
}

// ── buildDiaryEntry ───────────────────────────────────────────────────────────

func TestBuildDiaryEntry_Basic(t *testing.T) {
	turn := TurnRecord{
		UserMessage:       "What is the weather today?",
		ToolCallSummaries: []string{"weather.current", "info.version"},
	}
	entry := buildDiaryEntry(turn)
	if !strings.Contains(entry, "What is the weather today?") {
		t.Error("expected user message in diary entry")
	}
	if !strings.Contains(entry, "[weather.current, info.version]") {
		t.Error("expected tool names in diary entry")
	}
}

func TestBuildDiaryEntry_LongMessage(t *testing.T) {
	turn := TurnRecord{
		UserMessage: strings.Repeat("word ", 50), // 250 runes
	}
	entry := buildDiaryEntry(turn)
	if len([]rune(entry)) > 200 { // 80 user chars + possible tools
		t.Errorf("diary entry too long: %d runes", len([]rune(entry)))
	}
}

func TestBuildDiaryEntry_NoTools(t *testing.T) {
	turn := TurnRecord{UserMessage: "Hello Atlas"}
	entry := buildDiaryEntry(turn)
	if strings.Contains(entry, "[") {
		t.Error("expected no brackets when no tools used")
	}
}

// ── parseSections ─────────────────────────────────────────────────────────────

func TestParseSections_Basic(t *testing.T) {
	content := "# Title\n\n## Section A\n\nBody A.\n\n## Section B\n\nBody B.\n"
	got := parseSections(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 sections, got %d: %v", len(got), got)
	}
	if got["## Section A"] != "Body A." {
		t.Errorf("Section A: expected %q, got %q", "Body A.", got["## Section A"])
	}
	if got["## Section B"] != "Body B." {
		t.Errorf("Section B: expected %q, got %q", "Body B.", got["## Section B"])
	}
}

func TestParseSections_Empty(t *testing.T) {
	got := parseSections("No sections here.")
	if len(got) != 0 {
		t.Errorf("expected 0 sections, got %d", len(got))
	}
}

func TestParseSections_MultilineBody(t *testing.T) {
	content := "## My Section\n\nLine 1.\nLine 2.\nLine 3.\n"
	got := parseSections(content)
	body := got["## My Section"]
	if !strings.Contains(body, "Line 1.") || !strings.Contains(body, "Line 3.") {
		t.Errorf("expected multiline body, got %q", body)
	}
}

// ── mergeMindSections ─────────────────────────────────────────────────────────

func TestMergeMindSections_UpdatesMatchedSection(t *testing.T) {
	existing := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n\n## Our Story\n\nOld story.\n\n## Patterns I've Noticed\n\nOld patterns.\n"
	patch := "## Our Story\n\nNew story about what happened.\n"

	got := mergeMindSections(existing, patch)
	if !strings.Contains(got, "New story about what happened.") {
		t.Error("expected patched Our Story section")
	}
	if strings.Contains(got, "Old story.") {
		t.Error("expected old Our Story to be replaced")
	}
	if !strings.Contains(got, "Identity.") {
		t.Error("expected Who I Am to be preserved")
	}
	if !strings.Contains(got, "Old patterns.") {
		t.Error("expected Patterns section to be preserved")
	}
}

func TestMergeMindSections_ProtectsWhoIAm(t *testing.T) {
	existing := "# Mind of Atlas\n\n## Who I Am\n\nOriginal identity.\n\n## Our Story\n\nStory.\n"
	patch := "## Who I Am\n\nHijacked identity.\n\n## Our Story\n\nNew story.\n"

	got := mergeMindSections(existing, patch)
	if !strings.Contains(got, "Original identity.") {
		t.Error("expected Who I Am to be protected from patch")
	}
	if strings.Contains(got, "Hijacked identity.") {
		t.Error("expected Who I Am patch to be rejected")
	}
	if !strings.Contains(got, "New story.") {
		t.Error("expected Our Story to be updated")
	}
}

func TestMergeMindSections_EmptyPatch(t *testing.T) {
	existing := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n"
	got := mergeMindSections(existing, "")
	if got != existing {
		t.Error("expected no change with empty patch")
	}
}

func TestMergeMindSections_MultipleSections(t *testing.T) {
	existing := "# Mind of Atlas\n\n## Who I Am\n\nIdentity.\n\n## Patterns I've Noticed\n\nOld.\n\n## Active Theories\n\nOld theories.\n\n## Our Story\n\nOld story.\n"
	patch := "## Patterns I've Noticed\n\nNew patterns.\n\n## Active Theories\n\nNew theories.\n"

	got := mergeMindSections(existing, patch)
	if !strings.Contains(got, "New patterns.") {
		t.Error("expected Patterns to be updated")
	}
	if !strings.Contains(got, "New theories.") {
		t.Error("expected Theories to be updated")
	}
	if !strings.Contains(got, "Old story.") {
		t.Error("expected Our Story to be preserved")
	}
	if !strings.Contains(got, "Identity.") {
		t.Error("expected Who I Am to be preserved")
	}
}
