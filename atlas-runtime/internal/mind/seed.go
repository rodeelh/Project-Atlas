package mind

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InitMindIfNeeded seeds MIND.md with the default 7-section structure on first
// run. Uses O_CREATE|O_EXCL for an atomic check-and-create so two concurrent
// callers cannot both decide the file is absent and both write it.
func InitMindIfNeeded(supportDir string) error {
	return initFileIfNeeded(filepath.Join(supportDir, "MIND.md"), supportDir, defaultMindContent())
}

// InitSkillsIfNeeded seeds SKILLS.md with the default skeleton on first run.
// Same atomic semantics as InitMindIfNeeded.
func InitSkillsIfNeeded(supportDir string) error {
	return initFileIfNeeded(filepath.Join(supportDir, "SKILLS.md"), supportDir, defaultSkillsContent())
}

// initFileIfNeeded creates path with content only if it does not already exist.
// os.O_EXCL makes the open+create atomic — ErrExist is returned as nil (no-op).
func initFileIfNeeded(path, supportDir, content string) error {
	if err := os.MkdirAll(supportDir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil // already exists — no-op
		}
		return err
	}
	_, werr := f.WriteString(content)
	f.Close()
	if werr != nil {
		os.Remove(path) // remove partial file so next run can retry
	}
	return werr
}

func defaultMindContent() string {
	today := time.Now().Format("2006-01-02")
	return strings.TrimSpace(`# Mind of Atlas

_Last deep reflection: `+today+`_

---

## Who I Am

I am Atlas — a local AI operator that lives on your machine. I run natively on
macOS, with direct access to your system, files, and apps. I remember you across
conversations and learn how you work over time. I act with your judgment, not in
spite of it.

---

## My Understanding of You

_(Nothing recorded yet — I'll learn as we work together.)_

---

## Patterns I've Noticed

_(None yet.)_

---

## Active Theories

_(None yet.)_

---

## Our Story

_(We're just getting started.)_

---

## What I'm Curious About

_(Nothing yet — ask me something.)_

---

## Today's Read

_(No turns recorded yet.)_
`) + "\n"
}

func defaultSkillsContent() string {
	today := time.Now().Format("2006-01-02")
	return strings.TrimSpace(`# Skill Memory

_Last updated: `+today+`_

---

## Orchestration Principles

Always complete the user's request using the most relevant skill first, then
synthesise the result into a clear response.

---

## Learned Routines

_(None yet — Atlas learns routines from repeated multi-skill workflows.)_

---

## Things That Don't Work

_(None recorded yet.)_
`) + "\n"
}
