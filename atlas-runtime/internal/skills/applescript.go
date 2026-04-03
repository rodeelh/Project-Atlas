package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (r *Registry) registerAppleScript() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.calendar_read",
			Description: "Returns Calendar events in an optional date range.",
			Properties: map[string]ToolParam{
				"startDate": {Description: "Start date (e.g. '2024-01-01'), optional", Type: "string"},
				"endDate":   {Description: "End date (e.g. '2024-01-31'), optional", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asCalendarRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.calendar_write",
			Description: "Creates a Calendar event.",
			Properties: map[string]ToolParam{
				"title":        {Description: "Event title", Type: "string"},
				"startDate":    {Description: "Start date/time (e.g. '2024-01-15 10:00:00')", Type: "string"},
				"endDate":      {Description: "End date/time (e.g. '2024-01-15 11:00:00')", Type: "string"},
				"calendarName": {Description: "Name of the calendar to add to (optional, uses default)", Type: "string"},
			},
			Required: []string{"title", "startDate", "endDate"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		FnResult:    asCalendarWrite,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.reminders_read",
			Description: "Returns reminders, optionally filtered by list name.",
			Properties: map[string]ToolParam{
				"listName": {Description: "Name of the reminders list (optional, returns all if omitted)", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asRemindersRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.reminders_write",
			Description: "Creates a reminder.",
			Properties: map[string]ToolParam{
				"name":     {Description: "Reminder name", Type: "string"},
				"listName": {Description: "Name of the list to add to (optional)", Type: "string"},
				"dueDate":  {Description: "Due date (e.g. '2024-01-15 09:00:00'), optional", Type: "string"},
			},
			Required: []string{"name"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		FnResult:    asRemindersWrite,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.contacts_read",
			Description: "Returns contacts matching an optional search term.",
			Properties: map[string]ToolParam{
				"searchTerm": {Description: "Name or email to search for (optional, returns all if omitted)", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asContactsRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.notes_read",
			Description: "Returns notes matching an optional search term.",
			Properties: map[string]ToolParam{
				"searchTerm": {Description: "Text to search for in notes (optional, returns recent if omitted)", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asNotesRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.notes_write",
			Description: "Creates a new note in the Notes app.",
			Properties: map[string]ToolParam{
				"title": {Description: "Note title", Type: "string"},
				"body":  {Description: "Note body text", Type: "string"},
			},
			Required: []string{"title", "body"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		FnResult:    asNotesWrite,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.mail_read",
			Description: "Returns recent emails from Mail.app.",
			Properties: map[string]ToolParam{
				"count": {Description: "Number of recent emails to return (default 10)", Type: "integer"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asMailRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.mail_wait_for_message",
			Description: "Wait for a new email to arrive in Mail.app inbox matching an optional sender or subject filter. Polls every few seconds until a match arrives or the timeout elapses. Returns the subject, sender, and first 500 characters of the body.",
			Properties: map[string]ToolParam{
				"from_filter":    {Description: "Partial sender address to match (case-insensitive) — optional", Type: "string"},
				"subject_filter": {Description: "Partial subject to match (case-insensitive) — optional", Type: "string"},
				"timeout_s":      {Description: "Maximum seconds to wait (default 120)", Type: "integer"},
				"poll_s":         {Description: "Seconds between polls (default 5)", Type: "integer"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        asMailWaitForMessage,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.safari_read",
			Description: "Returns the current Safari tab URL and title.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        asSafariRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.safari_navigate",
			Description: "Navigates Safari to a URL.",
			Properties: map[string]ToolParam{
				"url": {Description: "URL to navigate to", Type: "string"},
			},
			Required: []string{"url"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          asSafariNavigate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.music_read",
			Description: "Returns the current Music app track and playback state.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        asMusicRead,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.music_control",
			Description: "Controls Music app playback.",
			Properties: map[string]ToolParam{
				"command": {
					Description: "Playback command",
					Type:        "string",
					Enum:        []string{"play", "pause", "next", "previous"},
				},
			},
			Required: []string{"command"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          asMusicControl,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.system_info",
			Description: "Returns macOS version, hostname, and uptime.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        asSystemInfo,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.run_custom",
			Description: "Runs a custom AppleScript and returns the output.",
			Properties: map[string]ToolParam{
				"script": {Description: "The AppleScript code to execute", Type: "string"},
			},
			Required: []string{"script"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal, // arbitrary AppleScript — highest local risk
		Fn:          asRunCustom,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.mail_write",
			Description: "Composes and optionally sends an email via Mail.app.",
			Properties: map[string]ToolParam{
				"to":      {Description: "Recipient email address", Type: "string"},
				"subject": {Description: "Email subject", Type: "string"},
				"body":    {Description: "Email body text", Type: "string"},
				"send":    {Description: "If true, send immediately; otherwise save as draft (default false)", Type: "boolean"},
			},
			Required: []string{"to", "subject", "body"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassSendPublishDelete, // may send email — always confirm
		FnResult:    asMailWrite,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.calendar_list_calendars",
			Description: "Returns a list of all available calendars in Calendar.app.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        asCalendarListCalendars,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "applescript.reminders_list_lists",
			Description: "Returns all reminder list names in Reminders.app.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        asRemindersListLists,
	})
}

// ── handlers ──────────────────────────────────────────────────────────────────

func runAS(ctx context.Context, script string) (string, error) {
	return runCmd(ctx, "osascript", "-e", script)
}

func asCalendarRead(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	json.Unmarshal(args, &p)

	var script string
	if p.StartDate != "" && p.EndDate != "" {
		script = fmt.Sprintf(`
tell application "Calendar"
	set startD to date %q
	set endD to date %q
	set evList to ""
	repeat with c in calendars
		repeat with e in (every event of c whose start date >= startD and start date <= endD)
			set evList to evList & summary of e & " | " & (start date of e as string) & "\n"
		end repeat
	end repeat
	evList
end tell`, p.StartDate, p.EndDate)
	} else {
		script = `
tell application "Calendar"
	set evList to ""
	set today to current date
	repeat with c in calendars
		repeat with e in (every event of c whose start date >= today)
			set evList to evList & summary of e & " | " & (start date of e as string) & "\n"
		end repeat
	end repeat
	evList
end tell`
	}

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("calendar read failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No calendar events found.", nil
	}
	return out, nil
}

func asCalendarWrite(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var p struct {
		Title        string `json:"title"`
		StartDate    string `json:"startDate"`
		EndDate      string `json:"endDate"`
		CalendarName string `json:"calendarName"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Title == "" || p.StartDate == "" || p.EndDate == "" {
		return ErrResult("create calendar event", "arg validation", false,
			fmt.Errorf("title, startDate, and endDate are required")), nil
	}

	// Dry-run: describe what would happen without executing.
	if IsDryRun(ctx) {
		target := p.Title
		if p.CalendarName != "" {
			target = fmt.Sprintf("%s (in %s)", p.Title, p.CalendarName)
		}
		return DryRunResult(
			fmt.Sprintf("would create calendar event %q from %s to %s", p.Title, p.StartDate, p.EndDate),
			fmt.Sprintf("create event %q start=%s end=%s calendar=%q", p.Title, p.StartDate, p.EndDate, p.CalendarName),
			target,
		), nil
	}

	// Idempotency check: look for an event with the same title on the same start date.
	dupCheck, _ := asCheckCalendarEventExists(ctx, p.Title, p.StartDate)
	var warnings []string
	if dupCheck.IsDuplicate {
		warnings = append(warnings, DuplicateWarning("calendar event", dupCheck))
	}

	var script string
	if p.CalendarName != "" {
		script = fmt.Sprintf(`
tell application "Calendar"
	tell calendar %q
		make new event with properties {summary:%q, start date:date %q, end date:date %q}
	end tell
end tell`, p.CalendarName, p.Title, p.StartDate, p.EndDate)
	} else {
		script = fmt.Sprintf(`
tell application "Calendar"
	tell calendar 1
		make new event with properties {summary:%q, start date:date %q, end date:date %q}
	end tell
end tell`, p.Title, p.StartDate, p.EndDate)
	}

	if _, err := runAS(ctx, script); err != nil {
		return ErrResult(
			fmt.Sprintf("create calendar event %q", p.Title),
			"AppleScript execution",
			false, err,
		), nil
	}

	mut := NewMutation("created", fmt.Sprintf("calendar event %q", p.Title), "", fmt.Sprintf("%s — %s to %s", p.Title, p.StartDate, p.EndDate))
	return ToolResult{
		Success:  true,
		Summary:  fmt.Sprintf("Created calendar event: %s (%s to %s)", p.Title, p.StartDate, p.EndDate),
		Warnings: warnings,
		Artifacts: map[string]any{
			"title":      p.Title,
			"start_date": p.StartDate,
			"end_date":   p.EndDate,
			"calendar":   p.CalendarName,
			"mutation":   mut.ToArtifact(),
		},
	}, nil
}

// asCheckCalendarEventExists checks for an existing event with the same title near startDate.
func asCheckCalendarEventExists(ctx context.Context, title, startDate string) (CheckDuplicateResult, error) {
	script := fmt.Sprintf(`
tell application "Calendar"
	repeat with c in calendars
		repeat with e in events of c
			if summary of e = %q then return "true"
		end repeat
	end repeat
	return "false"
end tell`, title)
	out, err := runAS(ctx, script)
	if err != nil {
		return NoDuplicate, err
	}
	if strings.TrimSpace(out) == "true" {
		return NewDuplicate(
			fmt.Sprintf("Calendar event with title %q already exists", title),
			"exact title match",
			"high",
		), nil
	}
	return NoDuplicate, nil
}

func asRemindersRead(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ListName string `json:"listName"`
	}
	json.Unmarshal(args, &p)

	var script string
	if p.ListName != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	set remList to ""
	repeat with r in reminders of list %q
		set remList to remList & name of r & " | completed: " & (completed of r as string) & "\n"
	end repeat
	remList
end tell`, p.ListName)
	} else {
		script = `
tell application "Reminders"
	set remList to ""
	repeat with l in lists
		repeat with r in reminders of l
			set remList to remList & name of r & " [" & name of l & "] | completed: " & (completed of r as string) & "\n"
		end repeat
	end repeat
	remList
end tell`
	}

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("reminders read failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No reminders found.", nil
	}
	return out, nil
}

func asRemindersWrite(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var p struct {
		Name     string `json:"name"`
		ListName string `json:"listName"`
		DueDate  string `json:"dueDate"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Name == "" {
		return ErrResult("create reminder", "arg validation", false,
			fmt.Errorf("name is required")), nil
	}

	// Dry-run.
	if IsDryRun(ctx) {
		target := p.Name
		if p.ListName != "" {
			target = fmt.Sprintf("%s (in list %q)", p.Name, p.ListName)
		}
		wouldHappen := fmt.Sprintf("create reminder %q", p.Name)
		if p.DueDate != "" {
			wouldHappen += fmt.Sprintf(" due %s", p.DueDate)
		}
		return DryRunResult(fmt.Sprintf("would create reminder %q", p.Name), wouldHappen, target), nil
	}

	// Idempotency check.
	dupCheck, _ := asCheckReminderExists(ctx, p.Name, p.ListName)
	var warnings []string
	if dupCheck.IsDuplicate {
		warnings = append(warnings, DuplicateWarning("reminder", dupCheck))
	}

	var script string
	if p.ListName != "" && p.DueDate != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	tell list %q
		make new reminder with properties {name:%q, due date:date %q}
	end tell
end tell`, p.ListName, p.Name, p.DueDate)
	} else if p.ListName != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	tell list %q
		make new reminder with properties {name:%q}
	end tell
end tell`, p.ListName, p.Name)
	} else {
		script = fmt.Sprintf(`
tell application "Reminders"
	make new reminder with properties {name:%q}
end tell`, p.Name)
	}

	if _, err := runAS(ctx, script); err != nil {
		return ErrResult(
			fmt.Sprintf("create reminder %q", p.Name),
			"AppleScript execution",
			false, err,
		), nil
	}

	mut := NewMutation("created", fmt.Sprintf("reminder %q", p.Name), "", p.Name)
	artifacts := map[string]any{
		"name":     p.Name,
		"mutation": mut.ToArtifact(),
	}
	if p.ListName != "" {
		artifacts["list"] = p.ListName
	}
	if p.DueDate != "" {
		artifacts["due_date"] = p.DueDate
	}

	summary := fmt.Sprintf("Created reminder: %s", p.Name)
	if p.ListName != "" {
		summary += fmt.Sprintf(" (in list %q)", p.ListName)
	}
	return ToolResult{
		Success:   true,
		Summary:   summary,
		Artifacts: artifacts,
		Warnings:  warnings,
	}, nil
}

// asCheckReminderExists checks whether a reminder with the given name already exists.
func asCheckReminderExists(ctx context.Context, name, listName string) (CheckDuplicateResult, error) {
	var script string
	if listName != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	if exists list %q then
		if (name of reminders of list %q) contains %q then return "true"
	end if
	return "false"
end tell`, listName, listName, name)
	} else {
		script = fmt.Sprintf(`
tell application "Reminders"
	repeat with l in lists
		if (name of reminders of l) contains %q then return "true"
	end repeat
	return "false"
end tell`, name)
	}
	out, err := runAS(ctx, script)
	if err != nil {
		return NoDuplicate, err
	}
	if strings.TrimSpace(out) == "true" {
		desc := fmt.Sprintf("Reminder named %q already exists", name)
		if listName != "" {
			desc = fmt.Sprintf("Reminder named %q already exists in list %q", name, listName)
		}
		return NewDuplicate(desc, "exact name match", "exact"), nil
	}
	return NoDuplicate, nil
}

func asContactsRead(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SearchTerm string `json:"searchTerm"`
	}
	json.Unmarshal(args, &p)

	var script string
	if p.SearchTerm != "" {
		script = fmt.Sprintf(`
tell application "Contacts"
	set results to ""
	set term to %q
	repeat with p in people
		if (first name of p contains term) or (last name of p contains term) then
			set results to results & first name of p & " " & last name of p & "\n"
		end if
	end repeat
	results
end tell`, p.SearchTerm)
	} else {
		script = `
tell application "Contacts"
	set results to ""
	repeat with p in people
		set results to results & first name of p & " " & last name of p & "\n"
	end repeat
	results
end tell`
	}

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("contacts read failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No contacts found.", nil
	}
	return out, nil
}

func asNotesRead(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SearchTerm string `json:"searchTerm"`
	}
	json.Unmarshal(args, &p)

	var script string
	if p.SearchTerm != "" {
		script = fmt.Sprintf(`
tell application "Notes"
	set results to ""
	set term to %q
	repeat with n in notes
		if name of n contains term or body of n contains term then
			set results to results & name of n & "\n---\n" & body of n & "\n\n"
		end if
	end repeat
	results
end tell`, p.SearchTerm)
	} else {
		script = `
tell application "Notes"
	set results to ""
	set noteList to notes
	set limit to 10
	set count to 0
	repeat with n in noteList
		if count < limit then
			set results to results & name of n & "\n"
			set count to count + 1
		end if
	end repeat
	results
end tell`
	}

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("notes read failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No notes found.", nil
	}
	return out, nil
}

func asNotesWrite(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var p struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Title == "" || p.Body == "" {
		return ErrResult("create note", "arg validation", false,
			fmt.Errorf("title and body are required")), nil
	}

	// Dry-run.
	if IsDryRun(ctx) {
		return DryRunResult(
			fmt.Sprintf("would create note %q (%d chars)", p.Title, len(p.Body)),
			fmt.Sprintf("create note titled %q with %d chars of body text", p.Title, len(p.Body)),
			fmt.Sprintf("Notes/%s", p.Title),
		), nil
	}

	// Idempotency check.
	dupCheck, _ := asCheckNoteExists(ctx, p.Title)
	var warnings []string
	if dupCheck.IsDuplicate {
		warnings = append(warnings, DuplicateWarning("note", dupCheck))
	}

	script := fmt.Sprintf(`
tell application "Notes"
	make new note with properties {name:%q, body:%q}
end tell`, p.Title, p.Body)

	if _, err := runAS(ctx, script); err != nil {
		return ErrResult(
			fmt.Sprintf("create note %q", p.Title),
			"AppleScript execution",
			false, err,
		), nil
	}

	mut := NewMutation("created", fmt.Sprintf("note %q", p.Title), "", p.Body)
	return ToolResult{
		Success: true,
		Summary: fmt.Sprintf("Created note: %s", p.Title),
		Artifacts: map[string]any{
			"title":      p.Title,
			"body_chars": len(p.Body),
			"mutation":   mut.ToArtifact(),
		},
		Warnings: warnings,
	}, nil
}

// asCheckNoteExists checks whether a note with the given title already exists in Notes.app.
func asCheckNoteExists(ctx context.Context, title string) (CheckDuplicateResult, error) {
	script := fmt.Sprintf(`tell application "Notes" to (name of notes) contains %q`, title)
	out, err := runAS(ctx, script)
	if err != nil {
		return NoDuplicate, err
	}
	if strings.TrimSpace(out) == "true" {
		return NewDuplicate(
			fmt.Sprintf("Note with title %q already exists", title),
			"exact title match",
			"exact",
		), nil
	}
	return NoDuplicate, nil
}

func asMailRead(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Count int `json:"count"`
	}
	json.Unmarshal(args, &p)
	count := p.Count
	if count <= 0 {
		count = 10
	}

	script := fmt.Sprintf(`
tell application "Mail"
	set results to ""
	set msgs to messages of inbox
	set limit to %d
	set i to 1
	repeat with m in msgs
		if i > limit then exit repeat
		set results to results & subject of m & " | from: " & sender of m & " | date: " & (date received of m as string) & "\n"
		set i to i + 1
	end repeat
	results
end tell`, count)

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("mail read failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No emails found.", nil
	}
	return out, nil
}

func asMailWaitForMessage(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		FromFilter    string `json:"from_filter"`
		SubjectFilter string `json:"subject_filter"`
		TimeoutS      int    `json:"timeout_s"`
		PollS         int    `json:"poll_s"`
	}
	json.Unmarshal(args, &p)

	if p.TimeoutS <= 0 {
		p.TimeoutS = 120
	}
	if p.PollS <= 0 {
		p.PollS = 5
	}

	fromLower := strings.ToLower(p.FromFilter)
	subjLower := strings.ToLower(p.SubjectFilter)

	deadline := time.Now().Add(time.Duration(p.TimeoutS) * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("mail_wait_for_message: cancelled")
		}
		if time.Now().After(deadline) {
			break
		}

		// Fetch the 20 most recent inbox messages and look for a match.
		script := `
tell application "Mail"
	set results to ""
	set msgs to messages of inbox
	set limit to 20
	set i to 1
	repeat with m in msgs
		if i > limit then exit repeat
		set subj to subject of m
		set sndr to sender of m
		set bd to ""
		try
			set bd to (content of m)
			if length of bd > 500 then set bd to text 1 thru 500 of bd
		end try
		set results to results & "---\nsubject: " & subj & "\nfrom: " & sndr & "\nbody: " & bd & "\n"
		set i to i + 1
	end repeat
	results
end tell`

		out, err := runAS(ctx, script)
		if err == nil && strings.TrimSpace(out) != "" {
			for _, block := range strings.Split(out, "---\n") {
				if strings.TrimSpace(block) == "" {
					continue
				}
				blockLower := strings.ToLower(block)
				if fromLower != "" && !strings.Contains(blockLower, fromLower) {
					continue
				}
				if subjLower != "" && !strings.Contains(blockLower, subjLower) {
					continue
				}
				return strings.TrimSpace(block), nil
			}
		}

		// Wait for next poll interval (or ctx cancellation).
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("mail_wait_for_message: cancelled")
		case <-time.After(time.Duration(p.PollS) * time.Second):
		}
	}

	filters := []string{}
	if p.FromFilter != "" {
		filters = append(filters, fmt.Sprintf("from=%q", p.FromFilter))
	}
	if p.SubjectFilter != "" {
		filters = append(filters, fmt.Sprintf("subject=%q", p.SubjectFilter))
	}
	filterStr := ""
	if len(filters) > 0 {
		filterStr = " matching " + strings.Join(filters, " and ")
	}
	return fmt.Sprintf("No email%s arrived within %d seconds.", filterStr, p.TimeoutS), nil
}

func asSafariRead(ctx context.Context, _ json.RawMessage) (string, error) {
	script := `
tell application "Safari"
	if (count of windows) > 0 then
		set w to window 1
		if (count of tabs of w) > 0 then
			set t to current tab of w
			return URL of t & " | " & name of t
		end if
	end if
	return "No open tabs"
end tell`

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("safari read failed: %w", err)
	}
	return out, nil
}

func asSafariNavigate(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	script := fmt.Sprintf(`
tell application "Safari"
	open location %q
end tell`, p.URL)

	_, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("safari navigate failed: %w", err)
	}
	return fmt.Sprintf("Safari navigated to %s", p.URL), nil
}

func asMusicRead(ctx context.Context, _ json.RawMessage) (string, error) {
	script := `
tell application "Music"
	if player state is playing then
		set trackInfo to name of current track & " by " & artist of current track & " | State: playing"
	else if player state is paused then
		set trackInfo to name of current track & " by " & artist of current track & " | State: paused"
	else
		set trackInfo to "Nothing playing"
	end if
	trackInfo
end tell`

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("music read failed: %w", err)
	}
	return out, nil
}

func asMusicControl(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	var action string
	switch p.Command {
	case "play":
		action = "play"
	case "pause":
		action = "pause"
	case "next":
		action = "next track"
	case "previous":
		action = "previous track"
	default:
		return "", fmt.Errorf("unknown command: %s (must be play, pause, next, or previous)", p.Command)
	}

	script := fmt.Sprintf(`tell application "Music" to %s`, action)
	_, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("music control failed: %w", err)
	}
	return fmt.Sprintf("Music: %s", p.Command), nil
}

func asSystemInfo(ctx context.Context, _ json.RawMessage) (string, error) {
	script := `
set sysInfo to ""
set sysInfo to sysInfo & "macOS: " & (system version of (system info)) & "\n"
set sysInfo to sysInfo & "Hostname: " & (computer name of (system info)) & "\n"
sysInfo`

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("system info failed: %w", err)
	}

	// Also get uptime via shell.
	uptime, uptimeErr := runCmd(ctx, "uptime")
	if uptimeErr == nil {
		out += "Uptime: " + uptime
	}
	return out, nil
}

func asRunCustom(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Script == "" {
		return "", fmt.Errorf("script is required")
	}

	out, err := runAS(ctx, p.Script)
	if err != nil {
		return "", fmt.Errorf("custom AppleScript failed: %w", err)
	}
	return out, nil
}

func asMailWrite(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var p struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Send    bool   `json:"send"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.To == "" || p.Subject == "" || p.Body == "" {
		return ErrResult("compose email", "arg validation", false,
			fmt.Errorf("to, subject, and body are required")), nil
	}

	// Dry-run: always describe what would be sent/drafted — critical for send_publish_delete class.
	if IsDryRun(ctx) {
		action := "save as draft"
		if p.Send {
			action = "send immediately"
		}
		return DryRunResult(
			fmt.Sprintf("would %s email to %s: %q", action, p.To, p.Subject),
			fmt.Sprintf("%s email to=%q subject=%q body_chars=%d", action, p.To, p.Subject, len(p.Body)),
			p.To,
		), nil
	}

	appleScriptAction := "-- draft saved"
	if p.Send {
		appleScriptAction = "send newMessage"
	}

	script := fmt.Sprintf(`
tell application "Mail"
	set newMessage to make new outgoing message with properties {subject:%q, content:%q, visible:true}
	tell newMessage
		make new to recipient with properties {address:%q}
	end tell
	%s
end tell`, p.Subject, p.Body, p.To, appleScriptAction)

	if _, err := runAS(ctx, script); err != nil {
		return ErrResult(
			fmt.Sprintf("compose email to %s", p.To),
			"AppleScript execution",
			false, err,
		), nil
	}

	// Echo target and action explicitly for send_publish_delete class.
	var summary, operation string
	if p.Send {
		summary = fmt.Sprintf("Email sent to %s: %s", p.To, p.Subject)
		operation = "sent"
	} else {
		summary = fmt.Sprintf("Email draft saved: %s (to: %s)", p.Subject, p.To)
		operation = "draft_saved"
	}

	mut := NewMutation(operation, fmt.Sprintf("email to %s", p.To), "", fmt.Sprintf("to=%s subject=%s", p.To, p.Subject))
	return ToolResult{
		Success: true,
		Summary: summary,
		Artifacts: map[string]any{
			"to":         p.To,
			"subject":    p.Subject,
			"sent":       p.Send,
			"body_chars": len(p.Body),
			"mutation":   mut.ToArtifact(),
		},
	}, nil
}

func asCalendarListCalendars(ctx context.Context, _ json.RawMessage) (string, error) {
	script := `
tell application "Calendar"
	set calList to ""
	repeat with c in calendars
		set calList to calList & name of c & "\n"
	end repeat
	calList
end tell`

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("calendar list failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No calendars found.", nil
	}
	return "Available calendars:\n" + out, nil
}

func asRemindersListLists(ctx context.Context, _ json.RawMessage) (string, error) {
	script := `
tell application "Reminders"
	set listNames to ""
	repeat with l in lists
		set listNames to listNames & name of l & "\n"
	end repeat
	listNames
end tell`

	out, err := runAS(ctx, script)
	if err != nil {
		return "", fmt.Errorf("reminders list lists failed: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "No reminder lists found.", nil
	}
	return "Reminder lists:\n" + out, nil
}
