// Package mind — dream.go implements the nightly "dream" consolidation cycle.
// Inspired by Anthropic's "auto dream" and Park et al.'s reflection mechanism,
// it periodically prunes stale memories, merges near-duplicates, synthesizes
// diary entries into structured memories, and refreshes MIND.md.
package mind

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/features"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

// dreamHour is the local hour at which the dream cycle fires (3 AM).
const dreamHour = 3

// ProviderResolver returns a fresh ProviderConfig from the current runtime
// config. Used to avoid capturing a stale provider at startup.
type ProviderResolver func() (agent.ProviderConfig, error)

// StartDreamCycle launches a background goroutine that runs the four-phase
// consolidation cycle once per day at dreamHour local time. Returns a stop
// function that cancels the scheduler.
//
// resolveProvider is called fresh on each cycle run so config changes (e.g.
// switching AI providers) are picked up without a restart.
func StartDreamCycle(supportDir string, db *storage.DB, cfgStore *config.Store, resolveProvider ProviderResolver) func() {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		runDreamScheduler(ctx, supportDir, db, cfgStore, resolveProvider)
	}()

	return func() {
		cancel()
		wg.Wait()
	}
}

func runDreamScheduler(ctx context.Context, supportDir string, db *storage.DB, cfgStore *config.Store, resolveProvider ProviderResolver) {
	for {
		// Calculate duration until next dreamHour.
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), dreamHour, 7, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		delay := next.Sub(now)

		logstore.Write("info", fmt.Sprintf("Dream cycle: next run in %s at %s",
			delay.Round(time.Minute), next.Format("15:04")), nil)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			runDreamCycle(ctx, supportDir, db, cfgStore, resolveProvider)
		}
	}
}

func runDreamCycle(ctx context.Context, supportDir string, db *storage.DB, cfgStore *config.Store, resolveProvider ProviderResolver) {
	start := time.Now()
	logstore.Write("info", "Dream cycle: starting", nil)

	cfg := cfgStore.Load()
	if !cfg.MemoryEnabled {
		logstore.Write("info", "Dream cycle: memory disabled, skipping", nil)
		return
	}

	// Resolve a fresh provider for AI calls.
	var provider agent.ProviderConfig
	if resolveProvider != nil {
		var err error
		provider, err = resolveProvider()
		if err != nil {
			logstore.Write("warn", "Dream cycle: no AI provider available: "+err.Error(), nil)
			// Still run non-AI phases (prune, merge).
		}
	}

	// Phase 1: Prune stale memories.
	pruned := phasePrune(db)

	// Phase 2: Merge near-duplicate memories.
	merged := phaseMerge(db)

	// Phase 3: Synthesize diary entries into memories (requires AI).
	synthesized := 0
	if provider.Type != "" {
		synthesized = phaseDiarySynthesis(ctx, provider, supportDir, db)
	}

	// Phase 4: Refresh MIND.md with current memories + diary (requires AI).
	if provider.Type != "" {
		phaseMindRefresh(ctx, provider, supportDir, db)
	}

	elapsed := time.Since(start).Round(time.Second)
	logstore.Write("info", fmt.Sprintf(
		"Dream cycle: complete in %s — pruned %d, merged %d, synthesized %d",
		elapsed, pruned, merged, synthesized),
		map[string]string{
			"pruned":      fmt.Sprintf("%d", pruned),
			"merged":      fmt.Sprintf("%d", merged),
			"synthesized": fmt.Sprintf("%d", synthesized),
		})
}

// ── Phase 1: Prune ──────────────────────────────────────────────────────────

func phasePrune(db *storage.DB) int {
	// Low-confidence memories older than 30 days.
	// Never-retrieved memories older than 60 days with importance < 0.7.
	pruned := db.DeleteStaleMemories(30, 60, 0.5, 0.7)
	if pruned > 0 {
		logstore.Write("info", fmt.Sprintf("Dream: pruned %d stale memories", pruned), nil)
	}
	return pruned
}

// ── Phase 2: Merge near-duplicates ──────────────────────────────────────────

func phaseMerge(db *storage.DB) int {
	all, err := db.ListAllMemories()
	if err != nil || len(all) < 2 {
		return 0
	}

	// Group by category.
	groups := map[string][]storage.MemoryRow{}
	for _, m := range all {
		groups[m.Category] = append(groups[m.Category], m)
	}

	merged := 0
	now := time.Now().UTC().Format(time.RFC3339Nano)

	for _, mems := range groups {
		if len(mems) < 2 {
			continue
		}
		// Find pairs with identical titles (different IDs).
		// The regex extractor already deduplicates on (category, title), but
		// LLM extraction or diary synthesis could create near-duplicates.
		seen := map[string]*storage.MemoryRow{}
		for i := range mems {
			m := &mems[i]
			key := strings.ToLower(strings.TrimSpace(m.Title))
			if existing, ok := seen[key]; ok {
				// Merge: keep the one with higher importance, combine content.
				keeper, discard := existing, m
				if m.Importance > existing.Importance {
					keeper, discard = m, existing
				}
				// Prefer longer content.
				if len(discard.Content) > len(keeper.Content) {
					keeper.Content = discard.Content
				}
				// Take max scores.
				if discard.Importance > keeper.Importance {
					keeper.Importance = discard.Importance
				}
				if discard.Confidence > keeper.Confidence {
					keeper.Confidence = discard.Confidence
				}
				keeper.IsUserConfirmed = keeper.IsUserConfirmed || discard.IsUserConfirmed
				keeper.UpdatedAt = now

				// Merge tags.
				var keeperTags, discardTags []string
				json.Unmarshal([]byte(keeper.TagsJSON), &keeperTags)   //nolint:errcheck
				json.Unmarshal([]byte(discard.TagsJSON), &discardTags) //nolint:errcheck
				tagSet := map[string]bool{}
				for _, t := range keeperTags {
					tagSet[t] = true
				}
				for _, t := range discardTags {
					if !tagSet[t] {
						keeperTags = append(keeperTags, t)
					}
				}
				if b, err := json.Marshal(keeperTags); err == nil {
					keeper.TagsJSON = string(b)
				}

				db.UpdateMemory(*keeper) //nolint:errcheck
				db.DeleteMemory(discard.ID)
				merged++
				seen[key] = keeper
			} else {
				seen[key] = m
			}
		}
	}

	if merged > 0 {
		logstore.Write("info", fmt.Sprintf("Dream: merged %d duplicate memories", merged), nil)
	}
	return merged
}

// ── Phase 3: Diary synthesis ────────────────────────────────────────────────

func phaseDiarySynthesis(ctx context.Context, provider agent.ProviderConfig, supportDir string, db *storage.DB) int {
	diary := features.DiaryContext(supportDir, 14)
	if diary == "" {
		return 0
	}

	system := `You analyze Atlas diary entries and extract recurring patterns, preferences, or behaviors worth remembering long-term.

Return a JSON array of objects. Each object has:
- "category": one of "preference", "workflow", "episodic"
- "title": short descriptive title (max 6 words)
- "content": one sentence describing the pattern
- "importance": 0.5-1.0

Rules:
- Only extract patterns that appear across MULTIPLE diary entries
- Skip one-off events unless they represent a major milestone
- Max 3 items
- Return [] if no clear patterns emerge`

	messages := []agent.OAIMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: "Diary entries from the last 14 days:\n\n" + truncate(diary, 3000)},
	}

	reply, _, _, err := agent.CallAINonStreamingExported(ctx, provider, messages, nil)
	if err != nil {
		logstore.Write("warn", "Dream: diary synthesis AI call failed: "+err.Error(), nil)
		return 0
	}

	replyStr, ok := reply.Content.(string)
	if !ok {
		return 0
	}
	replyStr = strings.TrimSpace(replyStr)

	// Strip markdown code fences.
	if strings.HasPrefix(replyStr, "```") {
		if idx := strings.Index(replyStr, "\n"); idx >= 0 {
			replyStr = replyStr[idx+1:]
		}
		if idx := strings.LastIndex(replyStr, "```"); idx >= 0 {
			replyStr = replyStr[:idx]
		}
		replyStr = strings.TrimSpace(replyStr)
	}

	var candidates []struct {
		Category   string  `json:"category"`
		Title      string  `json:"title"`
		Content    string  `json:"content"`
		Importance float64 `json:"importance"`
	}
	if err := json.Unmarshal([]byte(replyStr), &candidates); err != nil {
		logstore.Write("debug", "Dream: diary synthesis invalid JSON: "+err.Error(), nil)
		return 0
	}

	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	saved := 0

	for _, c := range candidates {
		if c.Title == "" || c.Content == "" {
			continue
		}
		if c.Importance < 0.5 || c.Importance > 1.0 {
			c.Importance = 0.7
		}

		existing, err := db.FindDuplicateMemory(c.Category, c.Title)
		if err != nil {
			continue
		}
		if existing != nil {
			// Update existing with newer content if longer.
			if len(c.Content) > len(existing.Content) {
				upd := *existing
				upd.Content = c.Content
				upd.UpdatedAt = now
				db.UpdateMemory(upd) //nolint:errcheck
			}
			continue
		}

		convID := "dream-cycle"
		row := storage.MemoryRow{
			ID:                    dreamMemoryID(),
			Category:              c.Category,
			Title:                 c.Title,
			Content:               c.Content,
			Source:                "diary_synthesis",
			Confidence:            0.80,
			Importance:            c.Importance,
			CreatedAt:             now,
			UpdatedAt:             now,
			TagsJSON:              `["dream","diary_synthesis"]`,
			RelatedConversationID: &convID,
		}
		db.SaveMemory(row) //nolint:errcheck
		saved++
	}

	if saved > 0 {
		logstore.Write("info", fmt.Sprintf("Dream: synthesized %d memories from diary", saved), nil)
	}
	return saved
}

// dreamMemoryID generates a random 16-byte hex memory ID for dream-created memories.
func dreamMemoryID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// ── Phase 4: MIND refresh ───────────────────────────────────────────────────

func phaseMindRefresh(ctx context.Context, provider agent.ProviderConfig, supportDir string, db *storage.DB) {
	mindPath := filepath.Join(supportDir, "MIND.md")
	data, err := os.ReadFile(mindPath)
	if err != nil {
		return
	}
	current := strings.TrimSpace(string(data))
	if current == "" {
		return
	}

	// Build context: current memories + recent diary.
	mems, _ := db.ListAllMemories()
	var memBlock strings.Builder
	for _, m := range mems {
		memBlock.WriteString(fmt.Sprintf("- [%s] %s: %s\n", m.Category, m.Title, m.Content))
	}

	diary := features.DiaryContext(supportDir, 14)

	system := `You are Atlas reviewing your MIND.md during a nightly consolidation cycle.

Return ONLY the sections you want to update, each with its exact "## " header.
Updatable sections:
- ## My Understanding of You
- ## Patterns I've Noticed
- ## Active Theories
- ## Our Story
- ## What I'm Curious About
- ## What Matters Right Now

Rules:
- Return NOTHING for sections that don't need changes
- Do NOT return ## Who I Am, ## Working Style, or ## Today's Read
- Do NOT include "# Mind of Atlas" or the metadata line
- Use memories and diary entries as evidence for updates
- Remove outdated theories or patterns contradicted by recent evidence
- First person throughout`

	userContent := fmt.Sprintf(`Current MIND.md:
%s

All stored memories:
%s

Recent diary (14 days):
%s

Return only sections that need updating based on the current evidence:`,
		truncateSandwich(current, 6000),
		truncate(memBlock.String(), 2000),
		truncate(diary, 1500),
	)

	reply, err := callFast(ctx, provider, system, userContent)
	if err != nil {
		logstore.Write("warn", "Dream: MIND refresh AI call failed: "+err.Error(), nil)
		return
	}

	patch := strings.TrimSpace(reply)
	if patch == "" {
		return
	}

	merged := mergeMindSections(current, patch)
	merged = updateReflectionDate(merged)

	if err := validateMindContent(merged); err != nil {
		logstore.Write("warn", "Dream: MIND refresh validation failed: "+err.Error(), nil)
		return
	}

	if err := atomicWrite(mindPath, []byte(merged), 0o600); err != nil {
		logstore.Write("warn", "Dream: MIND refresh write failed: "+err.Error(), nil)
		return
	}

	logstore.Write("info", "Dream: MIND.md refreshed from consolidated evidence", nil)
}
