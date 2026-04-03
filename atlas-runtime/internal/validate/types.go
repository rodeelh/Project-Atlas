package validate

// Recommendation is the outcome of a validation run.
type Recommendation string

const (
	RecommendationUsable       Recommendation = "usable"
	RecommendationNeedsRevision Recommendation = "needsRevision"
	RecommendationReject       Recommendation = "reject"
	RecommendationSkipped      Recommendation = "skipped"
)

// FailureCategory classifies why a validation failed.
type FailureCategory string

const (
	FailureInvalidShape      FailureCategory = "invalidRequestShape"
	FailureUnsupportedAuth   FailureCategory = "unsupportedAuth"
	FailureMissingCredentials FailureCategory = "missingCredentials"
	FailureNetworkFailure    FailureCategory = "networkFailure"
	FailureHTTPError         FailureCategory = "httpError"
	FailureEmptyResponse     FailureCategory = "emptyResponse"
	FailureUnusableResponse  FailureCategory = "unusableResponse"
	FailureMissingFields     FailureCategory = "missingExpectedFields"
)

// AuthType describes how the API authenticates requests.
type AuthType string

const (
	AuthNone             AuthType = "none"
	AuthAPIKeyHeader     AuthType = "apiKeyHeader"
	AuthAPIKeyQuery      AuthType = "apiKeyQuery"
	AuthBearerTokenStatic AuthType = "bearerTokenStatic"
	AuthBasicAuth        AuthType = "basicAuth"
)

// ExampleInput is a map of parameter name → example value.
type ExampleInput map[string]string

// ValidationRequest carries everything the Gate needs to validate an API action.
type ValidationRequest struct {
	// ProviderName is a human-readable label for audit records.
	ProviderName string
	// BaseURL is the API root, e.g. "https://api.example.com".
	BaseURL string
	// Endpoint is the path, e.g. "/v1/weather".
	Endpoint string
	// Method is the HTTP verb. Only GET is executed in v1; others return Skipped.
	Method string
	// AuthType describes how credentials are injected.
	AuthType AuthType
	// AuthHeaderName is used when AuthType is AuthAPIKeyHeader (e.g. "X-Api-Key").
	AuthHeaderName string
	// AuthQueryParam is used when AuthType is AuthAPIKeyQuery (e.g. "apikey").
	AuthQueryParam string
	// CredentialValue is the actual API key/token (caller resolves from Keychain).
	CredentialValue string
	// ExampleInputs are caller-provided example parameter sets. May be empty.
	ExampleInputs []ExampleInput
	// RequiredParams are parameter names used to auto-generate examples.
	RequiredParams []string
	// ExpectedResponseFields are JSON field names that should appear in a good response.
	ExpectedResponseFields []string
}

// ValidationResult is returned by Gate.Run.
type ValidationResult struct {
	Recommendation  Recommendation  `json:"recommendation"`
	Confidence      float64         `json:"confidence"`
	AttemptsCount   int             `json:"attemptsCount"`
	FailureCategory *FailureCategory `json:"failureCategory,omitempty"`
	ResponsePreview string          `json:"responsePreview,omitempty"`
	Summary         string          `json:"summary"`
}

// AuditRecord is persisted to api-validation-history.json after each run.
type AuditRecord struct {
	ID                   string          `json:"id"`
	ProviderName         string          `json:"providerName"`
	Endpoint             string          `json:"endpoint"`
	ExampleUsed          ExampleInput    `json:"exampleUsed,omitempty"`
	Confidence           float64         `json:"confidence"`
	Recommendation       Recommendation  `json:"recommendation"`
	FailureCategory      *FailureCategory `json:"failureCategory,omitempty"`
	ResponsePreviewTrimmed string         `json:"responsePreviewTrimmed,omitempty"`
	Timestamp            string          `json:"timestamp"`
}
