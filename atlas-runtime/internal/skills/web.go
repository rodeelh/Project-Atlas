package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"atlas-runtime-go/internal/creds"
)

func (r *Registry) registerWeb() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.search",
			Description: "Searches the web using Brave Search. Returns top results with titles and URLs.",
			Properties: map[string]ToolParam{
				"query": {Description: "Search query", Type: "string"},
				"count": {Description: "Number of results to return (default 5)", Type: "integer"},
			},
			Required: []string{"query"},
		},
		PermLevel: "read",
		Fn:        webSearch,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.fetch_page",
			Description: "Fetches a web page and returns the text content (first 3000 characters).",
			Properties: map[string]ToolParam{
				"url": {Description: "URL of the page to fetch", Type: "string"},
			},
			Required: []string{"url"},
		},
		PermLevel: "read",
		Fn:        webFetchPage,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.research",
			Description: "Searches the web and fetches the top N pages, returning a combined summary.",
			Properties: map[string]ToolParam{
				"query":   {Description: "Research query", Type: "string"},
				"sources": {Description: "Number of pages to fetch and summarize (default 3)", Type: "integer"},
			},
			Required: []string{"query"},
		},
		PermLevel: "read",
		Fn:        webResearch,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.news",
			Description: "Searches for recent news articles on a topic.",
			Properties: map[string]ToolParam{
				"query": {Description: "News topic to search for", Type: "string"},
			},
			Required: []string{"query"},
		},
		PermLevel: "read",
		Fn:        webNews,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.check_url",
			Description: "Checks whether a URL is reachable and returns the HTTP status code.",
			Properties: map[string]ToolParam{
				"url": {Description: "URL to check", Type: "string"},
			},
			Required: []string{"url"},
		},
		PermLevel: "read",
		Fn:        webCheckURL,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.multi_search",
			Description: "Runs 2–5 search queries in parallel and returns combined results.",
			Properties: map[string]ToolParam{
				"queries": {Description: "Comma-separated list of search queries (2–5)", Type: "string"},
				"count":   {Description: "Results per query (default 3)", Type: "integer"},
			},
			Required: []string{"queries"},
		},
		PermLevel: "read",
		Fn:        webMultiSearch,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.extract_links",
			Description: "Fetches a web page and extracts all outbound hyperlinks.",
			Properties: map[string]ToolParam{
				"url":   {Description: "URL to extract links from", Type: "string"},
				"limit": {Description: "Maximum number of links to return (default 20)", Type: "integer"},
			},
			Required: []string{"url"},
		},
		PermLevel: "read",
		Fn:        webExtractLinks,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "web.summarize_url",
			Description: "Fetches a web page and returns a structured summary with key points.",
			Properties: map[string]ToolParam{
				"url": {Description: "URL to summarize", Type: "string"},
			},
			Required: []string{"url"},
		},
		PermLevel: "read",
		Fn:        webSummarizeURL,
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// stripHTML removes HTML tags and condenses whitespace.
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
	// Remove non-printable chars.
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == '\n' {
			return r
		}
		return -1
	}, s)
	return strings.TrimSpace(s)
}

type braveResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// braveSearch calls the Brave Search API.
func braveSearch(ctx context.Context, apiKey, query string, count int, extraParams string) ([]braveResult, error) {
	if count <= 0 {
		count = 5
	}
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d%s",
		url.QueryEscape(query), count, extraParams)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("brave search error %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("brave search parse failed: %w", err)
	}

	results := make([]braveResult, 0, len(data.Web.Results))
	for _, r := range data.Web.Results {
		results = append(results, braveResult{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}
	return results, nil
}

// ── web.search ────────────────────────────────────────────────────────────────

func webSearch(ctx context.Context, args json.RawMessage) (string, error) {
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

	bundle, _ := creds.Read()
	if bundle.BraveSearchAPIKey == "" {
		return "Web search is unavailable: Brave Search API key not configured. Add your Brave API key in Atlas Settings → Skills → Web Research.", nil
	}

	results, err := braveSearch(ctx, bundle.BraveSearchAPIKey, p.Query, p.Count, "")
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No results found for: " + p.Query, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for \"%s\":\n", p.Query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Description))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── web.fetch_page ────────────────────────────────────────────────────────────

func webFetchPage(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Atlas/1.0 (compatible; Go HTTP client)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 200*1024)) // 200KB limit
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}

	text := stripHTML(string(bodyBytes))
	if len(text) > 3000 {
		text = text[:3000] + "... [truncated]"
	}
	return text, nil
}

// ── web.research ──────────────────────────────────────────────────────────────

func webResearch(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query   string `json:"query"`
		Sources int    `json:"sources"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Sources <= 0 {
		p.Sources = 3
	}

	bundle, _ := creds.Read()
	if bundle.BraveSearchAPIKey == "" {
		return "Web research is unavailable: Brave Search API key not configured. Add your Brave API key in Atlas Settings → Skills → Web Research.", nil
	}

	results, err := braveSearch(ctx, bundle.BraveSearchAPIKey, p.Query, p.Sources, "")
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No results found for: " + p.Query, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Research on \"%s\" (%d sources):\n\n", p.Query, len(results)))

	for i, r := range results {
		if i >= p.Sources {
			break
		}
		sb.WriteString(fmt.Sprintf("--- Source %d: %s ---\n%s\n\n", i+1, r.Title, r.URL))

		// Fetch page content.
		pageArgs, _ := json.Marshal(map[string]string{"url": r.URL})
		content, err := webFetchPage(ctx, pageArgs)
		if err != nil {
			sb.WriteString(fmt.Sprintf("[Could not fetch page: %v]\n\n", err))
			continue
		}
		// Trim per-source to keep total manageable.
		if len(content) > 1500 {
			content = content[:1500] + "... [truncated]"
		}
		sb.WriteString(content + "\n\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── web.news ──────────────────────────────────────────────────────────────────

func webNews(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	bundle, _ := creds.Read()
	if bundle.BraveSearchAPIKey == "" {
		return "News search is unavailable: Brave Search API key not configured. Add your Brave API key in Atlas Settings → Skills → Web Research.", nil
	}

	results, err := braveSearch(ctx, bundle.BraveSearchAPIKey, p.Query, 5, "&freshness=pd")
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No recent news found for: " + p.Query, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Recent news for \"%s\":\n", p.Query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Description))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── web.check_url ─────────────────────────────────────────────────────────────

func webCheckURL(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Atlas/1.0 (url-checker)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("%s — unreachable: %v", p.URL, err), nil
	}
	defer resp.Body.Close()

	return fmt.Sprintf("%s — HTTP %d %s", p.URL, resp.StatusCode, resp.Status), nil
}

// ── web.multi_search ──────────────────────────────────────────────────────────

func webMultiSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Queries string `json:"queries"`
		Count   int    `json:"count"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Queries == "" {
		return "", fmt.Errorf("queries is required")
	}
	if p.Count <= 0 {
		p.Count = 3
	}

	bundle, _ := creds.Read()
	if bundle.BraveSearchAPIKey == "" {
		return "Multi-search is unavailable: Brave Search API key not configured.", nil
	}

	parts := strings.Split(p.Queries, ",")
	if len(parts) > 5 {
		parts = parts[:5]
	}

	type result struct {
		query   string
		results []braveResult
		err     error
	}
	ch := make(chan result, len(parts))
	for _, q := range parts {
		q := strings.TrimSpace(q)
		go func() {
			r, err := braveSearch(ctx, bundle.BraveSearchAPIKey, q, p.Count, "")
			ch <- result{query: q, results: r, err: err}
		}()
	}

	var sb strings.Builder
	for range parts {
		res := <-ch
		sb.WriteString(fmt.Sprintf("=== %s ===\n", res.query))
		if res.err != nil {
			sb.WriteString(fmt.Sprintf("Error: %v\n\n", res.err))
			continue
		}
		for i, r := range res.results {
			sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── web.extract_links ─────────────────────────────────────────────────────────

var hrefRe = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)

func webExtractLinks(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL   string `json:"url"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	if p.Limit <= 0 {
		p.Limit = 20
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Atlas/1.0 (link-extractor)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
	matches := hrefRe.FindAllSubmatch(bodyBytes, -1)

	seen := map[string]bool{}
	var links []string
	base, _ := url.Parse(p.URL)

	for _, m := range matches {
		href := strings.TrimSpace(string(m[1]))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
			continue
		}
		// Resolve relative URLs
		if ref, err := url.Parse(href); err == nil && base != nil {
			href = base.ResolveReference(ref).String()
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		links = append(links, href)
		if len(links) >= p.Limit {
			break
		}
	}

	if len(links) == 0 {
		return "No links found on " + p.URL, nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Links from %s (%d):\n", p.URL, len(links)))
	for i, l := range links {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, l))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── web.summarize_url ─────────────────────────────────────────────────────────

func webSummarizeURL(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Atlas/1.0 (summarizer)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
	text := stripHTML(string(bodyBytes))

	// Extract title from <title> tag if present
	titleRe := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	title := ""
	if m := titleRe.Find(bodyBytes); m != nil {
		if sm := titleRe.FindSubmatch(bodyBytes); len(sm) > 1 {
			title = strings.TrimSpace(string(sm[1]))
		}
	}

	preview := text
	if len(preview) > 2000 {
		preview = preview[:2000] + "... [truncated]"
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString(fmt.Sprintf("**%s**\n", title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", p.URL))
	sb.WriteString(fmt.Sprintf("Status: HTTP %d\n\n", resp.StatusCode))
	sb.WriteString(preview)
	return sb.String(), nil
}
