package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"atlas-runtime-go/internal/creds"
)

func (r *Registry) registerWebSearch() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "websearch.query",
			Description: "Search the web using Brave Search and return the top results with titles, URLs, and descriptions.",
			Properties: map[string]ToolParam{
				"query": {Description: "The search query", Type: "string"},
				"count": {Description: "Number of results to return (default 5, max 20)", Type: "integer"},
			},
			Required: []string{"query"},
		},
		PermLevel: "read",
		Fn:        webSearchQuery,
	})
}

func webSearchQuery(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Count <= 0 {
		p.Count = 5
	}
	if p.Count > 20 {
		p.Count = 20
	}

	bundle, _ := creds.Read()
	if bundle.BraveSearchAPIKey == "" {
		return "", fmt.Errorf("Brave Search API key not configured — add it in Settings → Credentials")
	}

	results, err := braveSearch(ctx, bundle.BraveSearchAPIKey, p.Query, p.Count, "")
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No results found for: " + p.Query, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for \"%s\":\n\n", p.Query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
