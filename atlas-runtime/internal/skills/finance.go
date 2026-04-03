package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"atlas-runtime-go/internal/creds"
)

func (r *Registry) registerFinance() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "finance.quote",
			Description: "Get the current price and basic info for a stock or crypto symbol.",
			Properties: map[string]ToolParam{
				"symbol": {Description: "Ticker symbol, e.g. AAPL or BTC-USD", Type: "string"},
			},
			Required: []string{"symbol"},
		},
		PermLevel: "read",
		Fn:        financeQuote,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "finance.history",
			Description: "Get historical daily closing prices for a symbol over the past N days.",
			Properties: map[string]ToolParam{
				"symbol": {Description: "Ticker symbol", Type: "string"},
				"days":   {Description: "Number of days of history (default 30, max 365)", Type: "integer"},
			},
			Required: []string{"symbol"},
		},
		PermLevel: "read",
		Fn:        financeHistory,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "finance.portfolio",
			Description: "Get current quotes for a list of symbols (batch lookup).",
			Properties: map[string]ToolParam{
				"symbols": {Description: "Comma-separated ticker symbols, e.g. AAPL,MSFT,GOOG", Type: "string"},
			},
			Required: []string{"symbols"},
		},
		PermLevel: "read",
		Fn:        financePortfolio,
	})
}

// financeQuote fetches a real-time quote. Uses Finnhub when a key is set,
// falls back to Yahoo Finance (no key required).
func financeQuote(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}
	bundle, _ := creds.Read()
	if bundle.FinnhubAPIKey != "" {
		return finnhubQuote(p.Symbol, bundle.FinnhubAPIKey)
	}
	return yahooQuote(p.Symbol)
}

func financeHistory(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Symbol string `json:"symbol"`
		Days   int    `json:"days"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if p.Days <= 0 {
		p.Days = 30
	}
	if p.Days > 365 {
		p.Days = 365
	}
	bundle, _ := creds.Read()
	if bundle.FinnhubAPIKey != "" {
		return finnhubHistory(p.Symbol, p.Days, bundle.FinnhubAPIKey)
	}
	return yahooHistory(p.Symbol, p.Days)
}

func financePortfolio(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Symbols string `json:"symbols"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Symbols == "" {
		return "", fmt.Errorf("symbols is required")
	}
	bundle, _ := creds.Read()
	parts := strings.Split(p.Symbols, ",")
	var results []string
	for _, sym := range parts {
		sym = strings.TrimSpace(sym)
		if sym == "" {
			continue
		}
		var q string
		var err error
		if bundle.FinnhubAPIKey != "" {
			q, err = finnhubQuote(sym, bundle.FinnhubAPIKey)
		} else {
			q, err = yahooQuote(sym)
		}
		if err != nil {
			results = append(results, sym+": error: "+err.Error())
		} else {
			results = append(results, q)
		}
	}
	return strings.Join(results, "\n\n"), nil
}

// ── Finnhub ───────────────────────────────────────────────────────────────────

func finnhubQuote(symbol, apiKey string) (string, error) {
	url := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s", symbol)
	body, err := finnhubGET(url, apiKey)
	if err != nil {
		return "", err
	}
	var v struct {
		C  float64 `json:"c"`  // current price
		D  float64 `json:"d"`  // change
		Dp float64 `json:"dp"` // percent change
		H  float64 `json:"h"`  // high
		L  float64 `json:"l"`  // low
		O  float64 `json:"o"`  // open
		Pc float64 `json:"pc"` // previous close
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.C == 0 {
		return "", fmt.Errorf("no data for %s", symbol)
	}
	sign := "+"
	if v.D < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s — %.2f | %s%.2f (%s%.2f%%) | H: %.2f  L: %.2f  O: %.2f  Prev: %.2f",
		symbol, v.C, sign, v.D, sign, v.Dp, v.H, v.L, v.O, v.Pc), nil
}

func finnhubHistory(symbol string, days int, apiKey string) (string, error) {
	end := time.Now().Unix()
	start := time.Now().AddDate(0, 0, -days).Unix()
	url := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/candle?symbol=%s&resolution=D&from=%d&to=%d",
		symbol, start, end,
	)
	body, err := finnhubGET(url, apiKey)
	if err != nil {
		return "", err
	}
	var v struct {
		C []float64 `json:"c"` // closes
		T []int64   `json:"t"` // timestamps
		S string    `json:"s"` // status
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.S != "ok" || len(v.T) == 0 {
		return "", fmt.Errorf("no history data for %s", symbol)
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%s — last %d trading days:", symbol, len(v.T)))
	for i, ts := range v.T {
		date := time.Unix(ts, 0).UTC().Format("2006-01-02")
		if i < len(v.C) {
			lines = append(lines, fmt.Sprintf("  %s: %.2f", date, v.C[i]))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// yahooQuote fetches a basic quote from Yahoo Finance v8 API (no API key required).
func yahooQuote(symbol string) (string, error) {
	url := "https://query1.finance.yahoo.com/v8/finance/chart/" + symbol + "?interval=1d&range=1d"
	body, err := financeGET(url)
	if err != nil {
		return "", err
	}
	return parseYahooQuote(symbol, body), nil
}

func yahooHistory(symbol string, days int) (string, error) {
	end := time.Now().Unix()
	start := time.Now().AddDate(0, 0, -days).Unix()
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&period1=%d&period2=%d",
		symbol, start, end,
	)
	body, err := financeGET(url)
	if err != nil {
		return "", err
	}
	return parseYahooHistory(symbol, body), nil
}

// finnhubGET sends the API key in the X-Finnhub-Token header, not the URL,
// so the key does not appear in server logs or browser history.
func finnhubGET(url, apiKey string) (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Atlas/1.0 finance")
	req.Header.Set("X-Finnhub-Token", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func financeGET(url string) (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Atlas/1.0 finance")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseYahooQuote(symbol, body string) string {
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) != nil {
		return body
	}
	chart, _ := v["chart"].(map[string]any)
	if chart == nil {
		return "No data for " + symbol
	}
	result, _ := chart["result"].([]any)
	if len(result) == 0 {
		return "No quote data for " + symbol
	}
	r, _ := result[0].(map[string]any)
	meta, _ := r["meta"].(map[string]any)
	price, _ := meta["regularMarketPrice"].(float64)
	prev, _ := meta["previousClose"].(float64)
	currency, _ := meta["currency"].(string)
	name, _ := meta["shortName"].(string)
	if name == "" {
		name = symbol
	}
	change := price - prev
	pct := 0.0
	if prev != 0 {
		pct = change / prev * 100
	}
	sign := "+"
	if change < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s (%s) — %.2f %s | %s%.2f (%s%.2f%%)",
		name, currency, price, currency, sign, change, sign, pct)
}

func parseYahooHistory(symbol, body string) string {
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) != nil {
		return body
	}
	chart, _ := v["chart"].(map[string]any)
	if chart == nil {
		return "No history for " + symbol
	}
	result, _ := chart["result"].([]any)
	if len(result) == 0 {
		return "No history data for " + symbol
	}
	r, _ := result[0].(map[string]any)
	timestamps, _ := r["timestamp"].([]any)
	indicators, _ := r["indicators"].(map[string]any)
	quotes, _ := indicators["quote"].([]any)
	if len(quotes) == 0 || len(timestamps) == 0 {
		return "No history data for " + symbol
	}
	q, _ := quotes[0].(map[string]any)
	closes, _ := q["close"].([]any)

	var lines []string
	lines = append(lines, fmt.Sprintf("%s — last %d trading days:", symbol, len(timestamps)))
	for i, ts := range timestamps {
		t := int64(ts.(float64))
		date := time.Unix(t, 0).UTC().Format("2006-01-02")
		if i < len(closes) && closes[i] != nil {
			c := closes[i].(float64)
			lines = append(lines, fmt.Sprintf("  %s: %.2f", date, c))
		}
	}
	return strings.Join(lines, "\n")
}
