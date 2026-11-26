package server

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

// orderLibraryCards reorders a title page's season folders or a season page's
// episodes from the titles and dates the cards show. A season page returns true.
func (s *Server) orderLibraryCards(ctx context.Context, lib *db.Library, rel, sortKey string, dirs, files []nodeCard) bool {
	if s == nil || s.DB == nil || lib == nil || rel == "" {
		return false
	}
	id, err := s.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return false
	}
	if _, _, ok := s.DB.SeasonEdit(ctx, id); ok {
		sortEpisodeCards(files, sortKey)
		return true
	}
	node, err := s.DB.GetLibraryNode(ctx, id)
	if err != nil {
		return false
	}
	_, kind, _, _, _ := s.DB.ItemByNode(ctx, id)
	if strings.EqualFold(strings.TrimSpace(kind), "movie") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(kind), "show") || titleFolder(lib.Path, rel, node, dirs) {
		sortTitleFolders(lib.Path, sortKey, dirs)
	}
	return false
}

func titleFolder(libPath, rel string, node *db.LibraryNode, dirs []nodeCard) bool {
	if len(dirs) == 0 {
		return false
	}
	if node != nil {
		if strings.EqualFold(strings.TrimSpace(node.MMKind.String), "movie") {
			return false
		}
		if isShowKind(node.MMKind.String) {
			return true
		}
	}
	dir := filepath.Join(libPath, filepath.FromSlash(rel))
	if st, err := os.Stat(filepath.Join(dir, "movie.nfo")); err == nil && !st.IsDir() {
		if st, err := os.Stat(filepath.Join(dir, "tvshow.nfo")); err != nil || st.IsDir() {
			return false
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "tvshow.nfo")); err == nil && !st.IsDir() {
		return true
	}
	for _, card := range dirs {
		if _, ok := metadata.ParseSeasonFolderName(card.Node.Name); !ok {
			return false
		}
	}
	return true
}

func librarySortMode(sortKey string) string {
	switch strings.TrimSpace(sortKey) {
	case "year", "released":
		return "year"
	case "date_added", "added":
		return "added"
	case "episode":
		return "episode"
	default:
		return "name"
	}
}

func sortTitleFolders(libPath, sortKey string, dirs []nodeCard) {
	mode := librarySortMode(sortKey)
	sort.SliceStable(dirs, func(i, j int) bool {
		a, b := dirs[i], dirs[j]
		switch mode {
		case "year":
			ad := seasonReleaseDate(filepath.Join(libPath, filepath.FromSlash(a.Rel)), a)
			bd := seasonReleaseDate(filepath.Join(libPath, filepath.FromSlash(b.Rel)), b)
			if c := newerFirst(ad, bd); c != 0 {
				return c < 0
			}
		case "added":
			if c := newerFirst(a.Node.DateAdded, b.Node.DateAdded); c != 0 {
				return c < 0
			}
		default:
			if c := cmpSeasonName(a, b); c != 0 {
				return c < 0
			}
			if c := cmpFold(a.Title, b.Title); c != 0 {
				return c < 0
			}
		}
		if c := cmpSeasonNumber(a, b); c != 0 {
			return c < 0
		}
		if c := cmpFold(a.Title, b.Title); c != 0 {
			return c < 0
		}
		return a.Rel < b.Rel
	})
}

func sortEpisodeCards(files []nodeCard, sortKey string) {
	mode := librarySortMode(sortKey)
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		switch mode {
		case "year":
			if c := newerFirst(factValue(a, "Aired"), factValue(b, "Aired")); c != 0 {
				return c < 0
			}
		case "added":
			if c := newerFirst(a.Node.DateAdded, b.Node.DateAdded); c != 0 {
				return c < 0
			}
		case "episode":
			if c := cmpEpisodeNumber(a, b); c != 0 {
				return c < 0
			}
		default:
			if c := cmpFold(a.Title, b.Title); c != 0 {
				return c < 0
			}
		}
		if c := cmpEpisodeNumber(a, b); c != 0 {
			return c < 0
		}
		if c := cmpFold(a.Title, b.Title); c != 0 {
			return c < 0
		}
		return a.Rel < b.Rel
	})
}

// cmpSeasonName orders two season-named folders by season number.
// A folder that is not a season name does not participate.
func cmpSeasonName(a, b nodeCard) int {
	an, aok := metadata.ParseSeasonFolderName(a.Node.Name)
	bn, bok := metadata.ParseSeasonFolderName(b.Node.Name)
	if aok && bok && an != bn {
		return an - bn
	}
	return 0
}

func cmpSeasonNumber(a, b nodeCard) int {
	an, aok := seasonNumber(a)
	bn, bok := seasonNumber(b)
	switch {
	case aok && bok:
		return an - bn
	case aok:
		return -1
	case bok:
		return 1
	default:
		return 0
	}
}

func seasonNumber(card nodeCard) (int, bool) {
	if n, ok := metadata.ParseSeasonFolderName(card.Node.Name); ok {
		return n, true
	}
	return parseCardInt(card.Node.SeasonNum.String)
}

func seasonReleaseDate(dir string, card nodeCard) string {
	num, _ := seasonNumber(card)
	if p := metadata.FindSeasonNFO(dir, num); p != "" {
		if n, err := metadata.ReadSeasonNFO(p); err == nil && n != nil {
			if premiered := strings.TrimSpace(n.Premiered); premiered != "" {
				return premiered
			}
		}
	}
	return earliestAired(dir)
}

func earliestAired(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestAt time.Time
	for _, entry := range entries {
		if entry.IsDir() || !metadata.IsVideo(entry.Name()) {
			continue
		}
		aired := strings.TrimSpace(metadata.ReadVideoSidecar(filepath.Join(dir, entry.Name())).Aired)
		at, ok := parseSortTime(aired)
		if !ok {
			continue
		}
		if best == "" || at.Before(bestAt) {
			best, bestAt = aired, at
		}
	}
	return best
}

func episodeNumber(card nodeCard) (int, bool) {
	if n, ok := parseCardInt(card.Node.EpisodeNum.String); ok {
		return n, true
	}
	return parseCardInt(factValue(card, "Episode"))
}

func factValue(card nodeCard, label string) string {
	for _, fact := range card.Facts {
		if fact.Label == label {
			return strings.TrimSpace(fact.Value)
		}
	}
	return ""
}

func parseCardInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func cmpEpisodeNumber(a, b nodeCard) int {
	an, aok := episodeNumber(a)
	bn, bok := episodeNumber(b)
	switch {
	case aok && bok:
		return an - bn
	case aok:
		return -1
	case bok:
		return 1
	default:
		return 0
	}
}

func cmpFold(a, b string) int {
	return strings.Compare(strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b)))
}

func parseSortTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05", "2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// newerFirst reports whether a should come before b when newer dates lead.
// Undated values come last.
func newerFirst(a, b string) int {
	ta, aok := parseSortTime(a)
	tb, bok := parseSortTime(b)
	switch {
	case aok && bok:
		if ta.Equal(tb) {
			return 0
		}
		if ta.After(tb) {
			return -1
		}
		return 1
	case aok:
		return -1
	case bok:
		return 1
	default:
		return 0
	}
}
