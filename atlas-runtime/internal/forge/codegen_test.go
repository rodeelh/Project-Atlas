package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeProposal(t *testing.T, withContract bool) ForgeProposal {
	t.Helper()

	spec := ForgeSkillSpec{
		ID:          "weather-gov",
		Name:        "Weather Gov",
		Description: "US National Weather Service forecasts",
		Actions: []ForgeActionSpec{
			{
				ID:              "weather-gov.forecast",
				Name:            "Forecast",
				Description:     "Get a forecast for a grid point",
				PermissionLevel: "read",
			},
		},
	}
	specJSON, _ := json.Marshal(spec)

	plans := []ForgeActionPlan{
		{
			ActionID: "weather-gov.forecast",
			Type:     "http",
			HTTPRequest: &HTTPRequestPlan{
				Method:  "GET",
				URL:     "https://api.weather.gov/gridpoints/{office}/{gridX},{gridY}/forecast",
				AuthType: "none",
			},
		},
	}
	plansJSON, _ := json.Marshal(plans)

	contractJSON := ""
	if withContract {
		contract := APIResearchContract{
			ProviderName:   "NOAA NWS",
			BaseURL:        "https://api.weather.gov",
			AuthType:       "none",
			RequiredParams: []string{"office", "gridX", "gridY"},
			OptionalParams: []string{"units"},
		}
		b, _ := json.Marshal(contract)
		contractJSON = string(b)
	}

	return ForgeProposal{
		ID:           "prop-001",
		SkillID:      "weather-gov",
		Name:         "Weather Gov",
		Description:  "US National Weather Service forecasts",
		SpecJSON:     string(specJSON),
		PlansJSON:    string(plansJSON),
		ContractJSON: contractJSON,
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestGenerateAndInstallCustomSkill_CreatesFiles verifies that skill.json and
// run are written to <skillsDir>/<skillID>/ with correct permissions.
func TestGenerateAndInstallCustomSkill_CreatesFiles(t *testing.T) {
	supportDir := t.TempDir()

	proposal := makeProposal(t, true)
	if err := GenerateAndInstallCustomSkill(supportDir, proposal); err != nil {
		t.Fatalf("GenerateAndInstallCustomSkill: %v", err)
	}

	skillDir := filepath.Join(supportDir, "skills", "weather-gov")

	// skill.json must exist and be parseable.
	manifestPath := filepath.Join(skillDir, "skill.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("skill.json not written: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("skill.json is not valid JSON: %v", err)
	}

	// Source must be "forge".
	if manifest["source"] != "forge" {
		t.Errorf("manifest source: want %q, got %v", "forge", manifest["source"])
	}
	if manifest["id"] != "weather-gov" {
		t.Errorf("manifest id: want %q, got %v", "weather-gov", manifest["id"])
	}

	// run must exist and be executable.
	runPath := filepath.Join(skillDir, "run")
	info, err := os.Stat(runPath)
	if err != nil {
		t.Fatalf("run not written: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("run is not executable: mode %v", info.Mode())
	}
}

// TestGenerateAndInstallCustomSkill_ParametersFromContract verifies that
// URL-template params + contract params appear in the action's parameter schema.
func TestGenerateAndInstallCustomSkill_ParametersFromContract(t *testing.T) {
	supportDir := t.TempDir()
	proposal := makeProposal(t, true)

	if err := GenerateAndInstallCustomSkill(supportDir, proposal); err != nil {
		t.Fatalf("GenerateAndInstallCustomSkill: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(supportDir, "skills", "weather-gov", "skill.json"))
	var manifest struct {
		Actions []struct {
			Parameters map[string]any `json:"parameters"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(manifest.Actions) == 0 {
		t.Fatal("no actions in manifest")
	}

	params := manifest.Actions[0].Parameters
	if params == nil {
		t.Fatal("parameters is nil")
	}

	props, _ := params["properties"].(map[string]any)
	for _, name := range []string{"office", "gridX", "gridY"} {
		if props[name] == nil {
			t.Errorf("missing required param %q in properties", name)
		}
	}
	// "units" is optional from contract — must also appear.
	if props["units"] == nil {
		t.Errorf("missing optional param %q in properties", "units")
	}

	// Required list must include URL-template params.
	req, _ := params["required"].([]any)
	reqSet := make(map[string]bool, len(req))
	for _, r := range req {
		if s, ok := r.(string); ok {
			reqSet[s] = true
		}
	}
	for _, name := range []string{"office", "gridX", "gridY"} {
		if !reqSet[name] {
			t.Errorf("param %q should be required", name)
		}
	}
}

// TestGenerateAndInstallCustomSkill_RunScriptContainsPlans verifies the
// generated Python script embeds the HTTP plans JSON literal.
func TestGenerateAndInstallCustomSkill_RunScriptContainsPlans(t *testing.T) {
	supportDir := t.TempDir()
	proposal := makeProposal(t, false)

	if err := GenerateAndInstallCustomSkill(supportDir, proposal); err != nil {
		t.Fatalf("GenerateAndInstallCustomSkill: %v", err)
	}

	runSrc, err := os.ReadFile(filepath.Join(supportDir, "skills", "weather-gov", "run"))
	if err != nil {
		t.Fatalf("run not found: %v", err)
	}
	src := string(runSrc)

	// Must contain the URL from the plan.
	if !strings.Contains(src, "api.weather.gov") {
		t.Error("run script does not contain expected API URL")
	}
	// Must contain the Python entry point.
	if !strings.Contains(src, "def main()") {
		t.Error("run script missing main() function")
	}
	if !strings.Contains(src, `if __name__ == "__main__"`) {
		t.Error("run script missing __main__ guard")
	}
	// Must start with shebang.
	if !strings.HasPrefix(src, "#!/usr/bin/env python3") {
		t.Error("run script missing python3 shebang")
	}
}

// TestRemoveCustomSkillDir verifies the directory is removed and that calling
// Remove on a non-existent dir is a no-op (no error).
func TestRemoveCustomSkillDir(t *testing.T) {
	supportDir := t.TempDir()
	proposal := makeProposal(t, false)

	if err := GenerateAndInstallCustomSkill(supportDir, proposal); err != nil {
		t.Fatalf("install: %v", err)
	}

	skillDir := filepath.Join(supportDir, "skills", "weather-gov")
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill dir should exist after install: %v", err)
	}

	if err := RemoveCustomSkillDir(supportDir, "weather-gov"); err != nil {
		t.Fatalf("RemoveCustomSkillDir: %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill dir should be gone after remove")
	}

	// Second remove must not error.
	if err := RemoveCustomSkillDir(supportDir, "weather-gov"); err != nil {
		t.Fatalf("second RemoveCustomSkillDir should be no-op: %v", err)
	}
}

// TestBuildParamSchema_NoParams verifies nil is returned when there are no
// parameters to describe (avoids injecting an empty schema into the manifest).
func TestBuildParamSchema_NoParams(t *testing.T) {
	schema := buildParamSchema(nil, nil, nil)
	if schema != nil {
		t.Errorf("expected nil schema for empty inputs, got %v", schema)
	}
}

// TestBuildParamSchema_URLTemplateOnly verifies URL-template params become
// required in the generated schema.
func TestBuildParamSchema_URLTemplateOnly(t *testing.T) {
	plan := &HTTPRequestPlan{
		Method: "GET",
		URL:    "https://api.example.com/users/{userID}/posts/{postID}",
	}
	schema := buildParamSchema(plan, nil, nil)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	props, _ := schema["properties"].(map[string]any)
	if props["userID"] == nil || props["postID"] == nil {
		t.Errorf("missing URL params in properties: %v", props)
	}
	req, _ := schema["required"].([]string)
	reqSet := make(map[string]bool)
	for _, r := range req {
		reqSet[r] = true
	}
	if !reqSet["userID"] || !reqSet["postID"] {
		t.Errorf("URL params should be required: %v", req)
	}
}
