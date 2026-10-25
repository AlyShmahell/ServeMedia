package metadata

import (
	"strconv"
	"strings"
)

// NFOFacts is the show and movie metadata written beside the title and plot.
type NFOFacts struct {
	Genres    []string
	Rating    float64
	Runtime   int
	Language  string
	Premiered string
	Ended     string
	Countries []string
	Networks  []string
}

// FactsFromAttrs parses provider attributes into NFO fields.
func FactsFromAttrs(attrs map[string]string) NFOFacts {
	if len(attrs) == 0 {
		return NFOFacts{}
	}
	return NFOFacts{
		Genres:    splitList(attrs["genres"]),
		Rating:    ParseRating(attrs["rating"]),
		Runtime:   ParseRuntime(attrs["runtime"]),
		Language:  strings.TrimSpace(attrs["language"]),
		Premiered: strings.TrimSpace(attrs["premiered"]),
		Ended:     strings.TrimSpace(attrs["ended"]),
		Countries: splitList(attrs["country"]),
		Networks:  splitList(attrs["network"]),
	}
}

func (n *MovieNFO) ApplyFacts(f NFOFacts) {
	if n == nil {
		return
	}
	applyFacts(&n.Genre, &n.Rating, &n.Runtime, &n.Language, &n.Premiered, &n.Ended, &n.Country, &n.Network, f)
}

func (n *TVShowNFO) ApplyFacts(f NFOFacts) {
	if n == nil {
		return
	}
	applyFacts(&n.Genre, &n.Rating, &n.Runtime, &n.Language, &n.Premiered, &n.Ended, &n.Country, &n.Network, f)
}

func applyFacts(genre *[]string, rating *float64, runtime *int, language, premiered, ended *string, country, network *[]string, f NFOFacts) {
	if len(f.Genres) > 0 {
		*genre = f.Genres
	}
	if f.Rating > 0 {
		*rating = f.Rating
	}
	if f.Runtime > 0 {
		*runtime = f.Runtime
	}
	if f.Language != "" {
		*language = f.Language
	}
	if f.Premiered != "" {
		*premiered = f.Premiered
	}
	if f.Ended != "" {
		*ended = f.Ended
	}
	if len(f.Countries) > 0 {
		*country = f.Countries
	}
	if len(f.Networks) > 0 {
		*network = f.Networks
	}
}

func (n MovieNFO) EndedOn() string {
	return firstText(n.Ended, n.EndDate)
}

func (n TVShowNFO) EndedOn() string {
	return firstText(n.Ended, n.EndDate)
}

func (n MovieNFO) Networks() []string {
	if nets := cleanList(n.Network); len(nets) > 0 {
		return nets
	}
	return cleanList(n.Studio)
}

func (n TVShowNFO) Networks() []string {
	if nets := cleanList(n.Network); len(nets) > 0 {
		return nets
	}
	return cleanList(n.Studio)
}

func firstText(a, b string) string {
	if s := strings.TrimSpace(a); s != "" {
		return s
	}
	return strings.TrimSpace(b)
}

func cleanList(in []string) []string {
	return splitList(strings.Join(in, ","))
}

func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// ParseRating reads values such as "8.4", "8.4/10", and "imdb 8.1".
func ParseRating(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// ParseRuntime reads a minute count from "42", "106 min", or "1h 46m".
func ParseRuntime(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "n/a" {
		return 0
	}
	if i := strings.Index(s, "h"); i >= 0 {
		hours := leadingInt(s[:i])
		mins := leadingInt(s[i+1:])
		if hours > 0 {
			return hours*60 + mins
		}
	}
	return leadingInt(s)
}

func leadingInt(s string) int {
	n := 0
	seen := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			seen = true
			continue
		}
		if seen {
			break
		}
	}
	if !seen || n <= 0 {
		return 0
	}
	return n
}

// FormatRuntime renders minutes as "42 min" or "1h 46m".
func FormatRuntime(min int) string {
	if min <= 0 {
		return ""
	}
	if min < 60 {
		return strconv.Itoa(min) + " min"
	}
	h := min / 60
	m := min % 60
	if m == 0 {
		return strconv.Itoa(h) + "h"
	}
	return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
}
