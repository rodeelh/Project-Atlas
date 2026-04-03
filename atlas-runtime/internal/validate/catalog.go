package validate

import "strings"

// catalogEntry maps a keyword pattern to a known-good example input set.
type catalogEntry struct {
	keywords []string
	example  ExampleInput
	alternate ExampleInput
}

// builtInCatalog is the 12-entry catalog ported from ExampleInputCatalog.swift.
var builtInCatalog = []catalogEntry{
	{
		keywords:  []string{"open-meteo", "openmeteo", "weather", "meteo"},
		example:   ExampleInput{"latitude": "40.7128", "longitude": "-74.0060"},
		alternate: ExampleInput{"latitude": "51.5074", "longitude": "-0.1278"},
	},
	{
		keywords:  []string{"ipinfo", "ip-api", "ipapi", "ipgeolocation"},
		example:   ExampleInput{"ip": "8.8.8.8"},
		alternate: ExampleInput{"ip": "1.1.1.1"},
	},
	{
		keywords:  []string{"restcountries", "country", "countries"},
		example:   ExampleInput{"name": "France"},
		alternate: ExampleInput{"name": "Germany"},
	},
	{
		keywords:  []string{"github", "api.github"},
		example:   ExampleInput{"username": "octocat"},
		alternate: ExampleInput{"username": "torvalds"},
	},
	{
		keywords:  []string{"hacker-news", "hackernews", "hn.algolia"},
		example:   ExampleInput{"id": "1"},
		alternate: ExampleInput{"id": "42"},
	},
	{
		keywords:  []string{"jsonplaceholder", "json-placeholder"},
		example:   ExampleInput{"id": "1"},
		alternate: ExampleInput{"id": "2"},
	},
	{
		keywords:  []string{"openweathermap", "api.openweathermap"},
		example:   ExampleInput{"q": "London"},
		alternate: ExampleInput{"q": "Paris"},
	},
	{
		keywords:  []string{"coindesk", "coingecko", "crypto", "bitcoin"},
		example:   ExampleInput{"currency": "bitcoin"},
		alternate: ExampleInput{"currency": "ethereum"},
	},
	{
		keywords:  []string{"pokeapi", "poke-api", "pokemon"},
		example:   ExampleInput{"name": "pikachu"},
		alternate: ExampleInput{"name": "charizard"},
	},
	{
		keywords:  []string{"cocktaildb", "thecocktaildb"},
		example:   ExampleInput{"i": "11007"},
		alternate: ExampleInput{"i": "11008"},
	},
	{
		keywords:  []string{"omdbapi", "omdb"},
		example:   ExampleInput{"t": "Inception"},
		alternate: ExampleInput{"t": "Interstellar"},
	},
	{
		keywords:  []string{"newsapi", "news-api"},
		example:   ExampleInput{"q": "technology"},
		alternate: ExampleInput{"q": "science"},
	},
}

// defaultValues are used when auto-generating examples from requiredParams.
var defaultValues = map[string]string{
	"id":       "1",
	"query":    "test",
	"q":        "test",
	"location": "London",
	"city":     "London",
	"country":  "US",
	"user":     "user",
	"lat":      "40.7",
	"lon":      "-74.0",
	"lng":      "-74.0",
	"date":     "2024-01-01",
}

// alternateValues are used for the second attempt.
var alternateValues = map[string]string{
	"id":       "42",
	"query":    "hello",
	"q":        "hello",
	"location": "Paris",
	"city":     "Paris",
	"country":  "GB",
	"user":     "admin",
	"lat":      "51.5074",
	"lon":      "-0.1278",
	"lng":      "-0.1278",
	"date":     "2024-06-15",
}

// Resolve returns the best example input for the given request, plus a string
// indicating the source ("provided", "catalog", "generated").
func Resolve(req ValidationRequest) (ExampleInput, string) {
	// Tier 1: caller-provided.
	if len(req.ExampleInputs) > 0 {
		return req.ExampleInputs[0], "provided"
	}

	// Tier 2: catalog lookup by URL keyword.
	url := strings.ToLower(req.BaseURL + req.Endpoint)
	for _, entry := range builtInCatalog {
		for _, kw := range entry.keywords {
			if strings.Contains(url, kw) {
				return entry.example, "catalog"
			}
		}
	}

	// Tier 3: generate from requiredParams.
	return generateExample(req.RequiredParams, defaultValues), "generated"
}

// ResolveAlternate returns a second, distinct example for the retry attempt.
func ResolveAlternate(req ValidationRequest, firstSource string) ExampleInput {
	if firstSource == "provided" && len(req.ExampleInputs) > 1 {
		return req.ExampleInputs[1]
	}

	url := strings.ToLower(req.BaseURL + req.Endpoint)
	for _, entry := range builtInCatalog {
		for _, kw := range entry.keywords {
			if strings.Contains(url, kw) {
				return entry.alternate
			}
		}
	}

	return generateExample(req.RequiredParams, alternateValues)
}

func generateExample(params []string, vals map[string]string) ExampleInput {
	ex := ExampleInput{}
	for _, p := range params {
		key := strings.ToLower(p)
		if v, ok := vals[key]; ok {
			ex[p] = v
		} else {
			ex[p] = "test"
		}
	}
	return ex
}
