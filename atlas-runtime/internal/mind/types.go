// Package mind implements MIND.md and SKILLS.md lifecycle: automated two-tier
// reflection after every agent turn, DIARY.md integration, SKILLS.md learned
// routine detection and selective injection, and first-run seeding.
package mind

import "time"

// TurnRecord captures one completed agent turn. It is passed to both the MIND
// reflection pipeline and the SKILLS learning pipeline after every turn.
type TurnRecord struct {
	ConversationID      string
	UserMessage         string
	AssistantResponse   string
	ToolCallSummaries   []string // tool names used: ["web.search", "fs.write_file"]
	ToolResultSummaries []string // short result summaries (one per tool call)
	Timestamp           time.Time
}
