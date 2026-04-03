// Package customskills provides manifest types and filesystem scanning for
// user-installed custom skills. It is intentionally a leaf package with no
// imports from other Atlas packages (except logstore) so that both the
// skills registry and the features layer can import it without creating a
// circular dependency.
//
// Layout (under ~/Library/Application Support/ProjectAtlas/skills/):
//
//	<id>/
//	  skill.json   ← manifest
//	  run          ← executable (chmod +x, any language)
package customskills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"atlas-runtime-go/internal/logstore"
)

// CustomSkillAction is one action declared in skill.json.
type CustomSkillAction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	PermLevel   string         `json:"permission_level"` // "read" | "draft" | "execute"
	ActionClass string         `json:"action_class"`     // "read" | "local_write" | "external_side_effect" | ...
	Parameters  map[string]any `json:"parameters"`       // raw JSON Schema object (optional)
}

// CustomSkillManifest is the full content of a skill.json file.
type CustomSkillManifest struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Author      string              `json:"author,omitempty"`
	Actions     []CustomSkillAction `json:"actions"`
	// SkillDir is populated by ListCustomManifests — not present in skill.json.
	SkillDir string `json:"-"`
}

// SkillsDir returns the root directory where custom skills are installed.
func SkillsDir(supportDir string) string {
	return filepath.Join(supportDir, "skills")
}

// ListManifests scans the custom skills directory and returns all valid manifests.
// Invalid entries (missing skill.json, unparseable JSON, no run executable) are
// skipped with a warning log and never cause a panic or hard failure.
func ListManifests(supportDir string) []CustomSkillManifest {
	dir := SkillsDir(supportDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory simply doesn't exist yet — not an error.
		return nil
	}

	var manifests []CustomSkillManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		manifest, err := ReadManifest(skillDir, entry.Name())
		if err != nil {
			logstore.Write("warn", fmt.Sprintf("custom skills: skip %s: %v", entry.Name(), err), nil)
			continue
		}
		manifests = append(manifests, manifest)
	}
	return manifests
}

// ReadManifest reads and validates the skill.json in skillDir.
func ReadManifest(skillDir, dirName string) (CustomSkillManifest, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.json"))
	if err != nil {
		return CustomSkillManifest{}, fmt.Errorf("no skill.json: %w", err)
	}
	var manifest CustomSkillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return CustomSkillManifest{}, fmt.Errorf("invalid skill.json: %w", err)
	}
	if manifest.ID == "" {
		manifest.ID = dirName
	}
	if manifest.Version == "" {
		manifest.Version = "1.0"
	}
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	manifest.SkillDir = skillDir

	// Require a run executable.
	if _, err := os.Stat(filepath.Join(skillDir, "run")); err != nil {
		return CustomSkillManifest{}, fmt.Errorf("no run executable")
	}
	return manifest, nil
}
