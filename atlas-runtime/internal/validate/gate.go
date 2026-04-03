package validate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Gate runs the 3-phase API validation sequence and writes an audit record.
type Gate struct {
	// SupportDir is the Application Support path for writing audit records.
	SupportDir string
	// HTTPClient is used for live GET requests. Defaults to an 10s-timeout client.
	HTTPClient *http.Client
}

// Run executes the full validation sequence for the given request.
//
//  Phase 1 — Pre-flight (no network):
//    Step 1: Method check — only GET executed in v1; non-GET → skipped
//    Step 2: Shape check — baseURL and endpoint must be non-empty
//    Step 3: Auth type check — must be a supported auth type
//    Step 4: Credential readiness — key must be present when auth type requires it
//
//  Phase 2 — Candidate execution (max 2 attempts):
//    Attempt 1: best available example
//    Attempt 2: alternate example (only if attempt 1 returned needsRevision)
//    Hard failures (reject) abort immediately.
//
//  Phase 3 — Audit: record persisted regardless of outcome.
func (g *Gate) Run(ctx context.Context, req ValidationRequest) ValidationResult {
	// ── Phase 1: Pre-flight ───────────────────────────────────────────────────

	// Step 1: method check.
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = "GET"
	}
	if method != "GET" {
		result := ValidationResult{
			Recommendation: RecommendationSkipped,
			Confidence:     1.0,
			Summary:        fmt.Sprintf("Only GET requests are validated in v1 (%s skipped).", method),
		}
		g.audit(req, ExampleInput{}, result)
		return result
	}

	// Step 2: shape check.
	if strings.TrimSpace(req.BaseURL) == "" || strings.TrimSpace(req.Endpoint) == "" {
		cat := FailureInvalidShape
		result := ValidationResult{
			Recommendation:  RecommendationReject,
			Confidence:      0.0,
			FailureCategory: &cat,
			Summary:         "baseURL and endpoint must both be non-empty.",
		}
		g.audit(req, ExampleInput{}, result)
		return result
	}

	// Step 3: auth type check.
	switch req.AuthType {
	case AuthNone, AuthAPIKeyHeader, AuthAPIKeyQuery, AuthBearerTokenStatic, AuthBasicAuth:
		// supported
	default:
		cat := FailureUnsupportedAuth
		result := ValidationResult{
			Recommendation:  RecommendationReject,
			Confidence:      0.0,
			FailureCategory: &cat,
			Summary:         fmt.Sprintf("Unsupported auth type: %q.", req.AuthType),
		}
		g.audit(req, ExampleInput{}, result)
		return result
	}

	// Step 4: credential readiness.
	requiresKey := req.AuthType == AuthAPIKeyHeader ||
		req.AuthType == AuthAPIKeyQuery ||
		req.AuthType == AuthBearerTokenStatic ||
		req.AuthType == AuthBasicAuth
	if requiresKey && strings.TrimSpace(req.CredentialValue) == "" {
		cat := FailureMissingCredentials
		result := ValidationResult{
			Recommendation:  RecommendationReject,
			Confidence:      0.0,
			FailureCategory: &cat,
			Summary:         "API key/credential is required but not configured.",
		}
		g.audit(req, ExampleInput{}, result)
		return result
	}

	// ── Phase 2: Candidate execution ─────────────────────────────────────────

	client := g.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	firstExample, firstSource := Resolve(req)
	result, err := g.attempt(ctx, client, req, firstExample)
	attempts := 1

	if err != nil {
		cat := FailureNetworkFailure
		r := ValidationResult{
			Recommendation:  RecommendationReject,
			Confidence:      0.0,
			AttemptsCount:   attempts,
			FailureCategory: &cat,
			Summary:         "Network error: " + err.Error(),
		}
		g.audit(req, firstExample, r)
		return r
	}

	// Hard reject → no retry.
	if result.Recommendation == RecommendationReject {
		result.AttemptsCount = attempts
		g.audit(req, firstExample, result)
		return result
	}

	// needsRevision → try alternate example.
	if result.Recommendation == RecommendationNeedsRevision {
		altExample := ResolveAlternate(req, firstSource)
		altResult, altErr := g.attempt(ctx, client, req, altExample)
		attempts++
		if altErr == nil && altResult.Recommendation == RecommendationUsable {
			altResult.AttemptsCount = attempts
			g.audit(req, altExample, altResult)
			return altResult
		}
	}

	result.AttemptsCount = attempts
	g.audit(req, firstExample, result)
	return result
}

// attempt performs a single live GET and scores the response.
func (g *Gate) attempt(
	ctx context.Context,
	client *http.Client,
	req ValidationRequest,
	example ExampleInput,
) (ValidationResult, error) {

	rawURL := strings.TrimRight(req.BaseURL, "/") + "/" + strings.TrimLeft(req.Endpoint, "/")

	// Inject query-param auth and example inputs.
	u, err := url.Parse(rawURL)
	if err != nil {
		return ValidationResult{}, fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	for k, v := range example {
		q.Set(k, v)
	}
	if req.AuthType == AuthAPIKeyQuery && req.AuthQueryParam != "" {
		q.Set(req.AuthQueryParam, req.CredentialValue)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return ValidationResult{}, err
	}

	// Inject header-based auth.
	switch req.AuthType {
	case AuthAPIKeyHeader:
		name := req.AuthHeaderName
		if name == "" {
			name = "X-Api-Key"
		}
		httpReq.Header.Set(name, req.CredentialValue)
	case AuthBearerTokenStatic:
		httpReq.Header.Set("Authorization", "Bearer "+req.CredentialValue)
	case AuthBasicAuth:
		httpReq.SetBasicAuth(req.CredentialValue, "")
	}

	httpReq.Header.Set("User-Agent", "Atlas/1.0 validate")
	httpReq.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := client.Do(httpReq)
	if err != nil {
		return ValidationResult{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max

	scored := inspectResponse(resp.StatusCode, body, req.ExpectedResponseFields)

	var failCat *FailureCategory
	if scored.Recommendation == RecommendationReject {
		var cat FailureCategory
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			cat = FailureHTTPError
		} else if resp.StatusCode >= 500 {
			cat = FailureHTTPError
		} else {
			cat = FailureUnusableResponse
		}
		failCat = &cat
	} else if scored.Recommendation == RecommendationNeedsRevision {
		cat := FailureUnusableResponse
		failCat = &cat
	}

	return ValidationResult{
		Recommendation:  scored.Recommendation,
		Confidence:      scored.Confidence,
		FailureCategory: failCat,
		ResponsePreview: scored.Preview,
		Summary:         summaryFor(scored.Recommendation, resp.StatusCode, scored.Confidence),
	}, nil
}

func summaryFor(rec Recommendation, statusCode int, confidence float64) string {
	switch rec {
	case RecommendationUsable:
		return fmt.Sprintf("API responded successfully (HTTP %d, confidence %.0f%%).", statusCode, confidence*100)
	case RecommendationNeedsRevision:
		return fmt.Sprintf("API responded but output may be incomplete (HTTP %d, confidence %.0f%%).", statusCode, confidence*100)
	case RecommendationReject:
		return fmt.Sprintf("API validation failed (HTTP %d).", statusCode)
	default:
		return "Validation skipped."
	}
}

func (g *Gate) audit(req ValidationRequest, example ExampleInput, result ValidationResult) {
	if g.SupportDir == "" {
		return
	}
	preview := result.ResponsePreview
	if len(preview) > 200 {
		preview = preview[:200]
	}
	AppendAuditRecord(g.SupportDir, AuditRecord{
		ProviderName:           req.ProviderName,
		Endpoint:               req.Endpoint,
		ExampleUsed:            example,
		Confidence:             result.Confidence,
		Recommendation:         result.Recommendation,
		FailureCategory:        result.FailureCategory,
		ResponsePreviewTrimmed: preview,
	})
}
