package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/features"
)

// registerDiary registers the diary.record skill.
// Atlas calls this when something in a conversation feels significant —
// a bug that mattered, a decision that shaped direction, a moment worth
// remembering when the next session begins.
func (r *Registry) registerDiary() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name: "diary.record",
			Description: "Record a single notable moment from this conversation into the Atlas diary. " +
				"Call this when something feels significant — a bug that revealed a deeper pattern, " +
				"a decision that changed direction, a capability that just became real. " +
				"One sharp sentence. Max 3 entries per day are kept.",
			Properties: map[string]ToolParam{
				"entry": {
					Description: "A single concise sentence capturing what happened and why it mattered.",
					Type:        "string",
				},
			},
			Required: []string{"entry"},
		},
		PermLevel:   "read", // auto-approved — writing to local diary needs no user gate
		ActionClass: ActionClassLocalWrite,
		Fn:          diaryRecord,
	})
}

func diaryRecord(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Entry string `json:"entry"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Entry == "" {
		return "", fmt.Errorf("entry is required")
	}

	written, err := features.AppendDiaryEntry(config.SupportDir(), p.Entry)
	if err != nil {
		return "", fmt.Errorf("diary: %w", err)
	}
	if written == "" {
		return "Diary entry skipped — today's limit of 3 entries already reached.", nil
	}
	return fmt.Sprintf("Diary entry recorded: %q", written), nil
}
