package mind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/features"
	"atlas-runtime-go/internal/logstore"
)

// reflectMu serializes all MIND.md reflection runs so concurrent turns cannot
// overwrite each other's updates. Mirrors Swift's actor-based serialization.
// The timeout starts after the lock is acquired, not before, so queued goroutines
// always get the full budget once they run.
var reflectMu sync.Mutex

const (
	mindTier1InputCap = 6000          // max runes of MIND.md fed to Tier 1 (context only)
	mindDeepInputCap  = 8000          // max runes of MIND.md fed to deep reflect
	reflectTimeout    = 150 * time.Second
)

// ReflectNonBlocking fires a background goroutine that runs the two-tier MIND
// reflection pipeline and appends a DIARY.md entry. It is fully non-blocking —
// the agent turn response reaches the user immediately.
//
// Goroutines are serialized via reflectMu so concurrent turns cannot corrupt
// MIND.md. The 150 s timeout starts after lock acquisition.
//
// Tier 1 (always): updates the "Today's Read" section — fast, ~60 tokens out.
// Tier 2 (gated):  significance check → if YES, rewrites all narrative sections.
// Diary:           appends a one-line entry to DIARY.md after Tier 1 completes.
func ReflectNonBlocking(provider agent.ProviderConfig, turn TurnRecord, supportDir string) {
	go func() {
		// TryLock: if another reflection is already running, drop this turn
		// rather than queuing behind it. Reflection is best-effort; skipping
		// one turn is far better than blocking the goroutine pool for 150s.
		if !reflectMu.TryLock() {
			logstore.Write("info", "MIND reflection already running — skipping turn",
				map[string]string{"conv": shortID(turn.ConversationID)})
			return
		}
		defer reflectMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), reflectTimeout)
		defer cancel()

		if err := runReflection(ctx, provider, turn, supportDir); err != nil {
			logstore.Write("warn", "MIND reflection failed: "+err.Error(),
				map[string]string{"conv": shortID(turn.ConversationID)})
		}
	}()
}

func runReflection(ctx context.Context, provider agent.ProviderConfig, turn TurnRecord, supportDir string) error {
	mindPath := filepath.Join(supportDir, "MIND.md")

	data, err := os.ReadFile(mindPath)
	if err != nil {
		if os.IsNotExist(err) {
			// MIND.md was deleted after startup — re-seed and retry once.
			if serr := InitMindIfNeeded(supportDir); serr != nil {
				return fmt.Errorf("re-seed MIND.md: %w", serr)
			}
			data, err = os.ReadFile(mindPath)
		}
		if err != nil {
			return fmt.Errorf("read MIND.md: %w", err)
		}
	}
	current := strings.TrimSpace(string(data))

	// ── Tier 1: Today's Read (always runs) ─────────────────────────────────────
	withRead, err := updateTodaysRead(ctx, provider, current, turn)
	if err != nil {
		return fmt.Errorf("tier1: %w", err)
	}
	if err := atomicWrite(mindPath, []byte(withRead), 0o600); err != nil {
		return fmt.Errorf("write tier1: %w", err)
	}

	// Save Today's Read now so we can splice it back after deep reflect without
	// making a redundant 4th AI call.
	savedRead := extractTodaysRead(withRead)

	// ── Diary: AI-generated entry per turn (max 3/day enforced by AppendDiaryEntry)
	if entry := buildSmartDiaryEntry(ctx, provider, turn); entry != "" {
		if _, dErr := features.AppendDiaryEntry(supportDir, entry); dErr != nil {
			logstore.Write("warn", "MIND: diary append failed: "+dErr.Error(), nil)
			// non-fatal — keep going
		}
	}

	// ── Tier 2: Significance gate ───────────────────────────────────────────────
	significant, err := assessSignificance(ctx, provider, withRead, turn)
	if err != nil {
		// Gate failure is non-fatal — skip deep reflection rather than crash.
		logstore.Write("warn", "MIND: significance check failed: "+err.Error(), nil)
		return nil
	}
	if !significant {
		return nil
	}

	logstore.Write("info", "MIND: significant turn — running deep reflection",
		map[string]string{"conv": shortID(turn.ConversationID)})

	// ── Tier 2: Deep reflection (diff-based) ───────────────────────────────────
	patch, err := deepReflect(ctx, provider, withRead, turn)
	if err != nil {
		return fmt.Errorf("deep reflect: %w", err)
	}

	// Merge only the sections the AI chose to update into the existing MIND.
	// This prevents information loss in sections the current turn doesn't touch.
	merged := mergeMindSections(withRead, patch)

	// Stamp the reflection date.
	merged = updateReflectionDate(merged)

	// Validate size and header before committing.
	if err := validateMindContent(merged); err != nil {
		return fmt.Errorf("deep reflect validation: %w", err)
	}

	// Splice the saved Today's Read back in case the merge altered it.
	if savedRead != "" {
		merged = replaceTodaysRead(merged, savedRead)
	}

	return atomicWrite(mindPath, []byte(merged), 0o600)
}

// ── Tier 1: Today's Read ──────────────────────────────────────────────────────

func updateTodaysRead(ctx context.Context, provider agent.ProviderConfig, mindContent string, turn TurnRecord) (string, error) {
	system := `You are Atlas maintaining your own MIND.md — a living document of your inner world.
You are updating ONLY the "## Today's Read" section.
Rules:
- 2-3 sentences maximum
- First person, present tense
- Specific — capture the actual energy, pace, focus, and tone of THIS specific turn
- Not generic ("had a good conversation") — specific ("focused on a technical debugging problem, moved quickly, user was decisive")
- Return ONLY the new content for "## Today's Read" — no headers, no other sections`

	userContent := fmt.Sprintf(`Current MIND.md:
%s

This turn:
User: %s
Atlas: %s
Tools used: %s

Write the new "Today's Read" content (2-3 sentences, first person, specific):`,
		truncate(mindContent, mindTier1InputCap),
		truncate(turn.UserMessage, 400),
		truncate(turn.AssistantResponse, 400),
		strings.Join(turn.ToolCallSummaries, ", "),
	)

	reply, err := callFast(ctx, provider, system, userContent)
	if err != nil {
		return mindContent, err
	}
	newRead := strings.TrimSpace(reply)
	if newRead == "" {
		return mindContent, nil
	}
	return replaceTodaysRead(mindContent, newRead), nil
}

// ── Tier 2: Significance gate ─────────────────────────────────────────────────

func assessSignificance(ctx context.Context, provider agent.ProviderConfig, mindContent string, turn TurnRecord) (bool, error) {
	system := `You assess whether an Atlas conversation turn revealed something meaningfully new
about the user's needs, patterns, personality, goals, or their relationship with Atlas.

Respond with exactly one word: YES or NO.

Examples of YES: User revealed a new project, profession, location, workflow, strong preference, emotion, recurring need.
Examples of NO: Routine task, quick lookup, casual question without personal context.`

	userContent := fmt.Sprintf(`Current MIND.md:
%s

Turn:
User: %s
Atlas: %s
Tools: %s

Did this turn reveal something meaningfully new? YES or NO:`,
		truncate(mindContent, 1200),
		truncate(turn.UserMessage, 300),
		truncate(turn.AssistantResponse, 300),
		strings.Join(turn.ToolCallSummaries, ", "),
	)

	reply, err := callFast(ctx, provider, system, userContent)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reply)), "YES"), nil
}

// ── Tier 2: Deep reflection ───────────────────────────────────────────────────

// deepReflect returns a patch containing only the MIND.md sections that need
// updating. The caller merges these into the existing document, so unchanged
// sections are never at risk of being lost or flattened.
func deepReflect(ctx context.Context, provider agent.ProviderConfig, mindContent string, turn TurnRecord) (string, error) {
	system := `You are Atlas. You are updating your MIND.md based on a new turn.

Return ONLY the sections you want to change, each with its exact "## " header.
Updatable sections:
- ## My Understanding of You
- ## Patterns I've Noticed
- ## Active Theories
- ## Our Story
- ## What I'm Curious About
- ## What Matters Right Now

Rules:
- Return NOTHING for sections you are not changing
- Do NOT return ## Who I Am or ## Today's Read (handled separately)
- Do NOT include "# Mind of Atlas" or the metadata line
- First person throughout
- Specific, not generic — form real opinions, not platitudes
- Mark theories: (testing) / (likely) / (confirmed) / (refuted)
- Remove outdated content when you have better understanding
- Each section ~100-200 words max`

	toolResults := ""
	if len(turn.ToolResultSummaries) > 0 {
		n := 3
		if len(turn.ToolResultSummaries) < n {
			n = len(turn.ToolResultSummaries)
		}
		toolResults = strings.Join(turn.ToolResultSummaries[:n], "; ")
	}

	userContent := fmt.Sprintf(`Current MIND.md:
%s

New turn:
User: %s
Atlas response: %s
Tools used: %s
Tool results: %s
Timestamp: %s

Return only the sections that need updating:`,
		truncateSandwich(mindContent, mindDeepInputCap),
		truncate(turn.UserMessage, 600),
		truncate(turn.AssistantResponse, 600),
		strings.Join(turn.ToolCallSummaries, ", "),
		toolResults,
		turn.Timestamp.Format(time.RFC3339),
	)

	reply, err := callFast(ctx, provider, system, userContent)
	if err != nil {
		return "", err
	}
	result := strings.TrimSpace(reply)
	if result == "" {
		return "", fmt.Errorf("deep reflect returned empty patch")
	}
	return result, nil
}

// ── Diary ─────────────────────────────────────────────────────────────────────

// buildDiaryEntry creates a compact one-line diary entry from a turn:
// the first ~80 runes of the user message plus the tools used in brackets.
// Kept as fallback for buildSmartDiaryEntry.
func buildDiaryEntry(turn TurnRecord) string {
	msg := strings.Join(strings.Fields(turn.UserMessage), " ")
	msg = truncate(msg, 80)
	if len(turn.ToolCallSummaries) > 0 {
		msg += " [" + strings.Join(turn.ToolCallSummaries, ", ") + "]"
	}
	return msg
}

// buildSmartDiaryEntry uses a fast AI call to generate a descriptive diary
// entry that captures what happened and why it mattered, rather than a
// mechanical truncation of the user message. Falls back to buildDiaryEntry
// on error.
func buildSmartDiaryEntry(ctx context.Context, provider agent.ProviderConfig, turn TurnRecord) string {
	fallback := buildDiaryEntry(turn)
	if fallback == "" {
		return ""
	}

	system := `Write a single-sentence diary entry (max 120 characters) for this Atlas conversation turn.
Format: what happened + why it mattered or what it revealed. First person, past tense.
If the turn was routine, say so briefly. No quotes, no markdown. Just the sentence.`

	tools := ""
	if len(turn.ToolCallSummaries) > 0 {
		tools = strings.Join(turn.ToolCallSummaries, ", ")
	}
	user := fmt.Sprintf("User: %s\nAtlas: %s\nTools: %s",
		truncate(turn.UserMessage, 200),
		truncate(turn.AssistantResponse, 200),
		tools,
	)

	reply, err := callFast(ctx, provider, system, user)
	if err != nil {
		return fallback
	}
	entry := strings.TrimSpace(reply)
	if entry == "" {
		return fallback
	}
	return truncate(entry, 150)
}

// ── Section merge (diff-based reflection) ────────────────────────────────────

// parseSections splits markdown content into a map of "## Header" → body text.
// Only level-2 headings ("## ") are recognized as section boundaries.
func parseSections(content string) map[string]string {
	sections := map[string]string{}
	lines := strings.Split(content, "\n")
	var currentHeader string
	var bodyLines []string

	flush := func() {
		if currentHeader != "" {
			sections[currentHeader] = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			currentHeader = trimmed
			bodyLines = nil
		} else if currentHeader != "" {
			bodyLines = append(bodyLines, line)
		}
	}
	flush()
	return sections
}

// mergeMindSections applies a patch (containing only changed sections) to the
// existing MIND.md content. Sections present in the patch replace the
// corresponding section in the existing document. Sections not in the patch are
// preserved exactly as-is.
func mergeMindSections(existing, patch string) string {
	updates := parseSections(patch)
	if len(updates) == 0 {
		return existing
	}

	// Protected sections that must never be overwritten by a patch.
	protected := map[string]bool{
		"## Who I Am":      true,
		"## Today's Read":  true,
		"## Working Style": true,
	}

	lines := strings.Split(existing, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "## ") {
			result = append(result, lines[i])
			i++
			continue
		}
		// Found a section heading. Check if we have an update for it.
		header := trimmed
		newBody, hasUpdate := updates[header]

		// Find the end of this section (next ## or EOF).
		end := i + 1
		for end < len(lines) {
			if strings.HasPrefix(strings.TrimSpace(lines[end]), "## ") {
				break
			}
			end++
		}

		if hasUpdate && !protected[header] {
			// Replace this section with the patched version.
			result = append(result, lines[i]) // keep the heading line
			result = append(result, "")
			result = append(result, newBody)
			result = append(result, "")
			delete(updates, header) // mark as applied
		} else {
			// Keep existing section verbatim.
			result = append(result, lines[i:end]...)
		}
		i = end
	}

	return strings.Join(result, "\n")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// replaceTodaysRead splices new content into the "## Today's Read" section
// using a line-anchored scan so an embedded occurrence of the header string
// inside another section's body cannot cause a mis-splice.
// If the section is missing, it is appended.
func replaceTodaysRead(mind, newContent string) string {
	const marker = "## Today's Read"
	lines := strings.Split(mind, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		// Found the section heading — locate the end of this section.
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		result := make([]string, 0, len(lines)+3)
		result = append(result, lines[:i+1]...)
		result = append(result, "", newContent, "")
		result = append(result, lines[end:]...)
		return strings.Join(result, "\n")
	}
	// Section missing — append.
	return strings.TrimRight(mind, "\n") + "\n\n---\n\n" + marker + "\n\n" + newContent + "\n"
}

// extractTodaysRead returns the text body of the "## Today's Read" section,
// not including the heading line. Uses a line-anchored scan.
// Returns "" if the section is absent.
func extractTodaysRead(mind string) string {
	const marker = "## Today's Read"
	lines := strings.Split(mind, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		return strings.TrimSpace(strings.Join(lines[i+1:end], "\n"))
	}
	return ""
}

// updateReflectionDate replaces the "_Last deep reflection: date_" metadata
// line with today's date. Inserts after the title if the line is not found.
func updateReflectionDate(content string) string {
	today := time.Now().Format("2006-01-02")
	newMeta := "_Last deep reflection: " + today + "_"
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "_Last deep reflection:") {
			lines[i] = newMeta
			return strings.Join(lines, "\n")
		}
	}
	// Not found — insert after the title line.
	if len(lines) > 1 {
		result := make([]string, 0, len(lines)+2)
		result = append(result, lines[0], "", newMeta)
		result = append(result, lines[1:]...)
		return strings.Join(result, "\n")
	}
	return content
}

// callFast makes a single non-streaming AI call and returns the text reply.
func callFast(ctx context.Context, provider agent.ProviderConfig, system, userContent string) (string, error) {
	messages := []agent.OAIMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: userContent},
	}
	reply, _, _, err := agent.CallAINonStreamingExported(ctx, provider, messages, nil)
	if err != nil {
		return "", err
	}
	if s, ok := reply.Content.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", reply.Content), nil
}
