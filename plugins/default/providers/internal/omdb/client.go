package omdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/alyshmahell/medora-plugin-providers/internal/ratelimit"
	"github.com/alyshmahell/medora-plugin-sdk/rpcapi"
)

const apiBase = "https://www.omdbapi.com/"

type Client struct {
	APIKey  string
	// BaseURL overrides the OMDb endpoint (tests / mirrors). Trailing slash optional.
	BaseURL string
	HTTP    *http.Client
	Limiter *ratelimit.Limiter
}

func (c *Client) endpoint() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		u := strings.TrimSpace(c.BaseURL)
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	return apiBase
}

func (c *Client) Enabled() bool {
	return c != nil && strings.TrimSpace(c.APIKey) != ""
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) get(q url.Values) ([]byte, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("OMDb API key not configured (set OMDB_API_KEY)")
	}
	if c.Limiter != nil {
		if err := c.Limiter.Acquire(); err != nil {
			return nil, err
		}
	}
	q.Set("apikey", c.APIKey)
	u := c.endpoint() + "?" + q.Encode()
	res, err := c.httpClient().Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("OMDb %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

type response struct {
	Title      string `json:"Title"`
	Year       string `json:"Year"`
	Plot       string `json:"Plot"`
	Runtime    string `json:"Runtime"`
	ImdbRating string `json:"imdbRating"`
	Poster     string `json:"Poster"`
	ImdbID     string `json:"imdbID"`
	Type       string `json:"Type"`
	Genre      string `json:"Genre"`
	Response   string `json:"Response"`
	Error      string `json:"Error"`
	Episodes   []struct {
		Title   string `json:"Title"`
		Episode string `json:"Episode"`
		ImdbID  string `json:"imdbID"`
	} `json:"Episodes"`
}

type searchResponse struct {
	Search       []searchHit `json:"Search"`
	TotalResults string      `json:"totalResults"`
	Response     string      `json:"Response"`
	Error        string      `json:"Error"`
}

type searchHit struct {
	Title  string `json:"Title"`
	Year   string `json:"Year"`
	ImdbID string `json:"imdbID"`
	Type   string `json:"Type"`
	Poster string `json:"Poster"`
}

func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		y, _ := strconv.Atoi(s[:4])
		return y
	}
	return 0
}

func parseRuntime(s string) int {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "min"))
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func parseRating(s string) float64 {
	if s == "" || s == "N/A" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func posterURL(s string) string {
	if s == "" || s == "N/A" {
		return ""
	}
	return s
}

func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func stripArticles(s string) string {
	s = strings.TrimSpace(s)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, a) {
			return strings.TrimSpace(strings.TrimPrefix(s, a))
		}
	}
	return s
}

func titleKey(s string) string {
	return stripArticles(normalizeTitle(s))
}

var titleStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "for": true, "in": true, "no": true,
	"of": true, "on": true, "or": true, "the": true, "to": true, "with": true,
}

func significantTokens(s string) []string {
	parts := strings.Fields(titleKey(s))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) <= 1 || titleStopwords[p] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// enoughTokensPresent is true when a strict majority of significant query tokens
// appear in have, or the longest query token (len≥5) appears (romanized vs English).
func enoughTokensPresent(want, have string) bool {
	tokens := significantTokens(want)
	if len(tokens) == 0 {
		return true
	}
	hn := titleKey(have)
	longest := ""
	matched := 0
	for _, t := range tokens {
		if strings.Contains(hn, t) {
			matched++
		}
		if len(t) > len(longest) {
			longest = t
		}
	}
	if len(longest) >= 5 && strings.Contains(hn, longest) {
		return true
	}
	return matched*2 > len(tokens)
}

// pickBestSearchHit chooses the closest title match, preferring year when set.
// Anime libraries only accept exact titleKey matches (no fuzzy long-title hits).
// Among anime exacts: prefer year match when year > 0, else the first hit in
// OMDb search order (relevance) — not oldest year.
// For anime, also accept a hit whose titleKey matches the query after stripping
// a trailing "The Movie" suffix.
func pickBestSearchHit(want string, year int, hits []searchHit, libraryType string) (searchHit, bool) {
	wantKey := titleKey(want)
	if wantKey == "" || len(hits) == 0 {
		return searchHit{}, false
	}
	anime := strings.EqualFold(strings.TrimSpace(libraryType), "anime")
	wantKeys := []string{wantKey}
	if anime {
		if stripped := stripMovieTitleSuffix(want); stripped != want {
			if sk := titleKey(stripped); sk != "" && sk != wantKey {
				wantKeys = append(wantKeys, sk)
			}
		}
	}
	var exact []searchHit
	seenID := map[string]bool{}
	for _, h := range hits {
		hk := titleKey(h.Title)
		for _, wk := range wantKeys {
			if hk != wk {
				continue
			}
			if h.ImdbID != "" && seenID[h.ImdbID] {
				break
			}
			if h.ImdbID != "" {
				seenID[h.ImdbID] = true
			}
			exact = append(exact, h)
			break
		}
	}
	if anime {
		if len(exact) == 0 {
			return searchHit{}, false
		}
		if year > 0 {
			var yearHits []searchHit
			for _, h := range exact {
				if parseYear(h.Year) == year {
					yearHits = append(yearHits, h)
				}
			}
			if len(yearHits) == 1 {
				return yearHits[0], true
			}
			if len(yearHits) > 1 {
				// Multiple same-year exacts: caller may re-rank by Animation genre.
				return yearHits[0], true
			}
		}
		// Search-order first exact (OMDb relevance).
		return exact[0], true
	}
	if year > 0 {
		for _, h := range exact {
			if parseYear(h.Year) == year {
				return h, true
			}
		}
	} else if len(exact) == 1 {
		return exact[0], true
	} else if len(exact) > 1 {
		// Non-anime, no year: prefer oldest among exacts as a safe default.
		best := exact[0]
		bestY := parseYear(best.Year)
		for _, h := range exact[1:] {
			hy := parseYear(h.Year)
			if hy <= 0 {
				continue
			}
			if bestY <= 0 || hy < bestY {
				best, bestY = h, hy
			}
		}
		return best, true
	}
	bestIdx := -1
	bestScore := -1
	for i, h := range hits {
		if !enoughTokensPresent(want, h.Title) {
			continue
		}
		hn := titleKey(h.Title)
		score := 0
		switch {
		case hn == wantKey:
			score = 100
		case strings.HasPrefix(hn, wantKey) || strings.HasPrefix(wantKey, hn):
			score = 80
		case strings.Contains(hn, wantKey) || strings.Contains(wantKey, hn):
			score = 60
		default:
			score = 30
		}
		hy := parseYear(h.Year)
		if year > 0 && hy == year {
			score += 20
		} else if year > 0 && hy > 0 && hy != year {
			score -= 10
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return searchHit{}, false
	}
	return hits[bestIdx], true
}

var movieTitleSuffixRe = regexp.MustCompile(`(?i)\s*(?:-\s*)?(?:the\s+)?movie\s*$`)

func stripMovieTitleSuffix(title string) string {
	t := strings.TrimSpace(title)
	stripped := strings.TrimSpace(movieTitleSuffixRe.ReplaceAllString(t, ""))
	if stripped == "" {
		return t
	}
	return stripped
}

func isAnimationGenre(genre string) bool {
	g := strings.ToLower(genre)
	return strings.Contains(g, "animation") || strings.Contains(g, "anime")
}

// titleKeysCompatible reports exact match or "The Movie" / prefix variants
// (e.g. want "some film" vs hit "some film the movie").
func titleKeysCompatible(want, have string) bool {
	wk := titleKey(want)
	hk := titleKey(have)
	if wk == "" || hk == "" {
		return false
	}
	if wk == hk {
		return true
	}
	wStrip := titleKey(stripMovieTitleSuffix(want))
	hStrip := titleKey(stripMovieTitleSuffix(have))
	if wStrip != "" && (wStrip == hk || wStrip == hStrip) {
		return true
	}
	if hStrip != "" && hStrip == wk {
		return true
	}
	if strings.HasPrefix(hk, wk+" ") || strings.HasPrefix(wk, hk+" ") {
		return true
	}
	return false
}

// animeCompatibleHits returns title-compatible search hits, preferring year when set.
func animeCompatibleHits(want string, year int, hits []searchHit) []searchHit {
	var matched []searchHit
	seenID := map[string]bool{}
	for _, h := range hits {
		if !titleKeysCompatible(want, h.Title) {
			continue
		}
		if h.ImdbID != "" && seenID[h.ImdbID] {
			continue
		}
		if year > 0 && parseYear(h.Year) != year {
			continue
		}
		if h.ImdbID != "" {
			seenID[h.ImdbID] = true
		}
		matched = append(matched, h)
	}
	if year > 0 && len(matched) == 0 {
		return animeCompatibleHits(want, 0, hits)
	}
	return matched
}

// runtimeInBallpark reports whether omdbMinutes is close enough to probedMinutes.
func runtimeInBallpark(probed, omdb int) bool {
	if probed <= 0 || omdb <= 0 {
		return true
	}
	delta := probed - omdb
	if delta < 0 {
		delta = -delta
	}
	tol := probed / 5 // ~20%
	if tol < 15 {
		tol = 15
	}
	return delta <= tol
}

func (c *Client) lookup(q url.Values) (*rpcapi.Result, error) {
	body, err := c.get(q)
	if err != nil {
		return nil, err
	}
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Response == "False" {
		msg := r.Error
		if msg == "" {
			msg = "no results"
		}
		return nil, fmt.Errorf("OMDb: %s", msg)
	}
	return &rpcapi.Result{
		Title:      r.Title,
		Year:       parseYear(r.Year),
		Plot:       r.Plot,
		Runtime:    parseRuntime(r.Runtime),
		Rating:     parseRating(r.ImdbRating),
		PosterURL:  posterURL(r.Poster),
		Provider:   "omdb",
		ProviderID: r.ImdbID,
		Message:    "Found on OMDb",
	}, nil
}

func (c *Client) lookupByID(imdbID string) (*rpcapi.Result, error) {
	return c.lookup(url.Values{"i": {imdbID}, "plot": {"full"}})
}

func (c *Client) search(title, typ string, year int) ([]searchHit, error) {
	q := url.Values{"s": {title}, "type": {typ}}
	if year > 0 {
		q.Set("y", strconv.Itoa(year))
	}
	body, err := c.get(q)
	if err != nil {
		return nil, err
	}
	var r searchResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Response == "False" {
		msg := r.Error
		if msg == "" {
			msg = "no results"
		}
		return nil, fmt.Errorf("OMDb: %s", msg)
	}
	return r.Search, nil
}

func (c *Client) lookupTitleType(title, typ string, year int, libraryType string, durationMinutes int) (*rpcapi.Result, error) {
	animeMovie := typ == "movie" && strings.EqualFold(strings.TrimSpace(libraryType), "anime")
	q := url.Values{"t": {title}, "type": {typ}, "plot": {"full"}}

	// Anime films: search full title then strip-"The Movie" fallback. Widen
	// compatible titles and rank by Animation + probed runtime ballpark.
	if animeMovie {
		titles := []string{title}
		if stripped := stripMovieTitleSuffix(title); stripped != "" && stripped != title {
			titles = append(titles, stripped)
		}
		var allHits []searchHit
		seen := map[string]bool{}
		for _, t := range titles {
			hits, err := c.search(t, typ, 0)
			if err != nil {
				continue
			}
			for _, h := range hits {
				id := h.ImdbID
				if id == "" {
					id = h.Title + h.Year
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				allHits = append(allHits, h)
			}
		}
		if res, err := c.pickAnimeMovieResult(title, year, durationMinutes, allHits); err == nil {
			return res, nil
		}
		return nil, fmt.Errorf("OMDb: no results")
	}

	if year > 0 {
		q.Set("y", strconv.Itoa(year))
		if res, err := c.lookup(q); err == nil {
			return res, nil
		}
		q.Del("y")
	}
	if year <= 0 {
		if hits, err := c.search(title, typ, 0); err == nil {
			if hit, ok := pickBestSearchHit(title, year, hits, libraryType); ok && hit.ImdbID != "" {
				return c.lookupByID(hit.ImdbID)
			}
		}
	}
	if res, err := c.lookup(q); err == nil {
		return res, nil
	}
	hits, err := c.search(title, typ, year)
	if err != nil {
		if year > 0 {
			hits, err = c.search(title, typ, 0)
		}
		if err != nil {
			return nil, err
		}
	}
	hit, ok := pickBestSearchHit(title, year, hits, libraryType)
	if !ok || hit.ImdbID == "" {
		return nil, fmt.Errorf("OMDb: no results")
	}
	return c.lookupByID(hit.ImdbID)
}

type animeMovieCand struct {
	res    *rpcapi.Result
	anim   bool
	delta  int
	inBall bool
}

func (c *Client) pickAnimeMovieResult(title string, year, durationMinutes int, hits []searchHit) (*rpcapi.Result, error) {
	candidates := animeCompatibleHits(title, year, hits)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("OMDb: no results")
	}
	var list []animeMovieCand
	for _, hit := range candidates {
		if hit.ImdbID == "" {
			continue
		}
		res, genre, err := c.lookupByIDGenre(hit.ImdbID)
		if err != nil {
			continue
		}
		anim := isAnimationGenre(genre)
		delta := 0
		if durationMinutes > 0 && res.Runtime > 0 {
			delta = durationMinutes - res.Runtime
			if delta < 0 {
				delta = -delta
			}
		}
		list = append(list, animeMovieCand{
			res: res, anim: anim, delta: delta,
			inBall: runtimeInBallpark(durationMinutes, res.Runtime),
		})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("OMDb: no results")
	}
	best := list[0]
	for _, cnd := range list[1:] {
		if betterAnimeMovieCand(cnd, best, durationMinutes > 0) {
			best = cnd
		}
	}
	return best.res, nil
}

func betterAnimeMovieCand(a, b animeMovieCand, haveDuration bool) bool {
	// Prefer Animation when one is and the other isn't.
	if a.anim != b.anim {
		return a.anim
	}
	if haveDuration {
		if a.inBall != b.inBall {
			return a.inBall
		}
		if a.delta != b.delta {
			return a.delta < b.delta
		}
	}
	return false
}

func (c *Client) LookupMovie(title string, year int, libraryType string, durationMinutes int) (*rpcapi.Result, error) {
	return c.lookupTitleType(title, "movie", year, libraryType, durationMinutes)
}

func (c *Client) LookupShow(title string, year int, libraryType string) (*rpcapi.Result, error) {
	return c.lookupTitleType(title, "series", year, libraryType, 0)
}

func (c *Client) lookupByIDGenre(imdbID string) (*rpcapi.Result, string, error) {
	body, err := c.get(url.Values{"i": {imdbID}, "plot": {"full"}})
	if err != nil {
		return nil, "", err
	}
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", err
	}
	if r.Response == "False" {
		msg := r.Error
		if msg == "" {
			msg = "no results"
		}
		return nil, "", fmt.Errorf("OMDb: %s", msg)
	}
	return &rpcapi.Result{
		Title:      r.Title,
		Year:       parseYear(r.Year),
		Plot:       r.Plot,
		Runtime:    parseRuntime(r.Runtime),
		Rating:     parseRating(r.ImdbRating),
		PosterURL:  posterURL(r.Poster),
		Provider:   "omdb",
		ProviderID: r.ImdbID,
		Message:    "Found on OMDb",
	}, r.Genre, nil
}

func (c *Client) LookupSeason(showTitle string, seasonNum int) (*rpcapi.Result, error) {
	body, err := c.get(url.Values{
		"t": {showTitle}, "type": {"series"}, "Season": {strconv.Itoa(seasonNum)},
	})
	if err != nil {
		return nil, err
	}
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Response == "False" {
		msg := r.Error
		if msg == "" {
			msg = "no results"
		}
		return nil, fmt.Errorf("OMDb: %s", msg)
	}
	title := fmt.Sprintf("Season %d", seasonNum)
	id := r.ImdbID
	if id == "" && len(r.Episodes) > 0 {
		id = r.Episodes[0].ImdbID
	}
	return &rpcapi.Result{
		Title:      title,
		Provider:   "omdb",
		ProviderID: id,
		Message:    "Found on OMDb",
	}, nil
}

func (c *Client) LookupEpisode(showTitle string, seasonNum, epNum int) (*rpcapi.Result, error) {
	res, err := c.lookup(url.Values{
		"t": {showTitle}, "type": {"series"},
		"Season": {strconv.Itoa(seasonNum)}, "Episode": {strconv.Itoa(epNum)},
		"plot": {"full"},
	})
	if err != nil {
		return nil, err
	}
	// OMDb returns the series poster for episodes; never treat it as a still.
	res.PosterURL = ""
	return res, nil
}
