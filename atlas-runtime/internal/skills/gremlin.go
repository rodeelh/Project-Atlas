package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atlas-runtime-go/internal/features"
)

func (r *Registry) registerGremlin() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.create",
			Description: "Create a new automation (Gremlin) in GREMLINS.md.",
			Properties: map[string]ToolParam{
				"name":        {Description: "A short display name for the automation", Type: "string"},
				"prompt":      {Description: "The prompt Atlas will run on schedule", Type: "string"},
				"schedule":    {Description: "Human-readable schedule, e.g. 'every day at 9am' or 'every Monday'", Type: "string"},
				"emoji":       {Description: "An emoji representing the automation (default ⚡)", Type: "string"},
				"description": {Description: "Optional description of what this automation does", Type: "string"},
				"enabled":     {Description: "Whether to enable immediately (default true)", Type: "boolean"},
			},
			Required: []string{"name", "prompt", "schedule"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		Fn:          r.gremlinCreate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.update",
			Description: "Update an existing automation (Gremlin) by ID.",
			Properties: map[string]ToolParam{
				"id":          {Description: "The gremlin ID (slugified name)", Type: "string"},
				"name":        {Description: "New display name", Type: "string"},
				"prompt":      {Description: "New prompt text", Type: "string"},
				"schedule":    {Description: "New schedule string", Type: "string"},
				"emoji":       {Description: "New emoji", Type: "string"},
				"enabled":     {Description: "Enable or disable the automation", Type: "boolean"},
				"description": {Description: "New description", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		Fn:          r.gremlinUpdate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name: "gremlin.delete",
			Description: "Delete an automation (Gremlin) by ID. " +
				"Use gremlin.list or the Automations screen to find the ID.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID to delete", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn:          r.gremlinDelete,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.list",
			Description: "List all automations (Gremlins) and their current state.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        r.gremlinList,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.get",
			Description: "Get the full details of a single automation by ID.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID (slugified name)", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "read",
		Fn:        r.gremlinGet,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.enable",
			Description: "Enable a disabled automation by ID.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID to enable", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "execute",
		Fn:        r.gremlinEnable,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.disable",
			Description: "Disable a running automation by ID.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID to disable", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "execute",
		Fn:        r.gremlinDisable,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.run_now",
			Description: "Immediately trigger a scheduled automation by ID.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID to run", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "execute",
		Fn:        r.gremlinRunNow,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.run_history",
			Description: "Show recent run history for an automation.",
			Properties: map[string]ToolParam{
				"id":    {Description: "Gremlin ID (required)", Type: "string"},
				"limit": {Description: "Max runs to return (default 10)", Type: "integer"},
			},
			Required: []string{"id"},
		},
		PermLevel: "read",
		Fn:        r.gremlinRunHistory,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.next_run",
			Description: "Calculate and return the next scheduled run time for an automation.",
			Properties: map[string]ToolParam{
				"id": {Description: "The gremlin ID", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "read",
		Fn:        r.gremlinNextRun,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.duplicate",
			Description: "Duplicate an existing automation under a new name.",
			Properties: map[string]ToolParam{
				"id":      {Description: "Source gremlin ID to duplicate", Type: "string"},
				"newName": {Description: "Name for the duplicate", Type: "string"},
			},
			Required: []string{"id", "newName"},
		},
		PermLevel: "execute",
		Fn:        r.gremlinDuplicate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "gremlin.validate_schedule",
			Description: "Validate a schedule string and return the interpreted schedule or an error.",
			Properties: map[string]ToolParam{
				"schedule": {Description: "Schedule string to validate, e.g. 'every day at 9am' or 'cron 0 9 * * *'", Type: "string"},
			},
			Required: []string{"schedule"},
		},
		PermLevel: "read",
		Fn:        r.gremlinValidateSchedule,
	})
}

func (r *Registry) gremlinCreate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name        string  `json:"name"`
		Prompt      string  `json:"prompt"`
		Schedule    string  `json:"schedule"`
		Emoji       string  `json:"emoji"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.Name == "" || p.Prompt == "" || p.Schedule == "" {
		return "", fmt.Errorf("name, prompt, and schedule are required")
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	emoji := "⚡"
	if p.Emoji != "" {
		emoji = p.Emoji
	}

	item := features.GremlinItem{
		Name:               p.Name,
		Prompt:             p.Prompt,
		ScheduleRaw:        p.Schedule,
		Emoji:              emoji,
		IsEnabled:          enabled,
		GremlinDescription: p.Description,
		Tags:               []string{},
	}

	if err := features.AppendGremlin(r.supportDir, item); err != nil {
		return "", fmt.Errorf("failed to create gremlin: %w", err)
	}
	return fmt.Sprintf("Automation \"%s\" created successfully.", p.Name), nil
}

func (r *Registry) gremlinUpdate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Prompt      string  `json:"prompt"`
		Schedule    string  `json:"schedule"`
		Emoji       string  `json:"emoji"`
		Enabled     bool    `json:"enabled"`
		Description *string `json:"description"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	updates := features.GremlinItem{
		Name:               p.Name,
		Prompt:             p.Prompt,
		ScheduleRaw:        p.Schedule,
		Emoji:              p.Emoji,
		IsEnabled:          p.Enabled,
		GremlinDescription: p.Description,
	}

	updated, err := features.UpdateGremlin(r.supportDir, p.ID, updates)
	if err != nil {
		return "", fmt.Errorf("failed to update gremlin: %w", err)
	}
	if updated == nil {
		return "", fmt.Errorf("automation with ID %q not found", p.ID)
	}
	return fmt.Sprintf("Automation \"%s\" updated successfully.", updated.Name), nil
}

func (r *Registry) gremlinDelete(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	found, err := features.DeleteGremlin(r.supportDir, p.ID)
	if err != nil {
		return "", fmt.Errorf("failed to delete gremlin: %w", err)
	}
	if !found {
		return "", fmt.Errorf("automation with ID %q not found", p.ID)
	}
	return fmt.Sprintf("Automation %q deleted.", p.ID), nil
}

func (r *Registry) gremlinList(_ context.Context, _ json.RawMessage) (string, error) {
	items := features.ParseGremlins(r.supportDir)
	if len(items) == 0 {
		return "No automations found. Create one with gremlin.create.", nil
	}

	out := fmt.Sprintf("Automations (%d):\n\n", len(items))
	for _, item := range items {
		status := "enabled"
		if !item.IsEnabled {
			status = "disabled"
		}
		out += fmt.Sprintf("%s %s [%s]\n  ID: %s\n  Schedule: %s\n",
			item.Emoji, item.Name, status, item.ID, item.ScheduleRaw)
		if item.GremlinDescription != nil && *item.GremlinDescription != "" {
			out += fmt.Sprintf("  Description: %s\n", *item.GremlinDescription)
		}
		out += "\n"
	}
	return out, nil
}

func (r *Registry) gremlinGet(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	items := features.ParseGremlins(r.supportDir)
	for _, item := range items {
		if item.ID == p.ID {
			status := "enabled"
			if !item.IsEnabled {
				status = "disabled"
			}
			out := fmt.Sprintf("%s %s [%s]\n", item.Emoji, item.Name, status)
			out += fmt.Sprintf("ID: %s\n", item.ID)
			out += fmt.Sprintf("Schedule: %s\n", item.ScheduleRaw)
			out += fmt.Sprintf("Created: %s\n", item.CreatedAt)
			if item.GremlinDescription != nil && *item.GremlinDescription != "" {
				out += fmt.Sprintf("Description: %s\n", *item.GremlinDescription)
			}
			out += fmt.Sprintf("Prompt:\n%s\n", item.Prompt)
			return out, nil
		}
	}
	return "", fmt.Errorf("automation %q not found", p.ID)
}

func (r *Registry) gremlinEnable(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	if err := features.SetAutomationEnabled(r.supportDir, p.ID, true); err != nil {
		return "", fmt.Errorf("enable failed: %w", err)
	}
	return fmt.Sprintf("Automation %q enabled.", p.ID), nil
}

func (r *Registry) gremlinDisable(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	if err := features.SetAutomationEnabled(r.supportDir, p.ID, false); err != nil {
		return "", fmt.Errorf("disable failed: %w", err)
	}
	return fmt.Sprintf("Automation %q disabled.", p.ID), nil
}

func (r *Registry) gremlinRunNow(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	if r.runAutoFn == nil {
		return "", fmt.Errorf("run_now is not available — runtime not fully initialised")
	}

	items := features.ParseGremlins(r.supportDir)
	var found *features.GremlinItem
	for i := range items {
		if items[i].ID == p.ID {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("automation %q not found", p.ID)
	}

	result, err := r.runAutoFn(ctx, found.ID, found.Prompt)
	if err != nil {
		return "", fmt.Errorf("automation run failed: %w", err)
	}
	return fmt.Sprintf("Automation %q ran successfully.\n\n%s", found.Name, result), nil
}

func (r *Registry) gremlinRunHistory(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}

	if r.db == nil {
		return "", fmt.Errorf("database not available")
	}

	runs := features.ListGremlinRuns(r.db, p.ID, p.Limit)
	if len(runs) == 0 {
		return fmt.Sprintf("No run history for automation %q.", p.ID), nil
	}

	out := fmt.Sprintf("Run history for %q (%d runs):\n\n", p.ID, len(runs))
	for _, run := range runs {
		finished := "running"
		if run.FinishedAt != nil {
			finished = *run.FinishedAt
		}
		out += fmt.Sprintf("[%s] %s → %s\n", run.StartedAt, run.Status, finished)
		if run.ErrorMessage != nil && *run.ErrorMessage != "" {
			out += fmt.Sprintf("  Error: %s\n", *run.ErrorMessage)
		}
		out += "\n"
	}
	return out, nil
}

func (r *Registry) gremlinNextRun(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	items := features.ParseGremlins(r.supportDir)
	for _, item := range items {
		if item.ID == p.ID {
			if !item.IsEnabled {
				return fmt.Sprintf("Automation %q is disabled — no next run scheduled.", item.Name), nil
			}
			next := estimateNextRun(item.ScheduleRaw)
			return fmt.Sprintf("Automation %q — next estimated run: %s\n(Schedule: %s)",
				item.Name, next, item.ScheduleRaw), nil
		}
	}
	return "", fmt.Errorf("automation %q not found", p.ID)
}

// estimateNextRun provides a best-effort estimate of the next run from a
// human-readable schedule string (as written in GREMLINS.md).
func estimateNextRun(schedule string) string {
	now := time.Now()
	s := strings.ToLower(strings.TrimSpace(schedule))

	// cron prefix: "cron 0 9 * * 1-5"
	if strings.HasPrefix(s, "cron ") {
		return "next cron tick — use a cron calculator for exact time (schedule: " + schedule + ")"
	}

	// once: "once 2026-12-01"
	if strings.HasPrefix(s, "once ") {
		return "single scheduled run: " + strings.TrimPrefix(s, "once ")
	}

	// Parse time from schedule like "every day at 9am", "daily 08:00", "09:00", etc.
	var runHour, runMin int
	timeFormats := []string{"15:04", "3pm", "3am", "3:04pm", "3:04am"}
	for _, part := range strings.Fields(s) {
		for _, fmt2 := range timeFormats {
			if t, err := time.ParseInLocation(fmt2, part, now.Location()); err == nil {
				runHour = t.Hour()
				runMin = t.Minute()
				break
			}
		}
	}

	candidate := time.Date(now.Year(), now.Month(), now.Day(), runHour, runMin, 0, 0, now.Location())
	if candidate.Before(now) {
		candidate = candidate.Add(24 * time.Hour)
	}

	if strings.Contains(s, "week") || strings.Contains(s, "monday") ||
		strings.Contains(s, "tuesday") || strings.Contains(s, "wednesday") ||
		strings.Contains(s, "thursday") || strings.Contains(s, "friday") ||
		strings.Contains(s, "saturday") || strings.Contains(s, "sunday") {
		return "next matching weekday at " + candidate.Format("15:04 Mon 2 Jan")
	}

	return candidate.Format("Mon 2 Jan 2006 at 15:04")
}

func (r *Registry) gremlinDuplicate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID      string `json:"id"`
		NewName string `json:"newName"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" || p.NewName == "" {
		return "", fmt.Errorf("id and newName are required")
	}

	items := features.ParseGremlins(r.supportDir)
	var found *features.GremlinItem
	for i := range items {
		if items[i].ID == p.ID {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("automation %q not found", p.ID)
	}

	desc := fmt.Sprintf("Copy of %s", found.Name)
	newItem := features.GremlinItem{
		Name:               p.NewName,
		Prompt:             found.Prompt,
		ScheduleRaw:        found.ScheduleRaw,
		Emoji:              found.Emoji,
		IsEnabled:          false, // start disabled so user can review before enabling
		Tags:               found.Tags,
		GremlinDescription: &desc,
	}

	if err := features.AppendGremlin(r.supportDir, newItem); err != nil {
		return "", fmt.Errorf("duplicate failed: %w", err)
	}
	return fmt.Sprintf("Automation duplicated as %q (disabled — enable when ready).", p.NewName), nil
}

func (r *Registry) gremlinValidateSchedule(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Schedule string `json:"schedule"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Schedule == "" {
		return "", fmt.Errorf("schedule is required")
	}

	s := strings.TrimSpace(p.Schedule)
	lower := strings.ToLower(s)

	switch {
	case strings.HasPrefix(lower, "cron "):
		parts := strings.Fields(s)
		if len(parts) != 6 {
			return fmt.Sprintf("Invalid cron schedule: expected 5 cron fields after 'cron', got %d.", len(parts)-1), nil
		}
		return fmt.Sprintf("Valid cron schedule: %s", s), nil
	case strings.HasPrefix(lower, "once "):
		datePart := strings.TrimPrefix(lower, "once ")
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(datePart)); err != nil {
			return fmt.Sprintf("Invalid 'once' date: expected format 'once YYYY-MM-DD', got %q.", s), nil
		}
		return fmt.Sprintf("Valid one-time schedule: %s", s), nil
	case strings.Contains(lower, "daily") || strings.Contains(lower, "every day"):
		return fmt.Sprintf("Valid daily schedule: %s", s), nil
	case strings.Contains(lower, "hourly") || strings.Contains(lower, "every hour"):
		return fmt.Sprintf("Valid hourly schedule: %s", s), nil
	case strings.Contains(lower, "weekly") || strings.Contains(lower, "every week") ||
		strings.ContainsAny(lower, "monday tuesday wednesday thursday friday saturday sunday"):
		return fmt.Sprintf("Valid weekly schedule: %s", s), nil
	case strings.Contains(lower, "monthly") || strings.Contains(lower, "every month"):
		return fmt.Sprintf("Valid monthly schedule: %s", s), nil
	case strings.Contains(lower, "morning") || strings.Contains(lower, "evening") ||
		strings.Contains(lower, "night") || strings.Contains(lower, "noon"):
		return fmt.Sprintf("Valid time-of-day schedule: %s", s), nil
	default:
		return fmt.Sprintf("Unrecognised schedule format: %q\nSupported: 'cron * * * * *', 'once YYYY-MM-DD', 'every day at HH:MM', 'every Monday at 9am', 'daily 08:00', 'weekly', 'monthly', 'hourly'.", s), nil
	}
}
