// Custom skills — user-installed executables called via subprocess JSON protocol.
// Manifest types and filesystem scanning live in internal/customskills to avoid
// import cycles (skills/diary.go already imports features).
//
// Protocol: Atlas writes one JSON line to stdin, reads one JSON line from stdout.
//
//	stdin:  {"action":"search","args":{"query":"..."}}
//	stdout: {"success":true,"output":"..."} | {"success":false,"error":"..."}
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atlas-runtime-go/internal/customskills"
	"atlas-runtime-go/internal/logstore"
)

// LoadCustomSkills scans the custom skills directory and registers each valid skill into r.
// Designed to be called once at startup after NewRegistry. Non-fatal — a broken custom
// skill never prevents Atlas from starting.
func (r *Registry) LoadCustomSkills(supportDir string) {
	manifests := customskills.ListManifests(supportDir)
	for _, manifest := range manifests {
		runPath := filepath.Join(manifest.SkillDir, "run")
		count := 0
		for _, action := range manifest.Actions {
			r.registerCustomAction(manifest, action, manifest.SkillDir, runPath)
			count++
		}
		logstore.Write("info",
			fmt.Sprintf("custom skills: loaded %s v%s (%d action(s))", manifest.ID, manifest.Version, count),
			map[string]string{"skillDir": manifest.SkillDir})
	}
}

// registerCustomAction registers a single action from a custom skill manifest.
func (r *Registry) registerCustomAction(manifest customskills.CustomSkillManifest, action customskills.CustomSkillAction, skillDir, runPath string) {
	actionID := manifest.ID + "." + action.Name
	ac := parseActionClass(action.ActionClass, action.PermLevel)

	def := ToolDef{
		Name:        actionID,
		Description: action.Description,
	}
	if action.Parameters != nil {
		def.RawSchema = action.Parameters
	} else {
		// Empty params — skill accepts no arguments.
		def.Properties = map[string]ToolParam{}
		def.Required = []string{}
	}

	permLevel := action.PermLevel
	if permLevel == "" {
		permLevel = "execute"
	}

	// Capture loop variable for the closure.
	actionName := action.Name

	r.register(SkillEntry{
		Def:         def,
		PermLevel:   permLevel,
		ActionClass: ac,
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return callCustomSkill(ctx, runPath, skillDir, actionName, args)
		},
	})
}

// parseActionClass converts the action_class string from skill.json to an ActionClass constant.
// Falls back to defaultActionClass(permLevel) for unknown or empty strings.
func parseActionClass(class, permLevel string) ActionClass {
	switch strings.ToLower(class) {
	case "read":
		return ActionClassRead
	case "local_write":
		return ActionClassLocalWrite
	case "destructive_local":
		return ActionClassDestructiveLocal
	case "external_side_effect":
		return ActionClassExternalSideEffect
	case "send_publish_delete":
		return ActionClassSendPublishDelete
	}
	return defaultActionClass(permLevel)
}

// callCustomSkill invokes the skill's run executable.
// Protocol: one JSON line to stdin → one JSON line from stdout.
func callCustomSkill(ctx context.Context, runPath, workDir, actionName string, args json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	type callInput struct {
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}
	inputData, err := json.Marshal(callInput{Action: actionName, Args: args})
	if err != nil {
		return "", fmt.Errorf("custom skill: marshal input: %w", err)
	}

	cmd := exec.CommandContext(ctx, runPath) //nolint:gosec — user-installed executable; intentional
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(string(inputData) + "\n")

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("custom skill %s: timed out after 30s", actionName)
		}
		// Include stderr for better error messages.
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("custom skill %s: %s", actionName, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("custom skill %s: exec failed: %w", actionName, err)
	}

	// Limit output to 1 MB before handing to the model.
	const maxOutput = 1 << 20
	if len(out) > maxOutput {
		out = out[:maxOutput]
	}

	// Parse the output JSON envelope.
	var result struct {
		Success bool   `json:"success"`
		Output  string `json:"output"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		// Non-JSON output — return as plain text (legacy / simple scripts).
		return strings.TrimSpace(string(out)), nil
	}
	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "custom skill returned failure with no error message"
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	return result.Output, nil
}
