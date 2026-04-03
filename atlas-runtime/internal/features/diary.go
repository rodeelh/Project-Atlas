package features

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var diaryMu sync.Mutex

const (
	diaryFile      = "DIARY.md"
	diaryMaxPerDay = 3 // maximum entries Atlas will record per calendar day
)

// DiaryEntry is a single diary record.
type DiaryEntry struct {
	Date  string // "2006-01-02"
	Entry string // the one-line note
}

// ReadDiary returns the raw DIARY.md content, or "" if the file doesn't exist.
func ReadDiary(supportDir string) string {
	data, err := os.ReadFile(filepath.Join(supportDir, diaryFile))
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteDiary replaces the entire DIARY.md content atomically.
func WriteDiary(supportDir, content string) error {
	diaryMu.Lock()
	defer diaryMu.Unlock()
	path := filepath.Join(supportDir, diaryFile)
	if err := os.MkdirAll(supportDir, 0o700); err != nil {
		return err
	}
	return atomicWrite(path, []byte(strings.TrimSpace(content)), 0o600)
}

// AppendDiaryEntry adds a single entry under today's date heading.
// Enforces diaryMaxPerDay: if today already has that many entries the call
// is a no-op and returns ("", nil). Returns the entry text that was written,
// or "" if skipped.
func AppendDiaryEntry(supportDir, entry string) (string, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", nil
	}

	diaryMu.Lock()
	defer diaryMu.Unlock()

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(supportDir, diaryFile)

	raw := ReadDiary(supportDir)

	// Count how many entries today already has.
	todayHeading := "## " + today
	if countTodayEntries(raw, todayHeading) >= diaryMaxPerDay {
		return "", nil // already at the daily limit
	}

	var sb strings.Builder
	if raw == "" {
		sb.WriteString("# Atlas Diary\n")
	} else {
		sb.WriteString(strings.TrimRight(raw, "\n"))
	}

	// If today's heading doesn't exist yet, add it.
	if !strings.Contains(raw, todayHeading) {
		sb.WriteString("\n\n")
		sb.WriteString(todayHeading)
	}

	sb.WriteString("\n- ")
	sb.WriteString(entry)
	sb.WriteString("\n")

	if err := os.MkdirAll(supportDir, 0o700); err != nil {
		return "", err
	}
	if err := atomicWrite(path, []byte(sb.String()), 0o600); err != nil {
		return "", err
	}
	return entry, nil
}

// DiaryContext returns the last nDays of diary entries as a formatted string
// suitable for injection into the system prompt. Returns "" when the diary
// is empty or the file doesn't exist.
func DiaryContext(supportDir string, nDays int) string {
	raw := ReadDiary(supportDir)
	if raw == "" {
		return ""
	}

	// Collect headings that fall within the nDays window.
	cutoff := time.Now().AddDate(0, 0, -nDays).Format("2006-01-02")
	var out []string
	var current []string // lines for the current heading
	currentDate := ""

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "## ") {
			// Flush previous block if in window.
			if currentDate != "" && currentDate >= cutoff && len(current) > 0 {
				out = append(out, strings.Join(current, "\n"))
			}
			currentDate = strings.TrimPrefix(line, "## ")
			current = []string{line}
		} else if currentDate != "" {
			current = append(current, line)
		}
	}
	// Flush final block.
	if currentDate != "" && currentDate >= cutoff && len(current) > 0 {
		out = append(out, strings.Join(current, "\n"))
	}

	if len(out) == 0 {
		return ""
	}
	return fmt.Sprintf("## Atlas Diary (last %d days)\n\n%s", nDays, strings.Join(out, "\n\n"))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func countTodayEntries(raw, todayHeading string) int {
	idx := strings.Index(raw, todayHeading)
	if idx < 0 {
		return 0
	}
	// Count bullet lines between this heading and the next "## " heading.
	section := raw[idx+len(todayHeading):]
	next := strings.Index(section, "\n## ")
	if next >= 0 {
		section = section[:next]
	}
	count := 0
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			count++
		}
	}
	return count
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Chmod(perm) //nolint:errcheck
	tmp.Close()
	return os.Rename(tmpPath, path)
}
