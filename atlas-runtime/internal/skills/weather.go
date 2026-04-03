package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (r *Registry) registerWeather() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.current",
			Description: "Returns current temperature, wind, conditions, and feels-like for a location.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location (e.g. 'London', 'New York')", Type: "string"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        weatherCurrent,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.forecast",
			Description: "Returns daily max/min temperature, precipitation, and description for up to 7 days.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location", Type: "string"},
				"days":     {Description: "Number of forecast days (1-7, default 3)", Type: "integer"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        weatherForecast,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.hourly",
			Description: "Returns hourly temperature readings for a location.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location", Type: "string"},
				"hours":    {Description: "Number of hours ahead to return (default 12)", Type: "integer"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        weatherHourly,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.brief",
			Description: "Returns a one-line weather summary for a location.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location", Type: "string"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        weatherBrief,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.dayplan",
			Description: "Returns a weather-optimised daily plan split into morning, afternoon, and evening segments with outfit and activity recommendations.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location", Type: "string"},
			},
			Required: []string{"location"},
		},
		PermLevel: "read",
		Fn:        weatherDayplan,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "weather.activity_window",
			Description: "Finds the best and worst time windows for an outdoor activity based on hourly weather.",
			Properties: map[string]ToolParam{
				"location": {Description: "City name or location", Type: "string"},
				"activity": {
					Description: "Outdoor activity to plan for",
					Type:        "string",
					Enum:        []string{"walk", "run", "cycling", "golf", "beach", "photography", "theme_park", "hiking"},
				},
			},
			Required: []string{"location", "activity"},
		},
		PermLevel: "read",
		Fn:        weatherActivityWindow,
	})
}

// ── geocoding ────────────────────────────────────────────────────────────────

type geoResult struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Country   string  `json:"country"`
}

func geocode(ctx context.Context, location string) (geoResult, error) {
	u := "https://geocoding-api.open-meteo.com/v1/search?name=" +
		url.QueryEscape(location) + "&count=1&language=en&format=json"

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return geoResult{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return geoResult{}, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Results []geoResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return geoResult{}, fmt.Errorf("geocoding parse failed: %w", err)
	}
	if len(result.Results) == 0 {
		return geoResult{}, fmt.Errorf("location not found: %s", location)
	}
	return result.Results[0], nil
}

// ── WMO weather code descriptions ────────────────────────────────────────────

func wmoDescription(code int) string {
	switch {
	case code == 0:
		return "Clear sky"
	case code == 1:
		return "Mainly clear"
	case code == 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code == 45 || code == 48:
		return "Foggy"
	case code >= 51 && code <= 55:
		return "Drizzle"
	case code >= 61 && code <= 65:
		return "Rainy"
	case code >= 71 && code <= 75:
		return "Snowy"
	case code == 77:
		return "Snow grains"
	case code >= 80 && code <= 82:
		return "Rain showers"
	case code >= 85 && code <= 86:
		return "Snow showers"
	case code == 95:
		return "Thunderstorm"
	case code >= 96 && code <= 99:
		return "Thunderstorm with hail"
	default:
		return "Unknown conditions"
	}
}

// ── weather.current ───────────────────────────────────────────────────────────

func weatherCurrent(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&current=temperature_2m,apparent_temperature,wind_speed_10m,weathercode"+
			"&temperature_unit=celsius&wind_speed_unit=kmh",
		geo.Latitude, geo.Longitude,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Current struct {
			Temperature  float64 `json:"temperature_2m"`
			ApparentTemp float64 `json:"apparent_temperature"`
			WindSpeed    float64 `json:"wind_speed_10m"`
			WeatherCode  int     `json:"weathercode"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("weather parse failed: %w", err)
	}

	c := data.Current
	return fmt.Sprintf(
		"%s, %s: %s | Temp: %.1f°C (feels like %.1f°C) | Wind: %.1f km/h",
		geo.Name, geo.Country,
		wmoDescription(c.WeatherCode),
		c.Temperature, c.ApparentTemp, c.WindSpeed,
	), nil
}

// ── weather.forecast ──────────────────────────────────────────────────────────

func weatherForecast(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
		Days     int    `json:"days"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}
	days := p.Days
	if days <= 0 || days > 7 {
		days = 3
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_sum,weathercode"+
			"&temperature_unit=celsius&forecast_days=%d",
		geo.Latitude, geo.Longitude, days,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("forecast request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Daily struct {
			Time        []string  `json:"time"`
			TempMax     []float64 `json:"temperature_2m_max"`
			TempMin     []float64 `json:"temperature_2m_min"`
			Precip      []float64 `json:"precipitation_sum"`
			WeatherCode []int     `json:"weathercode"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("forecast parse failed: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s, %s — %d-day forecast:\n", geo.Name, geo.Country, days))
	for i, date := range data.Daily.Time {
		if i >= days {
			break
		}
		sb.WriteString(fmt.Sprintf("  %s: %s | High %.1f°C / Low %.1f°C | Precip %.1f mm\n",
			date,
			wmoDescription(data.Daily.WeatherCode[i]),
			data.Daily.TempMax[i],
			data.Daily.TempMin[i],
			data.Daily.Precip[i],
		))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── weather.hourly ────────────────────────────────────────────────────────────

func weatherHourly(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
		Hours    int    `json:"hours"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}
	hours := p.Hours
	if hours <= 0 {
		hours = 12
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&hourly=temperature_2m&temperature_unit=celsius&forecast_days=2",
		geo.Latitude, geo.Longitude,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hourly request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Hourly struct {
			Time  []string  `json:"time"`
			Temps []float64 `json:"temperature_2m"`
		} `json:"hourly"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("hourly parse failed: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s, %s — hourly temperatures:\n", geo.Name, geo.Country))
	count := int(math.Min(float64(hours), float64(len(data.Hourly.Time))))
	for i := 0; i < count; i++ {
		sb.WriteString(fmt.Sprintf("  %s: %.1f°C\n", data.Hourly.Time[i], data.Hourly.Temps[i]))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── weather.brief ─────────────────────────────────────────────────────────────

func weatherBrief(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&current=temperature_2m,weathercode&temperature_unit=celsius",
		geo.Latitude, geo.Longitude,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("brief request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weathercode"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("brief parse failed: %w", err)
	}

	return fmt.Sprintf("%s: %s, %.1f°C",
		geo.Name,
		wmoDescription(data.Current.WeatherCode),
		data.Current.Temperature,
	), nil
}

// ── weather.dayplan ───────────────────────────────────────────────────────────

type hourlySlice struct {
	time  string
	temp  float64
	precip float64 // precipitation probability %
	wind  float64
	code  int
}

func fetchHourlyRich(ctx context.Context, geo geoResult, hours int) ([]hourlySlice, error) {
	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&hourly=temperature_2m,precipitation_probability,weathercode,wind_speed_10m"+
			"&temperature_unit=celsius&wind_speed_unit=kmh&forecast_days=1",
		geo.Latitude, geo.Longitude,
	)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hourly rich request failed: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Hourly struct {
			Time   []string  `json:"time"`
			Temp   []float64 `json:"temperature_2m"`
			Precip []float64 `json:"precipitation_probability"`
			Code   []int     `json:"weathercode"`
			Wind   []float64 `json:"wind_speed_10m"`
		} `json:"hourly"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("hourly rich parse failed: %w", err)
	}

	n := len(data.Hourly.Time)
	if hours > 0 && hours < n {
		n = hours
	}
	out := make([]hourlySlice, n)
	for i := 0; i < n; i++ {
		precip := 0.0
		if i < len(data.Hourly.Precip) {
			precip = data.Hourly.Precip[i]
		}
		wind := 0.0
		if i < len(data.Hourly.Wind) {
			wind = data.Hourly.Wind[i]
		}
		code := 0
		if i < len(data.Hourly.Code) {
			code = data.Hourly.Code[i]
		}
		out[i] = hourlySlice{
			time:  data.Hourly.Time[i],
			temp:  data.Hourly.Temp[i],
			precip: precip,
			wind:  wind,
			code:  code,
		}
	}
	return out, nil
}

func umbrellaAdvice(maxPrecip float64) string {
	switch {
	case maxPrecip >= 60:
		return "bring an umbrella"
	case maxPrecip >= 30:
		return "consider an umbrella"
	default:
		return "no umbrella needed"
	}
}

func outfitAdvice(avgTemp float64) string {
	switch {
	case avgTemp >= 28:
		return "light summer clothing"
	case avgTemp >= 20:
		return "light layers"
	case avgTemp >= 12:
		return "jacket recommended"
	case avgTemp >= 5:
		return "warm coat"
	default:
		return "heavy winter clothing"
	}
}

func weatherDayplan(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" {
		return "", fmt.Errorf("location is required")
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	hours, err := fetchHourlyRich(ctx, geo, 24)
	if err != nil {
		return "", err
	}
	if len(hours) == 0 {
		return "No hourly data available.", nil
	}

	type segment struct {
		name  string
		hours []hourlySlice
	}
	segments := []segment{
		{name: "Morning (06:00–12:00)"},
		{name: "Afternoon (12:00–18:00)"},
		{name: "Evening (18:00–24:00)"},
	}
	for _, h := range hours {
		// Parse hour from time string like "2024-01-15T09:00"
		hr := 0
		if len(h.time) >= 13 {
			fmt.Sscanf(h.time[11:13], "%d", &hr)
		}
		switch {
		case hr >= 6 && hr < 12:
			segments[0].hours = append(segments[0].hours, h)
		case hr >= 12 && hr < 18:
			segments[1].hours = append(segments[1].hours, h)
		case hr >= 18:
			segments[2].hours = append(segments[2].hours, h)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Day plan for %s, %s:\n\n", geo.Name, geo.Country))
	for _, seg := range segments {
		if len(seg.hours) == 0 {
			continue
		}
		var temps, precips []float64
		var codes []int
		for _, h := range seg.hours {
			temps = append(temps, h.temp)
			precips = append(precips, h.precip)
			codes = append(codes, h.code)
		}
		avgTemp := avg(temps)
		maxPrecip := maxFloat(precips)
		// pick most severe weather code
		worstCode := codes[0]
		for _, c := range codes {
			if c > worstCode {
				worstCode = c
			}
		}
		sb.WriteString(fmt.Sprintf("**%s**\n", seg.name))
		sb.WriteString(fmt.Sprintf("  Conditions: %s\n", wmoDescription(worstCode)))
		sb.WriteString(fmt.Sprintf("  Avg temp: %.1f°C | Rain chance: %.0f%%\n", avgTemp, maxPrecip))
		sb.WriteString(fmt.Sprintf("  Outfit: %s | %s\n\n", outfitAdvice(avgTemp), umbrellaAdvice(maxPrecip)))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── weather.activity_window ───────────────────────────────────────────────────

// activityScore scores an hourly slot for a given activity (higher = better).
// Returns 0–100.
func activityScore(h hourlySlice, activity string) int {
	score := 100

	// Penalise rain
	if h.precip >= 70 {
		score -= 40
	} else if h.precip >= 40 {
		score -= 20
	} else if h.precip >= 20 {
		score -= 10
	}

	// Penalise bad weather codes
	if h.code >= 61 { // rain or worse
		score -= 30
	} else if h.code >= 51 { // drizzle
		score -= 15
	} else if h.code >= 3 { // overcast
		score -= 5
	}

	// Activity-specific temperature preferences
	switch activity {
	case "beach":
		if h.temp < 24 {
			score -= int((24 - h.temp) * 3)
		}
		if h.wind > 25 {
			score -= 20
		}
	case "run", "cycling", "hiking":
		if h.temp > 30 {
			score -= int((h.temp - 30) * 4)
		}
		if h.temp < 5 {
			score -= int((5 - h.temp) * 3)
		}
		if h.wind > 40 {
			score -= 15
		}
	case "walk", "theme_park":
		if h.temp > 35 {
			score -= 30
		} else if h.temp > 28 {
			score -= 10
		}
		if h.temp < 5 {
			score -= 20
		}
	case "golf":
		if h.temp < 10 || h.temp > 32 {
			score -= 20
		}
		if h.wind > 30 {
			score -= 25
		}
	case "photography":
		// Golden hours (06–08, 17–19) are best
		hr := 12
		if len(h.time) >= 13 {
			fmt.Sscanf(h.time[11:13], "%d", &hr)
		}
		if (hr >= 6 && hr <= 8) || (hr >= 17 && hr <= 19) {
			score += 20
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

func weatherActivityWindow(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Location string `json:"location"`
		Activity string `json:"activity"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Location == "" || p.Activity == "" {
		return "", fmt.Errorf("location and activity are required")
	}

	geo, err := geocode(ctx, p.Location)
	if err != nil {
		return "", err
	}

	hours, err := fetchHourlyRich(ctx, geo, 24)
	if err != nil {
		return "", err
	}
	if len(hours) == 0 {
		return "No hourly data available.", nil
	}

	type scored struct {
		h     hourlySlice
		score int
	}
	var slots []scored
	for _, h := range hours {
		slots = append(slots, scored{h: h, score: activityScore(h, p.Activity)})
	}

	best := slots[0]
	worst := slots[0]
	for _, s := range slots[1:] {
		if s.score > best.score {
			best = s
		}
		if s.score < worst.score {
			worst = s
		}
	}

	timeStr := func(t string) string {
		if len(t) >= 16 {
			return t[11:16]
		}
		return t
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s activity windows in %s, %s:\n\n", titleCase(p.Activity), geo.Name, geo.Country))
	sb.WriteString(fmt.Sprintf("Best window: %s\n", timeStr(best.h.time)))
	sb.WriteString(fmt.Sprintf("  %s | %.1f°C | Rain: %.0f%% | Wind: %.1f km/h | Score: %d/100\n\n",
		wmoDescription(best.h.code), best.h.temp, best.h.precip, best.h.wind, best.score))
	sb.WriteString(fmt.Sprintf("Worst window: %s\n", timeStr(worst.h.time)))
	sb.WriteString(fmt.Sprintf("  %s | %.1f°C | Rain: %.0f%% | Wind: %.1f km/h | Score: %d/100\n",
		wmoDescription(worst.h.code), worst.h.temp, worst.h.precip, worst.h.wind, worst.score))
	return sb.String(), nil
}

// ── math helpers ─────────────────────────────────────────────────────────────

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// titleCase capitalises the first letter of s without the deprecated strings.Title.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
