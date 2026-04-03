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
	"atlas-runtime-go/internal/logstore"
)

// sequenceTracker counts in-memory occurrences of skill sequences.
// Resets on restart — conservative by design, mirrors Swift SkillsEngine.
//
// skillsMu serializes SKILLS.md reads and writes so concurrent turns cannot
// produce a lost-update (both read the same file, both write, second wins).
var (
	seqMu     sync.Mutex
	seqCounts = map[string]int{}

	skillsMu sync.Mutex
)

// LearnFromTurnNonBlocking fires a background goroutine that detects learnable
// patterns in this turn and updates SKILLS.md when thresholds are met.
// Requires at least 2 tool calls to be worth checking.
func LearnFromTurnNonBlocking(provider agent.ProviderConfig, turn TurnRecord, supportDir string) {
	if len(turn.ToolCallSummaries) < 2 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := learnFromTurn(ctx, provider, turn, supportDir); err != nil {
			logstore.Write("warn", "SKILLS learning failed: "+err.Error(),
				map[string]string{"conv": shortID(turn.ConversationID)})
		}
	}()
}

func learnFromTurn(ctx context.Context, provider agent.ProviderConfig, turn TurnRecord, supportDir string) error {
	skillIDs := dedupeSkills(turn.ToolCallSummaries)

	// Check for explicit "teach me" instruction in the user message.
	lower := strings.ToLower(turn.UserMessage)
	explicitPhrases := []string{"next time i ask", "whenever i say", "always do", "when i ask"}
	for _, phrase := range explicitPhrases {
		if strings.Contains(lower, phrase) {
			return writeRoutine(ctx, provider, turn, skillIDs, "explicit user instruction", supportDir)
		}
	}

	// Track sequence occurrences — learn after 3 identical sequences.
	// Use ASCII separator "|" to avoid UTF-8 multi-byte overhead in map keys.
	key := strings.Join(skillIDs, "|")
	seqMu.Lock()
	seqCounts[key]++
	count := seqCounts[key]
	if count >= 3 {
		seqCounts[key] = 0 // reset so it doesn't fire again immediately
	}
	seqMu.Unlock()

	if count >= 3 {
		return writeRoutine(ctx, provider, turn, skillIDs, "repeated sequence (3+ times)", supportDir)
	}
	return nil
}

func writeRoutine(ctx context.Context, provider agent.ProviderConfig, turn TurnRecord, skillIDs []string, reason, supportDir string) error {
	skillsPath := filepath.Join(supportDir, "SKILLS.md")

	// Read current content BEFORE the AI call — no lock needed at this stage.
	data, err := os.ReadFile(skillsPath)
	if err != nil {
		return fmt.Errorf("read SKILLS.md: %w", err)
	}
	current := strings.TrimSpace(string(data))

	system := `You are Atlas updating your SKILLS.md — a living document of learned skill orchestration patterns.

You are adding or updating a "Learned Routine" entry based on a pattern you've observed.

Rules:
- Add the new routine to the "## Learned Routines" section
- Format exactly:
  ### [Routine Name]
  **Triggers:** [comma-separated trigger phrases]
  **Steps:**
  1. [skill.name] → [action] ([parameter])
  2. ...
  **Learned:** [today's date] — [brief note]
- Do not duplicate an existing routine
- Keep routines concise (max 5 steps)
- Return the COMPLETE updated SKILLS.md`

	userContent := fmt.Sprintf(`Current SKILLS.md:
%s

Reason to add routine: %s
User message that triggered this: %s
Skill sequence used: %s
Today's date: %s

Return the updated SKILLS.md with the new routine added:`,
		current,
		reason,
		truncate(turn.UserMessage, 300),
		strings.Join(skillIDs, " → "),
		time.Now().Format("2006-01-02"),
	)

	// AI call is OUTSIDE the lock — it can take up to 60s and must not block
	// concurrent file reads.
	reply, err := callFast(ctx, provider, system, userContent)
	if err != nil {
		return err
	}
	result := strings.TrimSpace(reply)
	if result == "" {
		return fmt.Errorf("SKILLS AI returned empty content")
	}

	// Validate size and header before committing.
	if err := validateSkillsContent(result); err != nil {
		return fmt.Errorf("SKILLS routine validation: %w", err)
	}

	// Stamp today's date before writing.
	result = updateSkillsDate(result)

	// Acquire lock only for the write. Re-read inside the lock to detect a
	// concurrent update; abort to avoid overwriting a newer version.
	skillsMu.Lock()
	defer skillsMu.Unlock()

	if latestData, rerr := os.ReadFile(skillsPath); rerr == nil {
		if latest := strings.TrimSpace(string(latestData)); latest != current {
			logstore.Write("warn", "SKILLS.md changed during AI call — aborting write to avoid data loss",
				map[string]string{"sequence": strings.Join(skillIDs, "|")})
			return nil
		}
	}

	logstore.Write("info", "SKILLS.md updated: "+reason,
		map[string]string{"sequence": strings.Join(skillIDs, "|")})
	return atomicWrite(skillsPath, []byte(result+"\n"), 0o600)
}

// ── Selective injection ───────────────────────────────────────────────────────

// SkillsContext returns the SKILLS.md content for system prompt injection.
//
//   - Always injects the non-routine sections (Orchestration Principles, Things
//     That Don't Work, etc.) so general guidance is always present.
//   - If a learned routine matches the user message, appends a focused ~150-token
//     routine block so Atlas knows exactly what steps to follow.
func SkillsContext(userMessage, supportDir string) string {
	data, err := os.ReadFile(filepath.Join(supportDir, "SKILLS.md"))
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}

	base := baseSkillsContent(content)
	routineBlock := selectiveBlock(userMessage, content)

	switch {
	case base == "" && routineBlock == "":
		return ""
	case routineBlock == "":
		return base
	case base == "":
		return routineBlock
	default:
		return base + "\n\n" + routineBlock
	}
}

// baseSkillsContent returns SKILLS.md with the "## Learned Routines" section
// stripped out, so only the general principles and other non-routine sections
// are included in the base injection.
func baseSkillsContent(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inRoutines := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## Learned Routines" {
			inRoutines = true
			continue
		}
		if inRoutines && strings.HasPrefix(trimmed, "## ") {
			inRoutines = false
		}
		if !inRoutines {
			result = append(result, line)
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// selectiveBlock returns a ~150-token routine block if the user message matches
// a trigger phrase in any learned routine. Returns "" if no match.
func selectiveBlock(userMessage, skillsContent string) string {
	routines := parseRoutines(skillsContent)
	if len(routines) == 0 {
		return ""
	}
	lower := strings.ToLower(userMessage)
	for _, r := range routines {
		for _, trigger := range r.triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				return formatRoutineBlock(r)
			}
		}
	}
	return ""
}

// ── Routine parsing ───────────────────────────────────────────────────────────

type learnedRoutine struct {
	name     string
	triggers []string
	steps    []string
}

func parseRoutines(content string) []learnedRoutine {
	var routines []learnedRoutine
	sections := strings.Split(content, "\n### ")
	for _, section := range sections[1:] {
		lines := strings.Split(section, "\n")
		if len(lines) == 0 {
			continue
		}
		name := strings.TrimSpace(lines[0])
		var triggers, steps []string
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "**Triggers:**") {
				raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "**Triggers:**"))
				for _, t := range strings.Split(raw, ",") {
					if t := strings.TrimSpace(t); t != "" {
						triggers = append(triggers, t)
					}
				}
			}
			// Match numbered steps: "1. ...", "10. ..." etc. (1–2 digit prefix only).
			// Capping at idx <= 2 prevents false positives like
			// "e.g. do thing" or "web.search → result" from matching.
			if idx := strings.Index(trimmed, ". "); idx > 0 && idx <= 2 {
				prefix := trimmed[:idx]
				allDigits := len(prefix) > 0
				for _, c := range prefix {
					if c < '0' || c > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					if step := strings.TrimSpace(trimmed[idx+2:]); step != "" {
						steps = append(steps, step)
					}
				}
			}
		}
		if len(triggers) > 0 || len(steps) > 0 {
			routines = append(routines, learnedRoutine{name: name, triggers: triggers, steps: steps})
		}
	}
	return routines
}

func formatRoutineBlock(r learnedRoutine) string {
	steps := r.steps
	if len(steps) > 4 {
		logstore.Write("info", "routine truncated to 4 steps for injection",
			map[string]string{"routine": r.name, "total": fmt.Sprintf("%d", len(steps))})
		steps = steps[:4]
	}
	var sb strings.Builder
	sb.WriteString("Learned routine for this request:\n")
	sb.WriteString("### " + r.name + "\n")
	if len(r.triggers) > 0 {
		sb.WriteString("**Triggers:** " + strings.Join(r.triggers, ", ") + "\n")
	}
	sb.WriteString("**Steps:**\n")
	for i, step := range steps {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
	}
	sb.WriteString("Follow these steps in order.")
	// Cap at ~150 tokens (~600 runes). Use rune-based truncation to avoid
	// splitting multi-byte characters.
	return truncate(sb.String(), 600)
}

// updateSkillsDate replaces the "_Last updated: date_" metadata line with
// today's date. No-ops if the line is not present.
func updateSkillsDate(content string) string {
	today := time.Now().Format("2006-01-02")
	newMeta := "_Last updated: " + today + "_"
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "_Last updated:") {
			lines[i] = newMeta
			return strings.Join(lines, "\n")
		}
	}
	return content
}

// dedupeSkills returns the skill names in order of first appearance,
// deduplicated. Uses the full tool name (e.g. "web.search") as the identifier.
func dedupeSkills(toolNames []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, name := range toolNames {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}
