// Package memory implements post-turn memory extraction for the Go runtime.
// It is a simplified port of the Swift MemoryExtractionEngine: same categories,
// same patterns, same deduplication logic, no LLM calls.
package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

// candidate is an in-flight memory item before persistence.
type candidate struct {
	category        string
	title           string
	content         string
	source          string
	confidence      float64
	importance      float64
	isUserConfirmed bool
	tags            []string
}

// ExtractAndPersist runs memory extraction on a completed turn and saves
// qualifying candidates to the database. Safe to call in a goroutine.
//
// Two-stage pipeline:
//  1. Regex-based extraction (fast, no API call) for known patterns.
//  2. LLM-based extraction for novel facts from both user and assistant messages.
//     Skipped when regex found an explicit "remember that" command.
func ExtractAndPersist(
	ctx context.Context,
	cfg config.RuntimeConfigSnapshot,
	provider agent.ProviderConfig,
	userMsg, assistantMsg string,
	toolSummaries []string,
	convID string,
	db *storage.DB,
) {
	if !cfg.MemoryEnabled {
		return
	}
	text := strings.TrimSpace(userMsg)
	if text == "" {
		return
	}

	threshold := cfg.MemoryAutoSaveThreshold
	if threshold <= 0 {
		threshold = 0.75
	}

	// Stage 1: regex-based extraction.
	candidates := extractCandidates(text)
	seen := map[string]bool{}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hadExplicit := false

	saved, updated := 0, 0

	for _, c := range candidates {
		if c.isUserConfirmed {
			hadExplicit = true
		}
		if !c.isUserConfirmed && c.confidence < threshold {
			continue
		}
		key := c.category + "|" + normalize(c.title)
		if seen[key] {
			continue
		}
		seen[key] = true

		tagsJSON := "[]"
		if len(c.tags) > 0 {
			if b, err := json.Marshal(c.tags); err == nil {
				tagsJSON = string(b)
			}
		}

		existing, err := db.FindDuplicateMemory(c.category, c.title)
		if err != nil {
			continue
		}

		if existing != nil {
			// Merge: prefer user-confirmed or higher-confidence content; take max scores.
			content := existing.Content
			if c.isUserConfirmed && c.confidence >= existing.Confidence {
				content = c.content
			} else if len(c.content) > len(existing.Content) {
				content = c.content
			}
			confidence := existing.Confidence
			if c.confidence > confidence {
				confidence = c.confidence
			}
			importance := existing.Importance
			if c.importance > importance {
				importance = c.importance
			}
			mergedTags := mergeTags(existing.TagsJSON, c.tags)
			mergedTagsJSON, _ := json.Marshal(mergedTags)

			upd := *existing
			upd.Content = content
			upd.Confidence = confidence
			upd.Importance = importance
			upd.IsUserConfirmed = existing.IsUserConfirmed || c.isUserConfirmed
			upd.TagsJSON = string(mergedTagsJSON)
			upd.UpdatedAt = now
			if db.UpdateMemory(upd) == nil { //nolint:errcheck
				updated++
			}
		} else {
			row := storage.MemoryRow{
				ID:                    newMemoryID(),
				Category:              c.category,
				Title:                 c.title,
				Content:               c.content,
				Source:                c.source,
				Confidence:            c.confidence,
				Importance:            c.importance,
				CreatedAt:             now,
				UpdatedAt:             now,
				IsUserConfirmed:       c.isUserConfirmed,
				IsSensitive:           false,
				TagsJSON:              tagsJSON,
				RelatedConversationID: &convID,
			}
			if db.SaveMemory(row) == nil { //nolint:errcheck
				saved++
			}
		}
	}

	if saved+updated > 0 {
		logstore.Write("debug",
			fmt.Sprintf("Memories extracted: %d new, %d updated", saved, updated),
			map[string]string{"conv": convID[:8]})
	}

	// Stage 2: LLM-based extraction — catches novel facts the regex misses.
	// Skip when the user gave an explicit "remember" command (intent is clear)
	// or when the provider has no API key (LM Studio is key-optional but we
	// still need a provider to make the call).
	if !hadExplicit && provider.Type != "" {
		extractWithLLM(ctx, provider, userMsg, assistantMsg, toolSummaries, convID, db)
	}
}

// extractCandidates runs all extraction rules against the user message.
// Explicit requests short-circuit all other extraction.
func extractCandidates(text string) []candidate {
	if c := explicitCandidate(text); c != nil {
		return []candidate{*c}
	}
	var out []candidate
	out = append(out, profileCandidates(text)...)
	out = append(out, preferenceCandidates(text)...)
	out = append(out, projectCandidates(text)...)
	out = append(out, workflowCandidates(text)...)
	if c := episodicCandidate(text); c != nil {
		out = append(out, *c)
	}
	return out
}

// ── Explicit ──────────────────────────────────────────────────────────────────

var explicitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bremember that\s+(.+)$`),
	regexp.MustCompile(`(?i)\bremember\s*:\s*(.+)$`),
	regexp.MustCompile(`(?i)\bplease remember\s+(.+)$`),
}

func explicitCandidate(text string) *candidate {
	for _, re := range explicitPatterns {
		m := re.FindStringSubmatch(text)
		if len(m) < 2 {
			continue
		}
		captured := strings.TrimSpace(m[1])
		if captured == "" {
			continue
		}
		// Try to recognise a known structured fact first.
		if c := structuredExplicitCandidate(captured); c != nil {
			c.isUserConfirmed = true
			c.confidence = 0.99
			c.importance = 0.95
			return c
		}
		// Generic explicit.
		cat, title, tags := inferExplicitDescriptor(captured)
		return &candidate{
			category:        cat,
			title:           title,
			content:         captured,
			source:          "user_explicit",
			confidence:      0.99,
			importance:      0.95,
			isUserConfirmed: true,
			tags:            tags,
		}
	}
	return nil
}

func structuredExplicitCandidate(captured string) *candidate {
	if name := extractName(captured); name != "" {
		return &candidate{
			category: "profile",
			title:    "Preferred display name",
			content:  "User prefers to be called " + name + ".",
			source:   "user_explicit",
			tags:     []string{"identity", "name"},
		}
	}
	if loc := extractLocation(captured); loc != "" {
		return &candidate{
			category: "profile",
			title:    "Preferred location",
			content:  "User is based in " + loc + ".",
			source:   "user_explicit",
			tags:     []string{"location", "weather"},
		}
	}
	if name := extractAtlasName(captured); name != "" {
		return &candidate{
			category: "preference",
			title:    "Preferred Atlas name",
			content:  "User prefers Atlas to go by " + name + ".",
			source:   "user_explicit",
			tags:     []string{"assistant", "atlas", "name", strings.ToLower(name)},
		}
	}
	if unit := extractTempUnit(strings.ToLower(captured)); unit != "" {
		return &candidate{
			category: "preference",
			title:    "Preferred temperature unit",
			content:  "User prefers " + unit + " for weather-related temperatures.",
			source:   "user_explicit",
			tags:     []string{"weather", "temperature", "unit", unit},
		}
	}
	return nil
}

// ── Profile ───────────────────────────────────────────────────────────────────

var nameRE = regexp.MustCompile(
	`(?i)\b(?:my name is|you can call me|call me|i go by)\s+([A-Za-z][A-Za-z0-9'_-]*(?:\s+[A-Za-z][A-Za-z0-9'_-]*){0,3})`)

var locationRE = regexp.MustCompile(
	`(?i)\b(?:i\s+(?:also\s+)?live in|i(?:'m| am)\s+in|i(?:'m| am)\s+(?:located|based) in|my location is)\s+([A-Za-z0-9 ,._'-]{2,80})`)

func extractName(text string) string {
	m := nameRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return sanitizeName(m[1])
}

func extractLocation(text string) string {
	m := locationRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return sanitizeLocation(m[1])
}

func profileCandidates(text string) []candidate {
	var out []candidate
	lower := strings.ToLower(text)

	if name := extractName(text); name != "" {
		out = append(out, candidate{
			category:        "profile",
			title:           "Preferred display name",
			content:         "User prefers to be called " + name + ".",
			source:          "user_explicit",
			confidence:      0.98,
			importance:      0.90,
			isUserConfirmed: true,
			tags:            []string{"identity", "name"},
		})
	}
	if loc := extractLocation(text); loc != "" {
		out = append(out, candidate{
			category:        "profile",
			title:           "Preferred location",
			content:         "User is based in " + loc + ".",
			source:          "user_explicit",
			confidence:      0.97,
			importance:      0.90,
			isUserConfirmed: true,
			tags:            []string{"location", "weather"},
		})
	}

	envSignals := []string{"working on", "building", "developing", "using xcode", "use xcode", "macos app", "macos project"}
	if (strings.Contains(lower, "macos") || strings.Contains(lower, "xcode")) && anyContains(lower, envSignals) {
		out = append(out, candidate{
			category:   "profile",
			title:      "Primary development environment",
			content:    "User primarily works in a macOS and Xcode development environment.",
			source:     "conversation_inference",
			confidence: 0.84,
			importance: 0.72,
			tags:       []string{"macos", "xcode", "development"},
		})
	}
	return out
}

// ── Preference ────────────────────────────────────────────────────────────────

var atlasNameRE = regexp.MustCompile(
	`(?i)\b(?:i want you to go by|please go by|you should go by|go by)\s+([A-Za-z][A-Za-z0-9'_-]*(?:\s+[A-Za-z][A-Za-z0-9'_-]*){0,3})`)

var tempUnitFRE = regexp.MustCompile(
	`(?i)\b(?:always\s+use|use|show|display|weather\s+in|temperature\s+in|temps?\s+in|degrees?\s+in|only\s+want\s+weather\s+in)\s*f\b`)

var tempUnitCRE = regexp.MustCompile(
	`(?i)\b(?:always\s+use|use|show|display|weather\s+in|temperature\s+in|temps?\s+in|degrees?\s+in|only\s+want\s+weather\s+in)\s*c\b`)

func extractAtlasName(text string) string {
	m := atlasNameRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return sanitizeName(m[1])
}

func extractTempUnit(lower string) string {
	if strings.Contains(lower, "fahrenheit") || tempUnitFRE.MatchString(lower) {
		return "fahrenheit"
	}
	if strings.Contains(lower, "celsius") || tempUnitCRE.MatchString(lower) {
		return "celsius"
	}
	return ""
}

func preferenceCandidates(text string) []candidate {
	var out []candidate
	lower := strings.ToLower(text)

	if strings.Contains(lower, "prefer concise") ||
		(strings.Contains(lower, "concise") && strings.Contains(lower, "answer")) {
		out = append(out, candidate{
			category:   "preference",
			title:      "Response style",
			content:    "User prefers concise responses for straightforward questions.",
			source:     "conversation_inference",
			confidence: 0.90,
			importance: 0.88,
			tags:       []string{"concise", "responses"},
		})
	}

	if strings.Contains(lower, "prefer detailed") ||
		strings.Contains(lower, "more detailed") ||
		strings.Contains(lower, "more detail") {
		out = append(out, candidate{
			category:   "preference",
			title:      "Response style",
			content:    "User prefers more detailed responses when nuance matters.",
			source:     "conversation_inference",
			confidence: 0.90,
			importance: 0.84,
			tags:       []string{"communication", "detailed", "responses"},
		})
	}

	if strings.Contains(lower, "approval") &&
		(strings.Contains(lower, "clear") || strings.Contains(lower, "surface")) {
		out = append(out, candidate{
			category:   "preference",
			title:      "Approval visibility",
			content:    "User wants approval-requiring actions surfaced clearly before Atlas continues.",
			source:     "conversation_inference",
			confidence: 0.88,
			importance: 0.90,
			tags:       []string{"approvals", "safety"},
		})
	}

	if strings.Contains(lower, "telegram") &&
		(strings.Contains(lower, "prefer") || strings.Contains(lower, "remote")) {
		out = append(out, candidate{
			category:   "preference",
			title:      "Preferred remote interface",
			content:    "User prefers Telegram as a remote interface for Atlas when it is available.",
			source:     "conversation_inference",
			confidence: 0.84,
			importance: 0.78,
			tags:       []string{"telegram", "remote"},
		})
	}

	if name := extractAtlasName(text); name != "" {
		out = append(out, candidate{
			category:        "preference",
			title:           "Preferred Atlas name",
			content:         "User prefers Atlas to go by " + name + ".",
			source:          "user_explicit",
			confidence:      0.97,
			importance:      0.84,
			isUserConfirmed: true,
			tags:            []string{"assistant", "atlas", "name", strings.ToLower(name)},
		})
	}

	if unit := extractTempUnit(lower); unit != "" {
		out = append(out, candidate{
			category:        "preference",
			title:           "Preferred temperature unit",
			content:         "User prefers " + unit + " for weather-related temperatures.",
			source:          "user_explicit",
			confidence:      0.96,
			importance:      0.89,
			isUserConfirmed: true,
			tags:            []string{"weather", "temperature", "unit", unit},
		})
	}
	return out
}

// ── Project ───────────────────────────────────────────────────────────────────

var workSignals = []string{
	"working on", "building", "implement", "implementing", "refactor", "fix", "add ",
}

func projectCandidates(text string) []candidate {
	var out []candidate
	lower := strings.ToLower(text)

	if strings.Contains(lower, "atlas") &&
		(strings.Contains(lower, "project") || strings.Contains(lower, "build") ||
			strings.Contains(lower, "working on")) {
		out = append(out, candidate{
			category:   "project",
			title:      "Atlas project context",
			content:    "Atlas is an active macOS-first AI operator project under ongoing development.",
			source:     "conversation_inference",
			confidence: 0.90,
			importance: 0.92,
			tags:       []string{"atlas", "project", "macos"},
		})
	}

	if strings.Contains(lower, "atlas") && anyContains(lower, workSignals) {
		var focusAreas []string
		if strings.Contains(lower, "ui") {
			focusAreas = append(focusAreas, "UI refinement")
		}
		if strings.Contains(lower, "memory") {
			focusAreas = append(focusAreas, "memory features")
		}
		if strings.Contains(lower, "telegram") {
			focusAreas = append(focusAreas, "Telegram operations")
		}
		if len(focusAreas) > 0 {
			out = append(out, candidate{
				category:   "project",
				title:      "Current Atlas focus",
				content:    "Current Atlas work includes " + strings.Join(focusAreas, ", ") + ".",
				source:     "conversation_inference",
				confidence: 0.78,
				importance: 0.76,
				tags:       []string{"atlas", "focus"},
			})
		}
	}
	return out
}

// ── Workflow ──────────────────────────────────────────────────────────────────

func workflowCandidates(text string) []candidate {
	var out []candidate
	lower := strings.ToLower(text)

	if strings.Contains(lower, "codex") && strings.Contains(lower, "xcode") &&
		anyContains(lower, []string{"working on", "build", "develop", "use", "using"}) {
		out = append(out, candidate{
			category:   "workflow",
			title:      "Development workflow",
			content:    "User uses Codex alongside Xcode for Atlas development work.",
			source:     "conversation_inference",
			confidence: 0.92,
			importance: 0.85,
			tags:       []string{"codex", "xcode", "workflow"},
		})
	}

	if strings.Contains(lower, "stabil") && strings.Contains(lower, "expansion") {
		out = append(out, candidate{
			category:   "workflow",
			title:      "Feature sequencing preference",
			content:    "User prefers feature stabilization before expanding scope.",
			source:     "conversation_inference",
			confidence: 0.88,
			importance: 0.86,
			tags:       []string{"workflow", "stability", "scope"},
		})
	}
	return out
}

// ── Episodic ──────────────────────────────────────────────────────────────────

var successSignals = []string{
	"working perfectly", "works perfectly", "validated successfully",
	"operating as expected", "bridge works", "received on my phone",
}

func episodicCandidate(text string) *candidate {
	lower := strings.ToLower(text)
	if !anyContains(lower, successSignals) {
		return nil
	}
	content := "A recent Atlas workflow was validated successfully during live testing."
	tags := []string{"validation", "milestone"}
	if strings.Contains(lower, "telegram") || strings.Contains(lower, "phone") || strings.Contains(lower, "chat") {
		content = "Telegram bridge behavior was validated successfully in a live user test."
		tags = []string{"telegram", "validation", "milestone"}
	}
	return &candidate{
		category:   "episodic",
		title:      "Recent validation milestone",
		content:    content,
		source:     "assistant_observation",
		confidence: 0.82,
		importance: 0.62,
		tags:       tags,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func inferExplicitDescriptor(content string) (category, title string, tags []string) {
	lower := strings.ToLower(content)
	if extractName(content) != "" {
		return "profile", "Preferred display name", []string{"identity", "name"}
	}
	if extractLocation(content) != "" {
		return "profile", "Preferred location", []string{"location", "weather"}
	}
	if extractAtlasName(content) != "" {
		return "preference", "Preferred Atlas name", []string{"assistant", "atlas", "name"}
	}
	if extractTempUnit(lower) != "" {
		return "preference", "Preferred temperature unit", []string{"weather", "temperature"}
	}
	if strings.Contains(lower, "concise") {
		return "preference", "Response style", []string{"communication", "concise"}
	}
	if strings.Contains(lower, "detailed") {
		return "preference", "Response style", []string{"communication", "detailed"}
	}
	cat := inferCategory(lower)
	t := titleFromContent(content)
	ts := extractKeywordTags(lower)
	return cat, t, ts
}

func inferCategory(lower string) string {
	if strings.Contains(lower, "prefer") || strings.Contains(lower, "fahrenheit") ||
		strings.Contains(lower, "celsius") || strings.Contains(lower, "concise") ||
		strings.Contains(lower, "detailed") {
		return "preference"
	}
	if strings.Contains(lower, "workflow") || strings.Contains(lower, "usually") ||
		strings.Contains(lower, "xcode") {
		return "workflow"
	}
	if strings.Contains(lower, "project") || strings.Contains(lower, "atlas") ||
		strings.Contains(lower, "building") {
		return "project"
	}
	if strings.Contains(lower, "name") || strings.Contains(lower, "call me") ||
		strings.Contains(lower, "live in") || strings.Contains(lower, "based in") {
		return "profile"
	}
	return "episodic"
}

func titleFromContent(content string) string {
	words := strings.Fields(strings.TrimSpace(content))
	if len(words) > 6 {
		words = words[:6]
	}
	t := strings.Join(words, " ")
	if len(t) > 48 {
		t = t[:48]
	}
	return t
}

func extractKeywordTags(lower string) []string {
	keywords := []string{
		"atlas", "telegram", "openai", "xcode", "codex", "runtime", "approvals",
		"memory", "ui", "tools", "location", "weather", "temperature",
		"fahrenheit", "celsius", "communication", "concise", "detailed", "name",
	}
	var tags []string
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			tags = append(tags, kw)
		}
	}
	return tags
}

func sanitizeName(raw string) string {
	fillerWords := map[string]bool{
		"btw": true, "please": true, "thanks": true, "thank": true, "lol": true, "haha": true,
	}
	cleaned := strings.Trim(strings.TrimSpace(raw), ".,!?")
	tokens := strings.Fields(cleaned)
	for len(tokens) > 0 {
		if fillerWords[strings.ToLower(tokens[len(tokens)-1])] {
			tokens = tokens[:len(tokens)-1]
		} else {
			break
		}
	}
	return strings.Join(tokens, " ")
}

func sanitizeLocation(raw string) string {
	cleaned := strings.Trim(strings.TrimSpace(raw), ".,!?")
	// Remove leading "also ".
	if after, ok := strings.CutPrefix(strings.ToLower(cleaned), "also "); ok {
		cleaned = cleaned[len(cleaned)-len(after):]
	}
	tokens := strings.Fields(cleaned)
	for i, t := range tokens {
		letters := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) {
				return r
			}
			return -1
		}, t)
		if letters == "" {
			continue
		}
		if len([]rune(letters)) <= 3 && letters == strings.ToLower(letters) {
			tokens[i] = strings.ToUpper(t)
		} else if t == strings.ToLower(t) {
			runes := []rune(t)
			runes[0] = unicode.ToTitle(runes[0])
			tokens[i] = string(runes)
		}
	}
	return strings.Join(tokens, " ")
}

func normalize(text string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func anyContains(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func mergeTags(existingJSON string, newTags []string) []string {
	var existing []string
	json.Unmarshal([]byte(existingJSON), &existing) //nolint:errcheck
	seen := map[string]bool{}
	for _, t := range existing {
		seen[t] = true
	}
	out := append([]string{}, existing...)
	for _, t := range newTags {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func newMemoryID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
