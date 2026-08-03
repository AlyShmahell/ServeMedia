package metadata

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type NFOActor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
}

type NFORating struct {
	Name  string  `xml:"name,attr"`
	Max   float64 `xml:"max,attr"`
	Value float64 `xml:"value"`
	Votes int     `xml:"votes"`
}

type MovieNFO struct {
	XMLName       xml.Name    `xml:"movie"`
	Title         string      `xml:"title"`
	OriginalTitle string      `xml:"originaltitle"`
	SortTitle     string      `xml:"sorttitle"`
	Year          int         `xml:"year"`
	Plot          string      `xml:"plot"`
	Outline       string      `xml:"outline"`
	Tagline       string      `xml:"tagline"`
	Runtime       int         `xml:"runtime"`
	Rating        float64     `xml:"rating"`
	Ratings       []NFORating `xml:"ratings>rating"`
	MPAA          string      `xml:"mpaa"`
	Genre         []string    `xml:"genre"`
	Studio        []string    `xml:"studio"`
	Country       []string    `xml:"country"`
	Premiered     string      `xml:"premiered"`
	Director      []string    `xml:"director"`
	Credits       []string    `xml:"credits"`
	Actor         []NFOActor  `xml:"actor"`
}

type TVShowNFO struct {
	XMLName       xml.Name    `xml:"tvshow"`
	Title         string      `xml:"title"`
	OriginalTitle string      `xml:"originaltitle"`
	SortTitle     string      `xml:"sorttitle"`
	Year          int         `xml:"year"`
	Plot          string      `xml:"plot"`
	Outline       string      `xml:"outline"`
	Tagline       string      `xml:"tagline"`
	Runtime       int         `xml:"runtime"`
	Rating        float64     `xml:"rating"`
	Ratings       []NFORating `xml:"ratings>rating"`
	MPAA          string      `xml:"mpaa"`
	Genre         []string    `xml:"genre"`
	Studio        []string    `xml:"studio"`
	Country       []string    `xml:"country"`
	Premiered     string      `xml:"premiered"`
	Aired         string      `xml:"aired"`
	Director      []string    `xml:"director"`
	Credits       []string    `xml:"credits"`
	Actor         []NFOActor  `xml:"actor"`
}

type SeasonNFO struct {
	XMLName xml.Name `xml:"season"`
	Title   string   `xml:"title"`
	Plot    string   `xml:"plot"`
	Outline string   `xml:"outline"`
}

type EpisodeNFO struct {
	XMLName xml.Name `xml:"episodedetails"`
	Title   string   `xml:"title"`
	Season  int      `xml:"season"`
	Episode int      `xml:"episode"`
	Plot    string   `xml:"plot"`
	Runtime int      `xml:"runtime"`
	Aired   string   `xml:"aired"`
}

func (n MovieNFO) DisplayRating() string {
	return displayRating(n.Rating, n.Ratings)
}

func (n TVShowNFO) DisplayRating() string {
	return displayRating(n.Rating, n.Ratings)
}

func displayRating(simple float64, ratings []NFORating) string {
	if v := NumericRating(simple, ratings); v > 0 {
		if len(ratings) > 0 && ratings[0].Value > 0 && ratings[0].Name != "" {
			return fmt.Sprintf("%s %.1f", ratings[0].Name, ratings[0].Value)
		}
		return fmt.Sprintf("%.1f", v)
	}
	return ""
}

// NumericRating returns the best available numeric rating, or 0 if none.
func NumericRating(simple float64, ratings []NFORating) float64 {
	if len(ratings) > 0 && ratings[0].Value > 0 {
		return ratings[0].Value
	}
	if simple > 0 {
		return simple
	}
	return 0
}

func JoinNonEmpty(parts []string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

var (
	yearRe = regexp.MustCompile(`\((\d{4})\)`)
	// Scene-style year: Title.1994.quality or Title 1994 quality (1900–2099).
	yearSceneRe = regexp.MustCompile(`(?:^|[.\-_ ])((?:19|20)\d{2})(?:[.\-_ ]|$)`)
	epRe        = regexp.MustCompile(`(?i)(?:^|[.\-_ ])S(\d{1,2})E(\d{1,3})`)
	vidExt      = map[string]bool{".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".m4v": true, ".webm": true}
)

func IsVideo(name string) bool {
	return vidExt[strings.ToLower(filepath.Ext(name))]
}

func ParseTitleYear(name string) (title string, year int) {
	base := strings.TrimSpace(filepath.Base(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ReplaceAll(base, ".", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if m := yearRe.FindStringSubmatch(base); len(m) == 2 {
		year, _ = strconv.Atoi(m[1])
		base = strings.TrimSpace(yearRe.ReplaceAllString(base, ""))
	}
	return strings.TrimSpace(base), year
}

// YearFromName extracts a year from (1994) or scene-style .1994. / 1994 tokens.
func YearFromName(name string) int {
	base := strings.TrimSpace(filepath.Base(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if m := yearRe.FindStringSubmatch(base); len(m) == 2 {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	if m := yearSceneRe.FindStringSubmatch(base); len(m) == 2 {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	spaced := strings.ReplaceAll(strings.ReplaceAll(base, ".", " "), "_", " ")
	if m := yearSceneRe.FindStringSubmatch(spaced); len(m) == 2 {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	return 0
}

// TitleYearFromVideoPath derives lookup title/year from the parent folder,
// falling back to the video filename — same rules as the movie scanner.
// Flat multi-movie dirs and generic library parents (movies/films/…) use the filename.
// When the folder has a title but no year, adopt a year from the filename if present.
func TitleYearFromVideoPath(videoPath string) (title string, year int) {
	if UseFilenameMovieTitle(videoPath) {
		return ParseTitleYear(filepath.Base(videoPath))
	}
	dir := filepath.Dir(videoPath)
	base := filepath.Base(dir)
	title, year = ParseTitleYear(base)
	if title == "" || title == "." {
		title, year = ParseTitleYear(filepath.Base(videoPath))
		return title, year
	}
	if year == 0 {
		if y := YearFromName(filepath.Base(videoPath)); y > 0 {
			year = y
		}
	}
	return title, year
}

func ParseEpisode(name string) (season, episode int, ok bool) {
	m := epRe.FindStringSubmatch(name)
	if len(m) != 3 {
		return 0, 0, false
	}
	season, _ = strconv.Atoi(m[1])
	episode, _ = strconv.Atoi(m[2])
	return season, episode, true
}

func ReadMovieNFO(path string) (*MovieNFO, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n MovieNFO
	if err := xml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func WriteMovieNFO(path string, n MovieNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}

func ReadTVShowNFO(path string) (*TVShowNFO, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n TVShowNFO
	if err := xml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func WriteTVShowNFO(path string, n TVShowNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}

func ReadSeasonNFO(path string) (*SeasonNFO, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n SeasonNFO
	if err := xml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	if n.Plot == "" {
		n.Plot = n.Outline
	}
	return &n, nil
}

func ReadEpisodeNFO(path string) (*EpisodeNFO, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n EpisodeNFO
	if err := xml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// FindSeasonNFO looks for season.nfo or seasonNN.nfo in dir.
func FindSeasonNFO(dir string, seasonNum int) string {
	candidates := []string{
		filepath.Join(dir, "season.nfo"),
		filepath.Join(dir, fmt.Sprintf("season%02d.nfo", seasonNum)),
		filepath.Join(dir, fmt.Sprintf("season%d.nfo", seasonNum)),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}

func FindSidecar(dir, base string, names ...string) string {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, n := range names {
		p := filepath.Join(dir, base+"-"+n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// SanitizePathSegment cleans a title for use under metadata/ store dirs.
func SanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "")
	return repl.Replace(s)
}

// TitleYear formats "Title (Year)" when year > 0.
func TitleYear(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}
