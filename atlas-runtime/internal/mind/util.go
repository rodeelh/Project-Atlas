package mind

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxFileSize = 50 * 1024 // 50 KB — sanity cap on AI-generated file content

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
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// truncate returns the first n runes of s.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// truncateSandwich keeps the first half and last half of s when it exceeds n
// runes, joining them with an ellipsis marker. This preserves both the opening
// context (Who I Am, Understanding of You) and the recent tail (Today's Read)
// of MIND.md when the document grows too large for the context window.
func truncateSandwich(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	half := n / 2
	sep := []rune("\n\n...[middle truncated for context window]...\n\n")
	result := make([]rune, 0, half+len(sep)+half)
	result = append(result, runes[:half]...)
	result = append(result, sep...)
	result = append(result, runes[len(runes)-half:]...)
	return string(result)
}

// shortID returns the first 8 chars of an ID for log metadata.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// validateMindContent checks that an AI-generated MIND.md is within size bounds
// and starts with the expected header.
func validateMindContent(content string) error {
	if len(content) > maxFileSize {
		return fmt.Errorf("AI returned oversized MIND.md (%d bytes > %d limit)", len(content), maxFileSize)
	}
	if !strings.HasPrefix(strings.TrimSpace(content), "# Mind of Atlas") {
		return fmt.Errorf("AI returned invalid MIND.md: missing '# Mind of Atlas' header")
	}
	return nil
}

// validateSkillsContent checks that an AI-generated SKILLS.md is within size
// bounds and starts with the expected header.
func validateSkillsContent(content string) error {
	if len(content) > maxFileSize {
		return fmt.Errorf("AI returned oversized SKILLS.md (%d bytes > %d limit)", len(content), maxFileSize)
	}
	if !strings.HasPrefix(strings.TrimSpace(content), "# Skill Memory") {
		return fmt.Errorf("AI returned invalid SKILLS.md: missing '# Skill Memory' header")
	}
	return nil
}
