package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

type heroChip struct {
	Kind  string
	Label string
	Value string
	Title string
	Tone  int
}

func (s *Server) heroChips(ctx context.Context, lib *db.Library, rel string, mediaID int64) []heroChip {
	if lib == nil {
		return nil
	}
	dir := filepath.Join(lib.Path, filepath.FromSlash(rel))
	for _, p := range heroNFOPaths(dir, s.storedNFO(ctx, mediaID)) {
		if chips := chipsFromNFO(p); len(chips) > 0 {
			return chips
		}
	}
	return nil
}

func (s *Server) ensureLibraryRatings(ctx context.Context, libraryID int64) {
	if s == nil || s.DB == nil || libraryID <= 0 {
		return
	}
	items, err := s.DB.ListUnratedPosterItems(ctx, libraryID)
	if err != nil || len(items) == 0 {
		return
	}
	store := s.metadataStore()
	for _, it := range items {
		rel := strings.TrimSpace(it.NFOPath)
		if rel == "" {
			continue
		}
		nfoPath := rel
		if !filepath.IsAbs(rel) {
			if store == "" {
				continue
			}
			nfoPath = filepath.Join(store, filepath.FromSlash(rel))
		}
		rating, ok := ratingFromNFO(nfoPath, it.Kind)
		if !ok {
			continue
		}
		_ = s.DB.SetMediaItemRating(ctx, it.ID, rating)
	}
}

func (s *Server) metadataStore() string {
	if s == nil {
		return ""
	}
	if s.Fetch != nil && strings.TrimSpace(s.Fetch.Store) != "" {
		return s.Fetch.Store
	}
	if s.Cfg != nil {
		return s.Cfg.Store.Path
	}
	return ""
}

func ratingFromNFO(path, kind string) (float64, bool) {
	if kind == "show" {
		n, err := metadata.ReadTVShowNFO(path)
		if err != nil || n == nil {
			return 0, false
		}
		return metadata.NumericRating(n.Rating, n.Ratings), true
	}
	n, err := metadata.ReadMovieNFO(path)
	if err != nil || n == nil {
		return 0, false
	}
	return metadata.NumericRating(n.Rating, n.Ratings), true
}

func (s *Server) storedNFO(ctx context.Context, mediaID int64) string {
	if mediaID <= 0 || s.DB == nil {
		return ""
	}
	it, err := s.DB.GetMediaItem(ctx, mediaID)
	if err != nil || it == nil || !it.NFOPath.Valid {
		return ""
	}
	rel := strings.TrimSpace(it.NFOPath.String)
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	store := s.metadataStore()
	if store == "" {
		return ""
	}
	return filepath.Join(store, rel)
}

func heroNFOPaths(dir, stored string) []string {
	var out []string
	for _, name := range []string{"tvshow.nfo", "movie.nfo"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.EqualFold(filepath.Ext(name), ".nfo") {
				continue
			}
			low := strings.ToLower(name)
			if low == "tvshow.nfo" || low == "movie.nfo" || strings.HasPrefix(low, "season") {
				continue
			}
			out = append(out, filepath.Join(dir, name))
		}
	}
	if stored != "" {
		out = append(out, stored)
	}
	return out
}

func chipsFromNFO(path string) []heroChip {
	b, err := os.ReadFile(path)
	if err != nil || len(bytesTrim(b)) == 0 {
		return nil
	}
	low := strings.ToLower(string(b))
	switch {
	case strings.Contains(low, "<tvshow"):
		n, err := metadata.ReadTVShowNFO(path)
		if err != nil || n == nil {
			return nil
		}
		return showChips(n)
	case strings.Contains(low, "<movie"):
		n, err := metadata.ReadMovieNFO(path)
		if err != nil || n == nil {
			return nil
		}
		return movieChips(n)
	default:
		return nil
	}
}

func movieChips(n *metadata.MovieNFO) []heroChip {
	if n == nil {
		return nil
	}
	return factChips(n.Genre, n.DisplayRating(), n.Runtime, n.Language, n.Premiered, n.EndedOn(), n.Country, n.Networks())
}

func showChips(n *metadata.TVShowNFO) []heroChip {
	if n == nil {
		return nil
	}
	return factChips(n.Genre, n.DisplayRating(), n.Runtime, n.Language, n.Premiered, n.EndedOn(), n.Country, n.Networks())
}

func factChips(genres []string, rating string, runtime int, language, premiered, ended string, countries, networks []string) []heroChip {
	var chips []heroChip
	if names := joinParts(genres); names != "" {
		chips = append(chips, heroChip{Kind: "genre", Value: names, Title: "Genre"})
	}
	if rating = strings.TrimSpace(rating); rating != "" {
		chips = append(chips, heroChip{Kind: "rating", Label: "Rating", Value: rating, Title: "Rating"})
	}
	if label := metadata.FormatRuntime(runtime); label != "" {
		chips = append(chips, heroChip{Kind: "runtime", Label: "Runtime", Value: label, Title: "Runtime"})
	}
	if language = strings.TrimSpace(language); language != "" {
		chips = append(chips, heroChip{Kind: "language", Label: "Language", Value: language, Title: "Language"})
	}
	if dates := dateSpan(premiered, ended); dates != "" {
		chips = append(chips, heroChip{Kind: "dates", Value: dates, Title: "Premiered to ended"})
	}
	for _, c := range countries {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		chips = append(chips, heroChip{Kind: "country", Value: c, Title: "Country"})
	}
	for _, n := range networks {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		chips = append(chips, heroChip{Kind: "network", Value: n, Title: "Network"})
	}
	return chips
}

func joinParts(parts []string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func dateSpan(premiered, ended string) string {
	premiered = strings.TrimSpace(premiered)
	ended = strings.TrimSpace(ended)
	switch {
	case premiered != "" && ended != "":
		return premiered + " – " + ended
	case premiered != "":
		return premiered
	default:
		return ended
	}
}
