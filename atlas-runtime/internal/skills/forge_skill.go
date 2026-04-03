package skills

// forge_skill.go — forge.orchestration.propose with full 8-gate validation pipeline.
//
// This file intentionally does NOT import atlas-runtime-go/internal/forge to
// avoid an import cycle (agent → skills → forge → agent). Forge persistence
// is injected at startup via Registry.SetForgePersistFn. The Forge types used
// here (forgeSpec, forgePlan, etc.) are local mirrors of forge.ForgeSkillSpec
// and friends — structurally identical, kept in sync manually.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"atlas-runtime-go/internal/validate"
)

// ── Local type mirrors ────────────────────────────────────────────────────────
// These match forge.ForgeSkillSpec / ForgeActionPlan / HTTPRequestPlan /
// APIResearchContract exactly. Keep in sync with forge/types.go.

type forgeSpec struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Category    string           `json:"category"`
	RiskLevel   string           `json:"riskLevel"`
	Tags        []string         `json:"tags"`
	Actions     []forgeActionSpec `json:"actions"`
}

type forgeActionSpec struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permissionLevel"`
}

type forgePlan struct {
	ActionID    string           `json:"actionID"`
	Type        string           `json:"type"`
	HTTPRequest *forgeHTTPRequest `json:"httpRequest"`
}

type forgeHTTPRequest struct {
	Method                string            `json:"method"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers"`
	Query                 map[string]string `json:"query"`
	AuthType              string            `json:"authType"`
	AuthSecretKey         string            `json:"authSecretKey"`
	AuthHeaderName        string            `json:"authHeaderName"`
	AuthQueryParamName    string            `json:"authQueryParamName"`
	OAuth2TokenURL        string            `json:"oauth2TokenURL"`
	OAuth2ClientIDKey     string            `json:"oauth2ClientIDKey"`
	OAuth2ClientSecretKey string            `json:"oauth2ClientSecretKey"`
	OAuth2Scope           string            `json:"oauth2Scope"`
	BodyFields            map[string]string `json:"bodyFields"`
	StaticBodyFields      map[string]string `json:"staticBodyFields"`
	SecretHeader          string            `json:"secretHeader"`
}

type forgeContract struct {
	ProviderName           string            `json:"providerName"`
	DocsURL                string            `json:"docsURL"`
	DocsQuality            string            `json:"docsQuality"`
	BaseURL                string            `json:"baseURL"`
	Endpoint               string            `json:"endpoint"`
	Method                 string            `json:"method"`
	AuthType               string            `json:"authType"`
	RequiredParams         []string          `json:"requiredParams"`
	OptionalParams         []string          `json:"optionalParams"`
	ParamLocations         map[string]string `json:"paramLocations"`
	ExampleRequest         string            `json:"exampleRequest"`
	ExampleResponse        string            `json:"exampleResponse"`
	ExpectedResponseFields []string          `json:"expectedResponseFields"`
	MappingConfidence      string            `json:"mappingConfidence"`
	ValidationStatus       string            `json:"validationStatus"`
	Notes                  string            `json:"notes"`
}

// ── Registration ──────────────────────────────────────────────────────────────

func (r *Registry) registerForge() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name: "forge.orchestration.propose",
			Description: "Propose a new Forge skill. For API skills you MUST research the API first " +
				"and provide contract_json — the proposal is refused if the contract fails quality gates. " +
				"For non-API skills (composed/transform/workflow) contract_json is not required.",
			Properties: map[string]ToolParam{
				"kind": {
					Description: "Skill kind: 'api' (calls external HTTP API), 'composed' (chains Atlas skills), " +
						"'transform' (converts data), 'workflow' (sequences steps). Defaults to 'api'.",
					Type: "string",
					Enum: []string{"api", "composed", "transform", "workflow"},
				},
				"contract_json": {
					Description: "Required for api skills. JSON-encoded APIResearchContract capturing what you " +
						"learned from researching the API: providerName, docsURL, docsQuality (low/medium/high), " +
						"baseURL, endpoint, method, authType, requiredParams, optionalParams, paramLocations, " +
						"exampleRequest, exampleResponse, expectedResponseFields, " +
						"mappingConfidence (must be 'high'), validationStatus, notes.",
					Type: "string",
				},
				"spec_json": {
					Description: "JSON-encoded ForgeSkillSpec. Must include: id (lowercase-hyphenated), name, " +
						"description, category, riskLevel (low/medium/high), tags, and actions " +
						"(array with id, name, description, permissionLevel).",
					Type: "string",
				},
				"plans_json": {
					Description: "JSON-encoded array of ForgeActionPlan. Each element: " +
						`{"actionID":"<id>","type":"http","httpRequest":{"method":"GET|POST|PUT|PATCH|DELETE",` +
						`"url":"https://...","authType":"none|apiKeyHeader|apiKeyQuery|bearerTokenStatic|basicAuth|oauth2ClientCredentials",` +
						`"authSecretKey":"com.projectatlas.myapi","authHeaderName":"X-API-Key",...}}. ` +
						"Use {paramName} in the URL for path substitution. For GET: remaining inputs become " +
						"query params. For POST/PUT/PATCH: use bodyFields to map input params to API body keys.",
					Type: "string",
				},
				"summary": {
					Description: "Human-readable explanation of what this skill does and why it is useful. " +
						"Displayed to the user in the Forge approval UI.",
					Type: "string",
				},
				"rationale": {
					Description: "Optional: why you are proposing this skill now — what user request triggered it.",
					Type: "string",
				},
			},
			Required: []string{"spec_json", "plans_json", "summary"},
		},
		PermLevel: "draft",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return r.forgeOrchestrationPropose(ctx, args)
		},
	})
}

// ── Execution ─────────────────────────────────────────────────────────────────

func (r *Registry) forgeOrchestrationPropose(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Kind         string `json:"kind"`
		ContractJSON string `json:"contract_json"`
		SpecJSON     string `json:"spec_json"`
		PlansJSON    string `json:"plans_json"`
		Summary      string `json:"summary"`
		Rationale    string `json:"rationale"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if strings.TrimSpace(p.SpecJSON) == "" {
		return "forge.orchestration.propose requires 'spec_json'. Provide a JSON-encoded ForgeSkillSpec.", nil
	}
	if strings.TrimSpace(p.PlansJSON) == "" {
		return "forge.orchestration.propose requires 'plans_json'. Provide a JSON-encoded array of ForgeActionPlan.", nil
	}
	if strings.TrimSpace(p.Summary) == "" {
		return "forge.orchestration.propose requires 'summary'. Provide a brief description of what the skill does.", nil
	}
	if p.Kind == "" {
		p.Kind = "api"
	}

	// Decode spec and plans for gate evaluation.
	var spec forgeSpec
	if err := json.Unmarshal([]byte(p.SpecJSON), &spec); err != nil {
		return fmt.Sprintf("Could not decode spec_json as ForgeSkillSpec: %v. Ensure spec_json is well-formed JSON.", err), nil
	}
	var plans []forgePlan
	if err := json.Unmarshal([]byte(p.PlansJSON), &plans); err != nil {
		return fmt.Sprintf("Could not decode plans_json as []ForgeActionPlan: %v. Ensure plans_json is a well-formed JSON array.", err), nil
	}

	// ── Validation pipeline ────────────────────────────────────────────────────

	var persistedContractJSON string

	if p.Kind == "api" {
		if strings.TrimSpace(p.ContractJSON) == "" {
			return "forge.orchestration.propose requires 'contract_json' for API skills. " +
				"Research the target API first, then provide a populated APIResearchContract. " +
				"Set kind to 'composed', 'transform', or 'workflow' if this is not an HTTP API skill.", nil
		}

		var contract forgeContract
		if err := json.Unmarshal([]byte(p.ContractJSON), &contract); err != nil {
			return fmt.Sprintf("Could not decode contract_json as APIResearchContract: %v. "+
				"Ensure contract_json is a valid JSON object with providerName, docsQuality, "+
				"mappingConfidence, and other required fields.", err), nil
		}

		// Gates 1–6: contract quality.
		if msg := forgeValidateContract(contract); msg != "" {
			return msg, nil
		}

		// Gate 7: auth plan field completeness.
		if msg := forgeValidatePlansAuth(plans); msg != "" {
			return msg, nil
		}

		// Gate 8: credential readiness.
		if msg := forgeValidateCredentials(plans); msg != "" {
			return msg, nil
		}

		// Live API pre-validation via validate.Gate.
		if msg := r.forgeValidateAPI(ctx, contract, plans); msg != "" {
			return msg, nil
		}

		persistedContractJSON = p.ContractJSON

	} else {
		// Non-API kinds: still check auth field completeness.
		if msg := forgeValidatePlansAuth(plans); msg != "" {
			return msg, nil
		}
	}

	// Spec structural validation (all kinds).
	if msg := forgeValidateSpec(spec); msg != "" {
		return msg, nil
	}

	// Guard: persistence callback must be injected.
	if r.forgePersistFn == nil {
		return "Forge is not yet ready — the runtime is still initialising. Please try again in a moment.", nil
	}

	// Persist the proposal.
	id, name, skillID, riskLevel, actionNames, domains, err := r.forgePersistFn(
		p.SpecJSON, p.PlansJSON, p.Summary, p.Rationale, persistedContractJSON,
	)
	if err != nil {
		return fmt.Sprintf("Forge proposal creation failed: %v", err), nil
	}

	domainsNote := "no external domains"
	if len(domains) > 0 {
		domainsNote = strings.Join(domains, ", ")
	}

	return fmt.Sprintf(`Forge proposal created.

Proposal ID: %s
Skill: %s (%s)
Actions: %s
Domains: %s
Risk level: %s

The proposal is pending your review. Open the Skills → Forge panel to inspect, install, and enable it. The skill will not be active until you approve it.`,
		id, name, skillID,
		strings.Join(actionNames, ", "),
		domainsNote, riskLevel), nil
}

// ── Gate functions ─────────────────────────────────────────────────────────────

// forgeValidateContract checks gates 1–6 against the APIResearchContract.
// Returns a refusal message on failure, or "" on pass.
func forgeValidateContract(c forgeContract) string {
	// Gate 1: docsQuality >= medium.
	switch c.DocsQuality {
	case "medium", "high":
	default:
		return fmt.Sprintf("Forge refused: docsQuality is '%s'. Research the API docs further — docsQuality must be 'medium' or 'high' before a proposal can be created.", c.DocsQuality)
	}

	// Gate 2: mappingConfidence must be high.
	if c.MappingConfidence != "high" {
		return fmt.Sprintf("Forge refused: mappingConfidence is '%s'. All field names, locations, and auth details must be confirmed from the official docs (mappingConfidence must be 'high').", c.MappingConfidence)
	}

	// Gate 3: endpoint must be defined.
	if strings.TrimSpace(c.Endpoint) == "" {
		return "Forge refused: endpoint is not defined in the contract. Identify the exact API endpoint path before proposing."
	}

	// Gate 4: valid HTTP method.
	switch strings.ToUpper(c.Method) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Sprintf("Forge refused: '%s' is not a valid HTTP method. Use GET, POST, PUT, PATCH, or DELETE.", c.Method)
	}

	// Gate 5: paramLocations defined for every required param.
	for _, param := range c.RequiredParams {
		if _, ok := c.ParamLocations[param]; !ok {
			return fmt.Sprintf("Forge refused: parameter location not defined for required param '%s'. Specify 'path', 'query', or 'body' for every required parameter.", param)
		}
	}

	// Gate 6: authType must be natively supported.
	switch c.AuthType {
	case "none", "apiKeyHeader", "apiKeyQuery", "bearerTokenStatic", "basicAuth", "oauth2ClientCredentials":
	case "oauth2AuthorizationCode":
		return "Forge refused: oauth2AuthorizationCode requires a browser login flow and is not supported. Use oauth2ClientCredentials for server-to-server OAuth2 flows."
	default:
		return fmt.Sprintf("Forge refused: auth type '%s' is not supported. Atlas Forge supports: none, apiKeyHeader, apiKeyQuery, bearerTokenStatic, basicAuth, oauth2ClientCredentials.", c.AuthType)
	}

	return ""
}

// forgeValidatePlansAuth checks Gate 7 — each plan's authType has all required
// companion fields for runtime injection.
func forgeValidatePlansAuth(plans []forgePlan) string {
	for _, plan := range plans {
		h := plan.HTTPRequest
		if h == nil {
			continue
		}
		switch h.AuthType {
		case "apiKeyHeader":
			if h.AuthSecretKey == "" || h.AuthHeaderName == "" {
				return fmt.Sprintf("Forge refused: plan '%s' uses apiKeyHeader auth but is missing authSecretKey or authHeaderName.", plan.ActionID)
			}
		case "apiKeyQuery":
			if h.AuthSecretKey == "" || h.AuthQueryParamName == "" {
				return fmt.Sprintf("Forge refused: plan '%s' uses apiKeyQuery auth but is missing authSecretKey or authQueryParamName.", plan.ActionID)
			}
		case "bearerTokenStatic", "basicAuth":
			if h.AuthSecretKey == "" {
				return fmt.Sprintf("Forge refused: plan '%s' uses %s auth but is missing authSecretKey.", plan.ActionID, h.AuthType)
			}
		case "oauth2ClientCredentials":
			if h.OAuth2TokenURL == "" || h.OAuth2ClientIDKey == "" || h.OAuth2ClientSecretKey == "" {
				return fmt.Sprintf("Forge refused: plan '%s' uses oauth2ClientCredentials but is missing oauth2TokenURL, oauth2ClientIDKey, or oauth2ClientSecretKey.", plan.ActionID)
			}
		}
	}
	return ""
}

// forgeValidateCredentials checks Gate 8 — all referenced Keychain secrets exist.
func forgeValidateCredentials(plans []forgePlan) string {
	checked := map[string]bool{}
	for _, plan := range plans {
		h := plan.HTTPRequest
		if h == nil {
			continue
		}
		for _, key := range []string{h.AuthSecretKey, h.OAuth2ClientIDKey, h.OAuth2ClientSecretKey, h.SecretHeader} {
			if key == "" || checked[key] {
				continue
			}
			checked[key] = true
			if !isValidKeychainServiceName(key) {
				return fmt.Sprintf("Forge refused: credential key '%s' contains invalid characters. Use only alphanumeric characters, dots, hyphens, and underscores.", key)
			}
			if !forgeKeychainExists(key) {
				return fmt.Sprintf("Forge refused: credential '%s' is not in the Keychain. Add it in Settings → Credentials before proposing this skill.", key)
			}
		}
	}
	return ""
}

// isValidKeychainServiceName returns true if s is safe to pass as the -s
// argument to the `security` CLI. Only alphanumeric, dot, hyphen, and
// underscore characters are allowed — this prevents shell metacharacter
// injection even though exec.Command does not invoke a shell.
func isValidKeychainServiceName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// forgeKeychainExists returns true if a Keychain generic-password item exists
// for the given service name. Callers must validate the service name with
// isValidKeychainServiceName before calling this function.
func forgeKeychainExists(service string) bool {
	return exec.Command("security", "find-generic-password", "-s", service, "-w").Run() == nil
}

// forgeReadKeychain reads a Keychain generic-password value by service name.
// Callers must validate the service name with isValidKeychainServiceName before
// calling this function.
func forgeReadKeychain(service string) string {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// forgeValidateAPI runs a live pre-validation against the primary GET plan via
// the validate.Gate pipeline.
func (r *Registry) forgeValidateAPI(ctx context.Context, contract forgeContract, plans []forgePlan) string {
	// Find the first GET-capable plan.
	var primary *forgeHTTPRequest
	for i := range plans {
		h := plans[i].HTTPRequest
		if h != nil && strings.ToUpper(h.Method) == "GET" {
			primary = h
			break
		}
	}
	if primary == nil {
		// No GET plan — skip live validation (write-only API).
		return ""
	}

	// Resolve credential value from Keychain.
	credValue := ""
	switch primary.AuthType {
	case "apiKeyHeader", "apiKeyQuery", "bearerTokenStatic", "basicAuth":
		if primary.AuthSecretKey != "" && isValidKeychainServiceName(primary.AuthSecretKey) {
			credValue = forgeReadKeychain(primary.AuthSecretKey)
		}
	}

	req := validate.ValidationRequest{
		ProviderName:           contract.ProviderName,
		BaseURL:                contract.BaseURL,
		Endpoint:               contract.Endpoint,
		Method:                 primary.Method,
		AuthType:               validate.AuthType(primary.AuthType),
		AuthHeaderName:         primary.AuthHeaderName,
		AuthQueryParam:         primary.AuthQueryParamName,
		CredentialValue:        credValue,
		RequiredParams:         contract.RequiredParams,
		ExpectedResponseFields: contract.ExpectedResponseFields,
	}

	gate := validate.Gate{SupportDir: r.supportDir}
	result := gate.Run(ctx, req)

	switch result.Recommendation {
	case validate.RecommendationReject:
		return fmt.Sprintf("API validation rejected this proposal.\n\n%s\n\nCheck the endpoint URL and authentication configuration, then try again.", result.Summary)
	case validate.RecommendationNeedsRevision:
		return fmt.Sprintf("API validation completed but the response needs attention.\n\n%s\n\nConfidence: %.0f%%\n\nReview the API configuration and try again.", result.Summary, result.Confidence*100)
	}
	// RecommendationUsable or RecommendationSkipped — proceed.
	return ""
}

// forgeValidateSpec checks the spec structure for all skill kinds.
func forgeValidateSpec(spec forgeSpec) string {
	var issues []string

	if strings.TrimSpace(spec.ID) == "" {
		issues = append(issues, "spec.id is required (lowercase-hyphenated, e.g. 'my-skill')")
	}
	if strings.TrimSpace(spec.Name) == "" {
		issues = append(issues, "spec.name is required")
	}
	if len(spec.Actions) == 0 {
		issues = append(issues, "spec.actions must contain at least one action")
	}
	for _, a := range spec.Actions {
		if strings.TrimSpace(a.ID) == "" {
			issues = append(issues, fmt.Sprintf("action '%s' is missing an id", a.Name))
		}
		if strings.TrimSpace(a.Name) == "" {
			issues = append(issues, fmt.Sprintf("action '%s' is missing a name", a.ID))
		}
	}

	validCategories := map[string]bool{
		"system": true, "utility": true, "creative": true, "communication": true,
		"automation": true, "research": true, "developer": true, "productivity": true,
	}
	if spec.Category != "" && !validCategories[spec.Category] {
		issues = append(issues, fmt.Sprintf("category '%s' is not valid — use: system, utility, creative, communication, automation, research, developer, productivity", spec.Category))
	}

	if len(issues) == 0 {
		return ""
	}
	bullets := make([]string, len(issues))
	for i, iss := range issues {
		bullets[i] = "• " + iss
	}
	return "Forge spec validation failed:\n" + strings.Join(bullets, "\n") + "\nFix these issues and call forge.orchestration.propose again."
}
