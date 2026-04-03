// Package forge implements the Forge proposal lifecycle — AI-driven skill
// research, proposal persistence, install, and uninstall.
package forge

import "context"

// AIProvider is the interface forge.Service uses to make non-streaming AI
// calls. Using an interface instead of agent.ProviderConfig breaks the
// agent → skills → forge → agent import cycle; the domain layer provides a
// concrete adapter that wraps the real provider.
type AIProvider interface {
	CallNonStreaming(ctx context.Context, system, user string) (string, error)
}

// ForgeProposal matches contracts.ts ForgeProposalRecord exactly.
type ForgeProposal struct {
	ID              string   `json:"id"`
	SkillID         string   `json:"skillID"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Summary         string   `json:"summary"`
	Rationale       string   `json:"rationale,omitempty"`
	RequiredSecrets []string `json:"requiredSecrets"`
	Domains         []string `json:"domains"`
	ActionNames     []string `json:"actionNames"`
	RiskLevel       string   `json:"riskLevel"`
	Status          string   `json:"status"` // pending | installed | enabled | rejected | uninstalled
	SpecJSON        string   `json:"specJSON"`
	PlansJSON       string   `json:"plansJSON"`
	ContractJSON    string   `json:"contractJSON,omitempty"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

// ResearchingItem matches contracts.ts ForgeResearchingItem exactly.
type ResearchingItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	StartedAt string `json:"startedAt"`
}

// ProposeRequest is the body of POST /forge/proposals.
type ProposeRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	APIURL      string `json:"apiURL"`
}

// ForgeSkillSpec is the agent-authored specification for a new skill.
type ForgeSkillSpec struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Category    string           `json:"category"`
	RiskLevel   string           `json:"riskLevel"`
	Tags        []string         `json:"tags"`
	Actions     []ForgeActionSpec `json:"actions"`
}

// ForgeActionSpec describes one action within a ForgeSkillSpec.
type ForgeActionSpec struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permissionLevel"`
}

// ForgeActionPlan is the HTTP execution plan for one action in a Forge skill.
type ForgeActionPlan struct {
	ActionID    string           `json:"actionID"`
	Type        string           `json:"type"` // "http"
	HTTPRequest *HTTPRequestPlan `json:"httpRequest"`
}

// HTTPRequestPlan describes how to make an HTTP call for a ForgeActionPlan.
type HTTPRequestPlan struct {
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
	SecretHeader          string            `json:"secretHeader"` // legacy Bearer injection
}

// APIResearchContract captures what the agent learned from researching an API
// before proposing a Forge skill.
type APIResearchContract struct {
	ProviderName           string            `json:"providerName"`
	DocsURL                string            `json:"docsURL"`
	DocsQuality            string            `json:"docsQuality"` // low | medium | high
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
	MappingConfidence      string            `json:"mappingConfidence"` // low | medium | high
	ValidationStatus       string            `json:"validationStatus"`
	Notes                  string            `json:"notes"`
}
