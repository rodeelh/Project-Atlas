package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"
)

func (r *Registry) registerInfo() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "atlas.info",
			Description: "Returns information about the Atlas runtime status, version, and active configuration.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        atlasInfo,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "atlas.list_skills",
			Description: "Lists all registered skill actions available in this Atlas runtime.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        r.atlasListSkills,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "atlas.capabilities",
			Description: "Returns a concise summary of Atlas capabilities grouped by skill area.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        r.atlasCapabilities,
	})
}

func (r *Registry) registerInfoSkill() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "info.current_time",
			Description: "Returns the current time for a given timezone or location.",
			Properties: map[string]ToolParam{
				"timezone": {Description: "IANA timezone name, e.g. 'America/New_York' or 'Europe/London' (optional)", Type: "string"},
				"location": {Description: "City or country name — used to infer timezone if timezone not provided (optional)", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        infoCurrentTime,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "info.current_date",
			Description: "Returns the current date for a given timezone or location.",
			Properties: map[string]ToolParam{
				"timezone": {Description: "IANA timezone name (optional)", Type: "string"},
				"location": {Description: "City or country name (optional)", Type: "string"},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        infoCurrentDate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "info.timezone_convert",
			Description: "Converts a time from one timezone to another.",
			Properties: map[string]ToolParam{
				"time":          {Description: "Time to convert, e.g. '14:30' or '2024-01-15 14:30'", Type: "string"},
				"from_timezone": {Description: "Source IANA timezone, e.g. 'America/New_York'", Type: "string"},
				"to_timezone":   {Description: "Target IANA timezone, e.g. 'Asia/Tokyo'", Type: "string"},
			},
			Required: []string{"time", "from_timezone", "to_timezone"},
		},
		PermLevel: "read",
		Fn:        infoTimezoneConvert,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "info.currency_for_location",
			Description: "Returns the currency used in a given country or city.",
			Properties: map[string]ToolParam{
				"location": {Description: "Country or city name", Type: "string"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        infoCurrencyForLocation,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "info.currency_convert",
			Description: "Converts an amount from one currency to another using live exchange rates.",
			Properties: map[string]ToolParam{
				"amount": {Description: "Amount to convert", Type: "number"},
				"from":   {Description: "Source currency ISO code, e.g. 'USD'", Type: "string"},
				"to":     {Description: "Target currency ISO code, e.g. 'EUR'", Type: "string"},
			},
			Required: []string{"amount", "from", "to"},
		},
		PermLevel: "read",
		Fn:        infoCurrencyConvert,
	})
}

// ── atlas.info ────────────────────────────────────────────────────────────────

func atlasInfo(_ context.Context, _ json.RawMessage) (string, error) {
	return fmt.Sprintf(
		"Atlas Go Runtime — status: running | Go: %s | OS: %s/%s",
		runtime.Version(), runtime.GOOS, runtime.GOARCH,
	), nil
}

func (r *Registry) atlasListSkills(_ context.Context, _ json.RawMessage) (string, error) {
	var names []string
	for id := range r.entries {
		names = append(names, id)
	}
	sort.Strings(names)
	return fmt.Sprintf("Registered actions (%d):\n%s", len(names), strings.Join(names, "\n")), nil
}

func (r *Registry) atlasCapabilities(_ context.Context, _ json.RawMessage) (string, error) {
	groups := map[string][]string{}
	for id := range r.entries {
		parts := strings.SplitN(id, ".", 2)
		ns := parts[0]
		groups[ns] = append(groups[ns], id)
	}
	var nsList []string
	for ns := range groups {
		nsList = append(nsList, ns)
	}
	sort.Strings(nsList)

	var sb strings.Builder
	sb.WriteString("Atlas capability summary:\n\n")
	for _, ns := range nsList {
		actions := groups[ns]
		sort.Strings(actions)
		sb.WriteString(fmt.Sprintf("**%s** (%d actions)\n", ns, len(actions)))
		for _, a := range actions {
			sb.WriteString("  • " + a + "\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── info.current_time ─────────────────────────────────────────────────────────

func resolveTimezone(timezone, location string) (*time.Location, string, error) {
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return nil, "", fmt.Errorf("unknown timezone %q: %w", timezone, err)
		}
		return loc, timezone, nil
	}
	if location != "" {
		tz := locationToTimezone(location)
		if tz != "" {
			loc, err := time.LoadLocation(tz)
			if err == nil {
				return loc, tz, nil
			}
		}
	}
	return time.Local, "local", nil
}

func infoCurrentTime(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Timezone string `json:"timezone"`
		Location string `json:"location"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck

	loc, tzName, err := resolveTimezone(p.Timezone, p.Location)
	if err != nil {
		return "", err
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("Current time in %s: %s", tzName, now.Format("15:04:05 MST (Mon 2 Jan 2006)")), nil
}

// ── info.current_date ─────────────────────────────────────────────────────────

func infoCurrentDate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Timezone string `json:"timezone"`
		Location string `json:"location"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck

	loc, tzName, err := resolveTimezone(p.Timezone, p.Location)
	if err != nil {
		return "", err
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("Current date in %s: %s", tzName, now.Format("Monday, 2 January 2006")), nil
}

// ── info.timezone_convert ─────────────────────────────────────────────────────

func infoTimezoneConvert(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Time         string `json:"time"`
		FromTimezone string `json:"from_timezone"`
		ToTimezone   string `json:"to_timezone"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Time == "" || p.FromTimezone == "" || p.ToTimezone == "" {
		return "", fmt.Errorf("time, from_timezone, and to_timezone are required")
	}

	fromLoc, err := time.LoadLocation(p.FromTimezone)
	if err != nil {
		return "", fmt.Errorf("unknown from_timezone %q: %w", p.FromTimezone, err)
	}
	toLoc, err := time.LoadLocation(p.ToTimezone)
	if err != nil {
		return "", fmt.Errorf("unknown to_timezone %q: %w", p.ToTimezone, err)
	}

	// Try parsing as full datetime first, then just time
	var t time.Time
	layouts := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"15:04",
		"15:04:05",
	}
	for _, layout := range layouts {
		if tt, err := time.ParseInLocation(layout, p.Time, fromLoc); err == nil {
			t = tt
			break
		}
	}
	if t.IsZero() {
		return "", fmt.Errorf("could not parse time %q — use formats like '14:30' or '2024-01-15 14:30'", p.Time)
	}

	converted := t.In(toLoc)
	return fmt.Sprintf("%s %s = %s %s",
		t.Format("15:04 MST"), p.FromTimezone,
		converted.Format("15:04 MST"), p.ToTimezone,
	), nil
}

// ── info.currency_for_location ────────────────────────────────────────────────

func infoCurrencyForLocation(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}

	code := locationToCurrency(strings.ToLower(p.Location))
	if code == "" {
		return fmt.Sprintf("Unknown currency for location: %s", p.Location), nil
	}
	return fmt.Sprintf("Currency in %s: %s", p.Location, code), nil
}

// ── info.currency_convert ─────────────────────────────────────────────────────

func infoCurrencyConvert(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Amount float64 `json:"amount"`
		From   string  `json:"from"`
		To     string  `json:"to"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.From == "" || p.To == "" {
		return "", fmt.Errorf("amount, from, and to are required")
	}

	from := strings.ToUpper(p.From)
	to := strings.ToUpper(p.To)

	if from == to {
		return fmt.Sprintf("%.2f %s = %.2f %s", p.Amount, from, p.Amount, to), nil
	}

	rate, err := fetchExchangeRate(ctx, from, to)
	if err != nil {
		return "", err
	}
	converted := p.Amount * rate
	return fmt.Sprintf("%.2f %s = %.2f %s (rate: %.6f)", p.Amount, from, converted, to, rate), nil
}

func fetchExchangeRate(ctx context.Context, from, to string) (float64, error) {
	u := fmt.Sprintf("https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json",
		strings.ToLower(from))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Atlas/1.0 currency")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("exchange rate fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("exchange rate parse failed: %w", err)
	}

	// API format: { "date": "...", "<from>": { "<to>": <rate> } }
	inner, ok := data[strings.ToLower(from)].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("no rates found for %s", from)
	}
	rate, ok := inner[strings.ToLower(to)].(float64)
	if !ok {
		return 0, fmt.Errorf("no rate found for %s→%s", from, to)
	}
	return rate, nil
}

// ── location helpers ──────────────────────────────────────────────────────────

// locationToTimezone maps common city/country names to IANA timezone IDs.
// Lowercase keys.
func locationToTimezone(loc string) string {
	loc = strings.ToLower(strings.TrimSpace(loc))
	m := map[string]string{
		// North America
		"new york": "America/New_York", "nyc": "America/New_York",
		"los angeles": "America/Los_Angeles", "la": "America/Los_Angeles",
		"chicago": "America/Chicago", "houston": "America/Chicago",
		"toronto": "America/Toronto", "montreal": "America/Toronto",
		"vancouver": "America/Vancouver",
		"denver": "America/Denver",
		"phoenix": "America/Phoenix",
		"miami": "America/New_York",
		"san francisco": "America/Los_Angeles", "sf": "America/Los_Angeles",
		"seattle": "America/Los_Angeles",
		"mexico city": "America/Mexico_City",
		// Europe
		"london": "Europe/London", "uk": "Europe/London", "england": "Europe/London",
		"paris": "Europe/Paris", "france": "Europe/Paris",
		"berlin": "Europe/Berlin", "germany": "Europe/Berlin",
		"rome": "Europe/Rome", "italy": "Europe/Rome",
		"madrid": "Europe/Madrid", "spain": "Europe/Madrid",
		"amsterdam": "Europe/Amsterdam",
		"zurich": "Europe/Zurich", "switzerland": "Europe/Zurich",
		"stockholm": "Europe/Stockholm", "sweden": "Europe/Stockholm",
		"oslo": "Europe/Oslo", "norway": "Europe/Oslo",
		"moscow": "Europe/Moscow", "russia": "Europe/Moscow",
		"istanbul": "Europe/Istanbul", "turkey": "Europe/Istanbul",
		"athens": "Europe/Athens", "greece": "Europe/Athens",
		"warsaw": "Europe/Warsaw", "poland": "Europe/Warsaw",
		"dubai": "Asia/Dubai", "uae": "Asia/Dubai",
		// Asia
		"tokyo": "Asia/Tokyo", "japan": "Asia/Tokyo",
		"beijing": "Asia/Shanghai", "shanghai": "Asia/Shanghai", "china": "Asia/Shanghai",
		"hong kong": "Asia/Hong_Kong",
		"singapore": "Asia/Singapore",
		"seoul": "Asia/Seoul", "korea": "Asia/Seoul",
		"mumbai": "Asia/Kolkata", "delhi": "Asia/Kolkata", "india": "Asia/Kolkata",
		"bangkok": "Asia/Bangkok", "thailand": "Asia/Bangkok",
		"jakarta": "Asia/Jakarta", "indonesia": "Asia/Jakarta",
		"karachi": "Asia/Karachi", "pakistan": "Asia/Karachi",
		"riyadh": "Asia/Riyadh", "saudi arabia": "Asia/Riyadh",
		"tel aviv": "Asia/Jerusalem", "israel": "Asia/Jerusalem",
		// Oceania
		"sydney": "Australia/Sydney", "melbourne": "Australia/Melbourne",
		"australia": "Australia/Sydney",
		"auckland": "Pacific/Auckland", "new zealand": "Pacific/Auckland",
		// Africa
		"cairo": "Africa/Cairo", "egypt": "Africa/Cairo",
		"johannesburg": "Africa/Johannesburg", "south africa": "Africa/Johannesburg",
		"nairobi": "Africa/Nairobi", "kenya": "Africa/Nairobi",
		"lagos": "Africa/Lagos", "nigeria": "Africa/Lagos",
		// South America
		"sao paulo": "America/Sao_Paulo", "brazil": "America/Sao_Paulo",
		"buenos aires": "America/Argentina/Buenos_Aires", "argentina": "America/Argentina/Buenos_Aires",
		"bogota": "America/Bogota", "colombia": "America/Bogota",
		"lima": "America/Lima", "peru": "America/Lima",
		"santiago": "America/Santiago", "chile": "America/Santiago",
	}
	return m[loc]
}

// locationToCurrency maps country/city names to ISO 4217 currency codes.
func locationToCurrency(loc string) string {
	m := map[string]string{
		"usa": "USD", "united states": "USD", "us": "USD",
		"new york": "USD", "los angeles": "USD", "chicago": "USD",
		"san francisco": "USD", "seattle": "USD", "miami": "USD",
		"canada": "CAD", "toronto": "CAD", "vancouver": "CAD",
		"uk": "GBP", "united kingdom": "GBP", "england": "GBP",
		"london": "GBP", "britain": "GBP",
		"eurozone": "EUR", "europe": "EUR",
		"germany": "EUR", "berlin": "EUR",
		"france": "EUR", "paris": "EUR",
		"italy": "EUR", "rome": "EUR",
		"spain": "EUR", "madrid": "EUR",
		"netherlands": "EUR", "amsterdam": "EUR",
		"portugal": "EUR",
		"switzerland": "CHF", "zurich": "CHF",
		"sweden": "SEK", "stockholm": "SEK",
		"norway": "NOK", "oslo": "NOK",
		"denmark": "DKK",
		"russia": "RUB", "moscow": "RUB",
		"japan": "JPY", "tokyo": "JPY",
		"china": "CNY", "beijing": "CNY", "shanghai": "CNY",
		"hong kong": "HKD",
		"singapore": "SGD",
		"south korea": "KRW", "korea": "KRW", "seoul": "KRW",
		"india": "INR", "mumbai": "INR", "delhi": "INR",
		"australia": "AUD", "sydney": "AUD", "melbourne": "AUD",
		"new zealand": "NZD", "auckland": "NZD",
		"brazil": "BRL", "sao paulo": "BRL",
		"mexico": "MXN", "mexico city": "MXN",
		"south africa": "ZAR", "johannesburg": "ZAR",
		"uae": "AED", "dubai": "AED",
		"saudi arabia": "SAR", "riyadh": "SAR",
		"turkey": "TRY", "istanbul": "TRY",
		"egypt": "EGP", "cairo": "EGP",
		"argentina": "ARS", "buenos aires": "ARS",
		"thailand": "THB", "bangkok": "THB",
		"indonesia": "IDR", "jakarta": "IDR",
		"malaysia": "MYR", "kuala lumpur": "MYR",
		"philippines": "PHP", "manila": "PHP",
		"israel": "ILS", "tel aviv": "ILS",
		"pakistan": "PKR", "karachi": "PKR",
		"nigeria": "NGN", "lagos": "NGN",
		"kenya": "KES", "nairobi": "KES",
		"poland": "PLN", "warsaw": "PLN",
		"czech republic": "CZK", "prague": "CZK",
		"hungary": "HUF", "budapest": "HUF",
		"colombia": "COP", "bogota": "COP",
		"chile": "CLP", "santiago": "CLP",
		"peru": "PEN", "lima": "PEN",
	}
	return m[loc]
}
