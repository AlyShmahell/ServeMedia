package tvmaze

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/alyshmahell/medora-plugin-providers/internal/ratelimit"
	"github.com/alyshmahell/medora-plugin-sdk/rpcapi"
)

const apiBase = "https://api.tvmaze.com"

type Client struct {
	HTTP    *http.Client
	Limiter *ratelimit.Limiter
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) get(path string, q url.Values) ([]byte, error) {
	if c.Limiter != nil {
		if err := c.Limiter.Acquire(); err != nil {
			return nil, err
		}
	}
	u := apiBase + path
	if q != nil {
		u += "?" + q.Encode()
	}
	res, err := c.httpClient().Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 404 {
		return nil, fmt.Errorf("TVmaze: no results")
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("TVmaze %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

type image struct {
	Medium   string `json:"medium"`
	Original string `json:"original"`
}

type show struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Language       string   `json:"language"`
	Genres         []string `json:"genres"`
	Summary        string   `json:"summary"`
	Premiered      string   `json:"premiered"`
	Runtime        int      `json:"runtime"`
	AverageRuntime int      `json:"averageRuntime"`
	Rating         struct {
		Average *float64 `json:"average"`
	} `json:"rating"`
	Image *image `json:"image"`
}

type season struct {
	ID      int    `json:"id"`
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Image   *image `json:"image"`
}

type episode struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Season  int    `json:"season"`
	Number  int    `json:"number"`
	Summary string `json:"summary"`
	Runtime int    `json:"runtime"`
	Airdate string `json:"airdate"`
	Image   *image `json:"image"`
}

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func yearFrom(s string) int {
	if len(s) >= 4 {
		y, _ := strconv.Atoi(s[:4])
		return y
	}
	return 0
}

func imgURL(img *image) string {
	if img == nil {
		return ""
	}
	if img.Original != "" {
		return img.Original
	}
	return img.Medium
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

// enoughTokensPresent reports whether have is a plausible title for want.
// Single-token queries require an exact titleKey (not a longer superset).
// Multi-token: strict majority of tokens, and candidate must not be far shorter;
// romanized anime titles may also match via longest token (len≥5) when animeCand.
func enoughTokensPresent(want, have string, animeCand bool) bool {
	tokens := significantTokens(want)
	if len(tokens) == 0 {
		return true
	}
	hn := titleKey(have)
	wantKey := titleKey(want)
	if len(tokens) == 1 {
		return hn == wantKey || hn == tokens[0]
	}
	haveToks := significantTokens(have)
	if len(haveToks)*2 < len(tokens) {
		return false
	}
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
	if matched*2 > len(tokens) {
		return true
	}
	if animeCand {
		// English query vs Japanese/romanized name (shared proper-name token).
		if len(longest) >= 5 && strings.Contains(hn, longest) {
			return true
		}
		// Shared proper-name tokens without English rest (romanized listings).
		if matched >= 2 {
			return true
		}
	}
	return false
}

func shortenedQuery(title string) string {
	tokens := significantTokens(title)
	if len(tokens) == 0 {
		return ""
	}
	joined := strings.Join(tokens, " ")
	if joined != strings.TrimSpace(title) && joined != titleKey(title) {
		return joined
	}
	longest := tokens[0]
	for _, t := range tokens[1:] {
		if len(t) > len(longest) {
			longest = t
		}
	}
	if len(longest) >= 5 && longest != titleKey(title) {
		return longest
	}
	return ""
}

func hasGenre(s show, name string) bool {
	name = strings.ToLower(name)
	for _, g := range s.Genres {
		if strings.EqualFold(g, name) {
			return true
		}
	}
	return false
}

// libraryTypeBias scores a show for Medora library type.
func libraryTypeBias(libraryType string, s show) float64 {
	switch strings.ToLower(strings.TrimSpace(libraryType)) {
	case "anime":
		if strings.EqualFold(s.Type, "Animation") || hasGenre(s, "Anime") {
			bias := 40.0
			if strings.EqualFold(s.Language, "Japanese") {
				bias += 10
			}
			return bias
		}
		return -50
	case "tv":
		if hasGenre(s, "Anime") && strings.EqualFold(s.Type, "Animation") {
			return -15
		}
		return 5
	default:
		return 0
	}
}

type searchHit struct {
	Score float64 `json:"score"`
	Show  show    `json:"show"`
}

func bestHitByBias(libraryType string, hits []searchHit) (show, bool) {
	bestIdx := -1
	bestScore := -1e9
	for i, h := range hits {
		bias := libraryTypeBias(libraryType, h.Show)
		score := h.Score + bias
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return show{}, false
	}
	return hits[bestIdx].Show, true
}

func filterExcluded(hits []searchHit, excludeIDs []string) []searchHit {
	if len(excludeIDs) == 0 {
		return hits
	}
	skip := map[string]bool{}
	for _, id := range excludeIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			skip[id] = true
		}
	}
	out := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		if skip[strconv.Itoa(h.Show.ID)] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// pickBestShow prefers article-stripped exact name (with library bias among ties),
// else score + closeness + library type. Anime single-token queries can fall back
// to the best Animation/Anime hit when English tokens are absent from romanized names.
// Multi-token anime fallback only accepts hits that pass enoughTokensPresent.
func pickBestShow(want string, year int, libraryType string, hits []searchHit) (show, bool) {
	wantKey := titleKey(want)
	if wantKey == "" || len(hits) == 0 {
		return show{}, false
	}
	anime := strings.EqualFold(strings.TrimSpace(libraryType), "anime")
	var exacts []searchHit
	for _, h := range hits {
		if titleKey(h.Show.Name) == wantKey {
			exacts = append(exacts, h)
		}
	}
	if len(exacts) > 0 {
		if year > 0 {
			var yearHits []searchHit
			for _, h := range exacts {
				if yearFrom(h.Show.Premiered) == year {
					yearHits = append(yearHits, h)
				}
			}
			if len(yearHits) > 0 {
				return bestHitByBias(libraryType, yearHits)
			}
		}
		return bestHitByBias(libraryType, exacts)
	}
	bestIdx := -1
	bestScore := -1e9
	for i, h := range hits {
		hn := titleKey(h.Show.Name)
		bias := libraryTypeBias(libraryType, h.Show)
		if !enoughTokensPresent(want, h.Show.Name, bias > 0) {
			continue
		}
		closeness := 0.0
		switch {
		case hn == wantKey:
			closeness = 100
		case strings.HasPrefix(hn, wantKey) || strings.HasPrefix(wantKey, hn):
			closeness = 80
		case strings.Contains(hn, wantKey) || strings.Contains(wantKey, hn):
			closeness = 60
		default:
			closeness = 30
		}
		score := h.Score + closeness + bias
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		return hits[bestIdx].Show, true
	}
	// English title vs Japanese/romanized listing.
	// Multi-token: prefer anime hits that pass enoughTokensPresent; otherwise allow
	// the best anime hit when the top search result is already anime (romanized names).
	// Single-token: best Animation/Anime/Japanese hit.
	tokens := significantTokens(want)
	if anime && len(tokens) >= 1 {
		var withTokens []searchHit
		var animeHits []searchHit
		for _, h := range hits {
			if libraryTypeBias(libraryType, h.Show) <= 0 {
				continue
			}
			animeHits = append(animeHits, h)
			if enoughTokensPresent(want, h.Show.Name, true) {
				withTokens = append(withTokens, h)
			}
		}
		if len(animeHits) == 0 {
			return show{}, false
		}
		if len(tokens) >= 2 {
			if len(withTokens) > 0 {
				return bestHitByBias(libraryType, withTokens)
			}
			if libraryTypeBias(libraryType, hits[0].Show) > 0 {
				return bestHitByBias(libraryType, animeHits)
			}
			return show{}, false
		}
		return bestHitByBias(libraryType, animeHits)
	}
	return show{}, false
}

func resultFromShow(s show) *rpcapi.Result {
	runtime := s.Runtime
	if runtime == 0 {
		runtime = s.AverageRuntime
	}
	rating := 0.0
	if s.Rating.Average != nil {
		rating = *s.Rating.Average
	}
	return &rpcapi.Result{
		Title:      s.Name,
		Year:       yearFrom(s.Premiered),
		Plot:       stripHTML(s.Summary),
		Runtime:    runtime,
		Rating:     rating,
		PosterURL:  imgURL(s.Image),
		Provider:   "tvmaze",
		ProviderID: strconv.Itoa(s.ID),
		Message:    "Found on TVmaze",
	}
}

func (c *Client) searchShows(q string) ([]searchHit, error) {
	body, err := c.get("/search/shows", url.Values{"q": {q}})
	if err != nil {
		return nil, err
	}
	var hits []searchHit
	if err := json.Unmarshal(body, &hits); err != nil {
		return nil, err
	}
	return hits, nil
}

func (c *Client) LookupShow(title string, year int, libraryType string, excludeIDs ...string) (*rpcapi.Result, error) {
	title = strings.TrimSpace(title)
	hits, err := c.searchShows(title)
	if err != nil {
		return nil, err
	}
	hits = filterExcluded(hits, excludeIDs)
	s, ok := pickBestShow(title, year, libraryType, hits)
	if !ok && len(hits) > 0 {
		if alt := shortenedQuery(title); alt != "" && alt != title {
			if altHits, err := c.searchShows(alt); err == nil {
				altHits = filterExcluded(altHits, excludeIDs)
				if s2, ok2 := pickBestShow(title, year, libraryType, altHits); ok2 {
					s, ok = s2, true
				}
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("TVmaze: no results")
	}
	return resultFromShow(s), nil
}

func (c *Client) resolveShowID(showTitle, libraryType, showProvider, showProviderID string) (string, error) {
	if showProviderID != "" && (showProvider == "" || showProvider == "tvmaze") {
		return showProviderID, nil
	}
	showRes, err := c.LookupShow(showTitle, 0, libraryType)
	if err != nil {
		return "", err
	}
	return showRes.ProviderID, nil
}

func (c *Client) LookupSeason(showTitle string, seasonNum int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error) {
	id, err := c.resolveShowID(showTitle, libraryType, showProvider, showProviderID)
	if err != nil {
		return nil, err
	}
	body, err := c.get("/shows/"+id+"/seasons", nil)
	if err != nil {
		return nil, err
	}
	var seasons []season
	if err := json.Unmarshal(body, &seasons); err != nil {
		return nil, err
	}
	for _, s := range seasons {
		if s.Number == seasonNum {
			title := s.Name
			if title == "" {
				title = fmt.Sprintf("Season %d", seasonNum)
			}
			return &rpcapi.Result{
				Title:      title,
				Plot:       stripHTML(s.Summary),
				PosterURL:  imgURL(s.Image),
				Provider:   "tvmaze",
				ProviderID: strconv.Itoa(s.ID),
				Message:    "Found on TVmaze",
			}, nil
		}
	}
	return nil, fmt.Errorf("TVmaze: season %d not found", seasonNum)
}

func (c *Client) LookupEpisode(showTitle string, seasonNum, epNum int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error) {
	id, err := c.resolveShowID(showTitle, libraryType, showProvider, showProviderID)
	if err != nil {
		return nil, err
	}
	body, err := c.get("/shows/"+id+"/episodebynumber", url.Values{
		"season": {strconv.Itoa(seasonNum)},
		"number": {strconv.Itoa(epNum)},
	})
	if err != nil {
		return nil, err
	}
	var e episode
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, err
	}
	title := e.Name
	if title == "" {
		title = fmt.Sprintf("Episode %d", epNum)
	}
	return &rpcapi.Result{
		Title:      title,
		Plot:       stripHTML(e.Summary),
		Runtime:    e.Runtime,
		StillURL:   imgURL(e.Image),
		Provider:   "tvmaze",
		ProviderID: strconv.Itoa(e.ID),
		Message:    "Found on TVmaze",
	}, nil
}
