package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

// llmCandidate is the JSON shape returned by the LLM extraction prompt.
type llmCandidate struct {
	Category   string  `json:"category"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
}

// extractWithLLM sends both user and assistant messages to the fast model to
// extract memories that the regex pipeline cannot catch: novel preferences,
// implicit signals, facts discovered through tool results, and relationship
// changes. Results are deduplicated against existing memories before insert.
func extractWithLLM(
	ctx context.Context,
	provider agent.ProviderConfig,
	userMsg, assistantMsg string,
	toolSummaries []string,
	convID string,
	db *storage.DB,
) {
	system := `You extract factual memories from an Atlas conversation turn.

Return a JSON array of objects. Each object has:
- "category": one of "profile", "preference", "project", "workflow", "episodic"
- "title": short descriptive title (max 6 words)
- "content": one sentence describing the fact
- "importance": 0.0-1.0 (how important is this to remember long-term?)

Categories:
- profile: name, location, role, expertise, tools they use
- preference: communication style, response format, approval preferences
- project: active projects, goals, deadlines, tech stack
- workflow: how they work, recurring patterns, habits, schedules
- episodic: significant events, milestones, breakthroughs, frustrations

Rules:
- Only extract NEW facts not already obvious from the conversation
- Skip greetings, routine questions, and small talk
- Skip facts that are only relevant to the current turn (ephemeral)
- Return [] if nothing worth remembering
- Max 3 items per turn
- Be conservative — false positives are worse than missed extractions`

	tools := ""
	if len(toolSummaries) > 0 {
		tools = strings.Join(toolSummaries, ", ")
	}
	userContent := fmt.Sprintf("User: %s\nAtlas: %s\nTools used: %s",
		truncateRunes(userMsg, 400),
		truncateRunes(assistantMsg, 400),
		tools,
	)

	messages := []agent.OAIMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: userContent},
	}

	reply, _, _, err := agent.CallAINonStreamingExported(ctx, provider, messages, nil)
	if err != nil {
		logstore.Write("debug", "LLM memory extraction failed: "+err.Error(),
			map[string]string{"conv": convID[:min(8, len(convID))]})
		return
	}

	replyStr, ok := reply.Content.(string)
	if !ok {
		return
	}
	replyStr = strings.TrimSpace(replyStr)

	// Strip markdown code fences if the model wrapped its response.
	replyStr = stripCodeFence(replyStr)

	var candidates []llmCandidate
	if err := json.Unmarshal([]byte(replyStr), &candidates); err != nil {
		logstore.Write("debug", "LLM memory extraction: invalid JSON: "+err.Error(),
			map[string]string{"conv": convID[:min(8, len(convID))]})
		return
	}

	if len(candidates) == 0 {
		return
	}
	// Cap at 3 to prevent runaway extraction.
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	validCategories := map[string]bool{
		"profile": true, "preference": true, "project": true,
		"workflow": true, "episodic": true,
	}

	saved := 0
	for _, c := range candidates {
		if !validCategories[c.Category] {
			continue
		}
		if c.Title == "" || c.Content == "" {
			continue
		}
		if c.Importance < 0.3 || c.Importance > 1.0 {
			c.Importance = 0.7 // normalize out-of-range values
		}

		// Deduplicate against existing memories.
		existing, err := db.FindDuplicateMemory(c.Category, c.Title)
		if err != nil {
			continue
		}

		if existing != nil {
			// Merge: prefer longer/newer content, take max scores.
			content := existing.Content
			if len(c.Content) > len(existing.Content) {
				content = c.Content
			}
			importance := existing.Importance
			if c.Importance > importance {
				importance = c.Importance
			}
			updated := *existing
			updated.Content = content
			updated.Importance = importance
			updated.UpdatedAt = now
			db.UpdateMemory(updated) //nolint:errcheck
		} else {
			row := storage.MemoryRow{
				ID:                    newMemoryID(),
				Category:              c.Category,
				Title:                 c.Title,
				Content:               c.Content,
				Source:                "llm_extraction",
				Confidence:            0.85, // LLM extraction is reasonably confident
				Importance:            c.Importance,
				CreatedAt:             now,
				UpdatedAt:             now,
				TagsJSON:              "[]",
				RelatedConversationID: &convID,
			}
			db.SaveMemory(row) //nolint:errcheck
			saved++
		}
	}

	if saved > 0 {
		logstore.Write("debug", fmt.Sprintf("LLM extraction: %d new memories saved", saved),
			map[string]string{"conv": convID[:min(8, len(convID))]})
	}
}

// truncateRunes returns the first n runes of s.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// stripCodeFence removes ```json ... ``` wrapping if present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove opening fence line.
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		// Remove closing fence.
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
