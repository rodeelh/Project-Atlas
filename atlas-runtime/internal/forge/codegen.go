package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"atlas-runtime-go/internal/customskills"
	"atlas-runtime-go/internal/logstore"
)

// GenerateAndInstallCustomSkill generates a skill.json manifest and a
// stdlib-only Python run script from a ForgeProposal, then writes them into
// the custom skills directory so that LoadCustomSkills() picks them up on the
// next registry refresh (daemon restart or hot-reload).
//
// The generated skill carries Source: "forge" in its manifest so that it
// continues to appear with the Forge badge in the Skills UI instead of the
// generic Custom badge.
func GenerateAndInstallCustomSkill(supportDir string, proposal ForgeProposal) error {
	// ── 1. Parse embedded JSON strings ───────────────────────────────────────
	var spec ForgeSkillSpec
	if err := json.Unmarshal([]byte(proposal.SpecJSON), &spec); err != nil {
		return fmt.Errorf("forge/codegen: bad specJSON: %w", err)
	}

	var plans []ForgeActionPlan
	if err := json.Unmarshal([]byte(proposal.PlansJSON), &plans); err != nil {
		return fmt.Errorf("forge/codegen: bad plansJSON: %w", err)
	}

	var contract *APIResearchContract
	if proposal.ContractJSON != "" {
		var c APIResearchContract
		if err := json.Unmarshal([]byte(proposal.ContractJSON), &c); err != nil {
			logstore.Write("warn", "forge/codegen: bad contractJSON — skipping parameter hints: "+err.Error(), nil)
		} else {
			contract = &c
		}
	}

	// ── 2. Build actions for skill.json ──────────────────────────────────────
	actions := buildManifestActions(spec, plans, contract)

	manifest := customskills.CustomSkillManifest{
		ID:          proposal.SkillID,
		Name:        proposal.Name,
		Version:     "1.0",
		Description: proposal.Description,
		Source:      "forge",
		Actions:     actions,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("forge/codegen: marshal skill.json: %w", err)
	}

	// ── 3. Generate Python run script ─────────────────────────────────────────
	plansData, err := json.Marshal(plans)
	if err != nil {
		return fmt.Errorf("forge/codegen: marshal plans: %w", err)
	}
	runScript := buildRunScript(string(plansData))

	// ── 4. Write files to skills directory ───────────────────────────────────
	skillDir := filepath.Join(customskills.SkillsDir(supportDir), proposal.SkillID)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("forge/codegen: mkdir %s: %w", skillDir, err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.json"), manifestData, 0o644); err != nil {
		return fmt.Errorf("forge/codegen: write skill.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "run"), []byte(runScript), 0o755); err != nil {
		return fmt.Errorf("forge/codegen: write run: %w", err)
	}

	logstore.Write("info", fmt.Sprintf("forge/codegen: installed %q → %s", proposal.SkillID, skillDir), nil)
	return nil
}

// RemoveCustomSkillDir removes the generated custom skill directory for a
// forge-installed skill.  Returns nil if the directory does not exist.
func RemoveCustomSkillDir(supportDir, skillID string) error {
	skillDir := filepath.Join(customskills.SkillsDir(supportDir), skillID)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return nil
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("forge/codegen: remove %s: %w", skillDir, err)
	}
	return nil
}

// ── Parameter schema helpers ─────────────────────────────────────────────────

// urlParamRe matches {param} placeholders in URL and body-field templates.
var urlParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// buildManifestActions derives []CustomSkillAction from spec + plans + contract.
func buildManifestActions(
	spec ForgeSkillSpec,
	plans []ForgeActionPlan,
	contract *APIResearchContract,
) []customskills.CustomSkillAction {
	// Contract-level required / optional hint lists (may be nil).
	var contractRequired, contractOptional []string
	var paramLocations map[string]string
	if contract != nil {
		contractRequired = contract.RequiredParams
		contractOptional = contract.OptionalParams
		paramLocations = contract.ParamLocations
	}
	_ = paramLocations // informational — not needed for schema generation

	actions := make([]customskills.CustomSkillAction, 0, len(spec.Actions))
	for _, sa := range spec.Actions {
		// Find matching HTTP plan.
		var plan *HTTPRequestPlan
		for i := range plans {
			if plans[i].ActionID == sa.ID {
				plan = plans[i].HTTPRequest
				break
			}
		}

		params := buildParamSchema(plan, contractRequired, contractOptional)

		permLevel := sa.PermissionLevel
		if permLevel == "" {
			permLevel = "read"
		}

		actionClass := "read"
		if plan != nil {
			switch strings.ToUpper(plan.Method) {
			case "POST", "PUT", "PATCH", "DELETE":
				actionClass = "external_side_effect"
			}
		}

		actions = append(actions, customskills.CustomSkillAction{
			Name:        slugify(sa.Name),
			Description: sa.Description,
			PermLevel:   permLevel,
			ActionClass: actionClass,
			Parameters:  params,
		})
	}
	return actions
}

// buildParamSchema creates a JSON Schema "object" map from all param sources.
// Returns nil when there are no parameters to describe (model won't see a
// parameters field and can call the action with no arguments).
func buildParamSchema(plan *HTTPRequestPlan, required, optional []string) map[string]any {
	seen := make(map[string]bool)
	var allParams []string
	var reqParams []string

	add := func(name string, isRequired bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		allParams = append(allParams, name)
		if isRequired {
			reqParams = append(reqParams, name)
		}
	}

	if plan != nil {
		// URL template placeholders → required (they must be filled in).
		for _, m := range urlParamRe.FindAllStringSubmatch(plan.URL, -1) {
			add(m[1], true)
		}
		// Body field templates.
		for _, v := range plan.BodyFields {
			for _, m := range urlParamRe.FindAllStringSubmatch(v, -1) {
				add(m[1], false)
			}
		}
		// Query param templates.
		for _, v := range plan.Query {
			for _, m := range urlParamRe.FindAllStringSubmatch(v, -1) {
				add(m[1], false)
			}
		}
	}

	for _, p := range required {
		add(p, true)
	}
	for _, p := range optional {
		add(p, false)
	}

	if len(allParams) == 0 {
		return nil
	}

	properties := make(map[string]any, len(allParams))
	for _, p := range allParams {
		properties[p] = map[string]any{
			"type":        "string",
			"description": p,
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(reqParams) > 0 {
		schema["required"] = reqParams
	}
	return schema
}

// ── Python run script generator ──────────────────────────────────────────────

// buildRunScript returns a self-contained, stdlib-only Python 3 script that
// implements the Atlas custom skill subprocess protocol (one JSON line in,
// one JSON line out).  The HTTP plans are embedded as a JSON constant so the
// script is completely standalone — no external dependencies or network calls
// other than the actual API request.
func buildRunScript(plansJSON string) string {
	const header = `#!/usr/bin/env python3
"""
Atlas Forge-generated skill runner.
Auto-generated — do not edit manually.
Protocol: one JSON line on stdin, one JSON line on stdout.
  stdin:  {"action": "<name>", "args": {"param": "value", ...}}
  stdout: {"success": true,  "output": "<text>"}
          {"success": false, "error":  "<message>"}
"""
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

PLANS = `

	const footer = `

def _find_plan(action_name):
    for p in PLANS:
        aid = p.get("actionID", "")
        if aid == action_name or aid.rsplit(".", 1)[-1] == action_name:
            return p.get("httpRequest")
    return None

def _secret(key):
    """Resolve a credential from environment variables injected by Atlas."""
    if not key:
        return ""
    for variant in (key, key.upper(), key.upper().replace("-", "_").replace(".", "_")):
        val = os.environ.get(variant)
        if val is not None:
            return val
    return ""

def _fill(template, args):
    """Replace {param} placeholders with values from args."""
    return re.sub(r"\{([^}]+)\}", lambda m: str(args.get(m.group(1), m.group(0))), template)

def _run(plan, args):
    method = plan.get("method", "GET").upper()
    url = _fill(plan.get("url", ""), args)

    headers = dict(plan.get("headers") or {})
    query   = {k: _fill(v, args) for k, v in (plan.get("query") or {}).items()}
    body_fields  = dict(plan.get("bodyFields") or {})
    static_body  = dict(plan.get("staticBodyFields") or {})

    auth_type        = plan.get("authType", "none")
    auth_secret_key  = plan.get("authSecretKey", "")
    auth_header_name = plan.get("authHeaderName", "")
    auth_query_param = plan.get("authQueryParamName", "")
    secret_header    = plan.get("secretHeader", "")
    secret = _secret(auth_secret_key)

    # Apply authentication.
    if auth_type == "bearer" or secret_header:
        headers["Authorization"] = "Bearer " + secret
    elif auth_type == "api_key_header" and auth_header_name:
        headers[auth_header_name] = secret
    elif auth_type == "query_param" and auth_query_param:
        query[auth_query_param] = secret
    elif auth_type == "basic":
        parts = secret.split(":", 1)
        u, p = parts[0], parts[1] if len(parts) > 1 else ""
        encoded = base64.b64encode(f"{u}:{p}".encode()).decode()
        headers["Authorization"] = "Basic " + encoded

    # Build request body from static + dynamic fields.
    body = dict(static_body)
    for field, tmpl in body_fields.items():
        body[field] = _fill(tmpl, args)

    # Route remaining args: into body for mutation methods, query for reads.
    placed = set(re.findall(r"\{([^}]+)\}", plan.get("url", "")))
    for tmpl in body_fields.values():
        placed.update(re.findall(r"\{([^}]+)\}", tmpl))
    for k, v in args.items():
        if k not in placed and k not in query and k not in body:
            if method in ("POST", "PUT", "PATCH"):
                body[k] = v
            else:
                query[k] = str(v)

    # Append query string.
    if query:
        sep = "&" if "?" in url else "?"
        url = url + sep + urllib.parse.urlencode(query)

    data = None
    if method in ("POST", "PUT", "PATCH"):
        data = json.dumps(body).encode("utf-8")
        headers.setdefault("Content-Type", "application/json")

    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=25) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            try:
                return json.dumps(json.loads(raw), indent=2), None
            except json.JSONDecodeError:
                return raw, None
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")[:500]
        return None, f"HTTP {e.code} {e.reason}: {detail}"
    except Exception as exc:
        return None, str(exc)

def main():
    line = sys.stdin.readline()
    if not line.strip():
        print(json.dumps({"success": False, "error": "empty input"}))
        return
    try:
        req = json.loads(line)
    except json.JSONDecodeError as exc:
        print(json.dumps({"success": False, "error": "invalid JSON: " + str(exc)}))
        return

    action = req.get("action", "")
    args   = req.get("args") or {}

    plan = _find_plan(action)
    if plan is None:
        print(json.dumps({"success": False, "error": f"unknown action: {action}"}))
        return

    output, err = _run(plan, args)
    if err is not None:
        print(json.dumps({"success": False, "error": err}))
    else:
        print(json.dumps({"success": True, "output": output}))

if __name__ == "__main__":
    main()
`
	return header + plansJSON + footer
}
