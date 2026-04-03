package validate

import (
	"encoding/json"
	"strings"
)

// inspectResult is the output of the response inspector.
type inspectResult struct {
	Recommendation Recommendation
	Confidence     float64
	Preview        string
	Fields         []string
}

// inspectResponse scores an HTTP response deterministically — no LLM involved.
// Ports APIResponseInspector.swift exactly.
func inspectResponse(statusCode int, body []byte, expectedFields []string) inspectResult {
	preview := safePreview(body)

	// Rule 1: auth failure → hard reject.
	if statusCode == 401 || statusCode == 403 {
		return inspectResult{Recommendation: RecommendationReject, Confidence: 0.0, Preview: preview}
	}

	// Rule 2: other 4xx → needs revision.
	if statusCode >= 402 && statusCode < 500 {
		return inspectResult{Recommendation: RecommendationNeedsRevision, Confidence: 0.1, Preview: preview}
	}

	// Rule 3: 5xx → hard reject.
	if statusCode >= 500 {
		return inspectResult{Recommendation: RecommendationReject, Confidence: 0.0, Preview: preview}
	}

	// Rule 4: 2xx + empty body.
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) == 0 {
		return inspectResult{Recommendation: RecommendationNeedsRevision, Confidence: 0.1, Preview: ""}
	}

	// Parse JSON to check structure.
	var parsed interface{}
	isJSON := json.Unmarshal(body, &parsed) == nil

	if isJSON {
		// Rule 5: structurally empty JSON ([] or {}).
		if isEmpty(parsed) {
			return inspectResult{Recommendation: RecommendationNeedsRevision, Confidence: 0.1, Preview: preview}
		}

		// Rule 6: error-body false positive — object with ≤3 fields all error-like.
		if obj, ok := parsed.(map[string]interface{}); ok && len(obj) <= 3 && allErrorKeys(obj) {
			return inspectResult{Recommendation: RecommendationNeedsRevision, Confidence: 0.2, Preview: preview}
		}
	}

	// Scoring phase.
	var base float64
	var extractedFields []string

	if isJSON {
		switch v := parsed.(type) {
		case map[string]interface{}:
			base = 0.6
			for k := range v {
				extractedFields = append(extractedFields, k)
			}
		case []interface{}:
			base = 0.4
			if len(v) > 0 {
				if elem, ok := v[0].(map[string]interface{}); ok {
					for k := range elem {
						extractedFields = append(extractedFields, k)
					}
				}
			}
		default:
			base = 0.3
		}
	} else {
		base = 0.3
	}

	confidence := base
	recommendation := RecommendationUsable

	if len(expectedFields) > 0 {
		found := 0
		for _, ef := range expectedFields {
			for _, ex := range extractedFields {
				if strings.EqualFold(ef, ex) {
					found++
					break
				}
			}
		}
		matchRatio := float64(found) / float64(len(expectedFields))
		confidence = base + matchRatio*0.4
		if confidence > 1.0 {
			confidence = 1.0
		}
		if matchRatio < 0.5 {
			recommendation = RecommendationNeedsRevision
		}
	} else {
		if len(extractedFields) > 0 {
			confidence += 0.3
			if confidence > 1.0 {
				confidence = 1.0
			}
		} else {
			recommendation = RecommendationNeedsRevision
		}
	}

	return inspectResult{
		Recommendation: recommendation,
		Confidence:     confidence,
		Preview:        preview,
		Fields:         extractedFields,
	}
}

// isEmpty returns true for empty JSON objects or arrays.
func isEmpty(v interface{}) bool {
	switch t := v.(type) {
	case map[string]interface{}:
		return len(t) == 0
	case []interface{}:
		return len(t) == 0
	}
	return false
}

// allErrorKeys returns true if all keys in the object are error-indicator words.
var errorKeywords = []string{"error", "errors", "fault", "message", "detail", "details", "code", "status"}

func allErrorKeys(obj map[string]interface{}) bool {
	for k := range obj {
		found := false
		lower := strings.ToLower(k)
		for _, kw := range errorKeywords {
			if lower == kw {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// safePreview returns up to 500 chars of the body with credential-like lines stripped.
var secretKeywords = []string{"key", "token", "secret", "password", "auth", "bearer", "credential"}

func safePreview(body []byte) string {
	lines := strings.Split(string(body), "\n")
	var safe []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		hasSecret := false
		for _, kw := range secretKeywords {
			if strings.Contains(lower, kw) {
				hasSecret = true
				break
			}
		}
		// Also strip lines with long base64-like values (≥32 chars between quotes).
		if !hasSecret {
			safe = append(safe, line)
		}
	}
	result := strings.Join(safe, "\n")
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}
