package metadata

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TitleNFOFacts are show/movie NFO fields used as ServeMedia's source of truth.
type TitleNFOFacts struct {
	Title     string
	Year      string
	Premiered string
	Plot      string
	Rating    float64
}

// TitleNFOFactsFromDir reads tvshow.nfo or movie.nfo beside dir.
func TitleNFOFactsFromDir(dir string) (TitleNFOFacts, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return TitleNFOFacts{}, false
	}
	for _, name := range []string{"tvshow.nfo", "movie.nfo"} {
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		switch name {
		case "tvshow.nfo":
			n, err := ReadTVShowNFO(path)
			if err != nil || n == nil {
				continue
			}
			return TitleNFOFactsFromShow(*n), true
		default:
			n, err := ReadMovieNFO(path)
			if err != nil || n == nil {
				continue
			}
			return TitleNFOFactsFromMovie(*n), true
		}
	}
	return TitleNFOFacts{}, false
}

// TitleNFOFactsFromShow maps a tvshow NFO into cacheable facts.
func TitleNFOFactsFromShow(n TVShowNFO) TitleNFOFacts {
	return titleFacts(n.Title, n.Year, n.Premiered, n.Plot, NumericRating(n.Rating, n.Ratings))
}

// TitleNFOFactsFromMovie maps a movie NFO into cacheable facts.
func TitleNFOFactsFromMovie(n MovieNFO) TitleNFOFacts {
	return titleFacts(n.Title, n.Year, n.Premiered, n.Plot, NumericRating(n.Rating, n.Ratings))
}

func titleFacts(title string, year int, premiered, plot string, rating float64) TitleNFOFacts {
	premiered = strings.TrimSpace(premiered)
	y := ""
	if year > 0 {
		y = strconv.Itoa(year)
	} else if p := prefix4Year(premiered); p != "" {
		y = p
	}
	return TitleNFOFacts{
		Title:     strings.TrimSpace(title),
		Year:      y,
		Premiered: premiered,
		Plot:      strings.TrimSpace(plot),
		Rating:    rating,
	}
}

func prefix4Year(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return ""
	}
	y := s[:4]
	for _, r := range y {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return y
}
