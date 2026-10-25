package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactsFromAttrs(t *testing.T) {
	f := FactsFromAttrs(map[string]string{
		"genres":    "Drama, Comedy, drama",
		"rating":    "8.4/10",
		"runtime":   "106 min",
		"language":  "ja",
		"premiered": "2016-07-16",
		"ended":     "2019-05-19",
		"country":   "Japan, Japan",
		"network":   "HBO",
	})
	if len(f.Genres) != 2 || f.Genres[0] != "Drama" || f.Rating != 8.4 || f.Runtime != 106 {
		t.Fatalf("%#v", f)
	}
	if f.Language != "ja" || f.Premiered != "2016-07-16" || f.Ended != "2019-05-19" {
		t.Fatalf("%#v", f)
	}
	if len(f.Countries) != 1 || f.Countries[0] != "Japan" || f.Networks[0] != "HBO" {
		t.Fatalf("%#v", f)
	}
	if ParseRuntime("1h 46m") != 106 || ParseRuntime("0") != 0 || ParseRuntime("N/A") != 0 {
		t.Fatal("runtime")
	}
	if ParseRating("imdb 8.1") != 8.1 || ParseRating("N/A") != 0 {
		t.Fatal("rating")
	}
	if FormatRuntime(42) != "42 min" || FormatRuntime(106) != "1h 46m" || FormatRuntime(120) != "2h" {
		t.Fatalf("%q %q %q", FormatRuntime(42), FormatRuntime(106), FormatRuntime(120))
	}
}

func TestMovieNFOFactsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "movie.nfo")
	nfo := MovieNFO{Title: "Film", Year: 2016, Plot: "plot"}
	nfo.ApplyFacts(FactsFromAttrs(map[string]string{
		"genres": "Adventure", "rating": "8.4", "runtime": "106",
		"language": "en", "premiered": "2016-07-16", "country": "Japan",
	}))
	if err := WriteMovieNFO(p, nfo); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMovieNFO(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Genre) != 1 || got.Genre[0] != "Adventure" || got.Rating != 8.4 || got.Runtime != 106 {
		t.Fatalf("%#v", got)
	}
	if got.Language != "en" || got.Premiered != "2016-07-16" || len(got.Country) != 1 {
		t.Fatalf("%#v", got)
	}
	blank := MovieNFO{Title: "Film", Genre: []string{"Keep"}, Runtime: 90, Language: "ja"}
	blank.ApplyFacts(NFOFacts{})
	if blank.Genre[0] != "Keep" || blank.Runtime != 90 || blank.Language != "ja" {
		t.Fatalf("blank overwrite %#v", blank)
	}
}

func TestShowNFOEndedAndNetwork(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tvshow.nfo")
	raw := `<?xml version="1.0"?><tvshow><title>Show</title><enddate>2019-05-19</enddate><studio>HBO</studio><genre>Drama</genre></tvshow>`
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ReadTVShowNFO(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.EndedOn() != "2019-05-19" {
		t.Fatalf("ended %q", n.EndedOn())
	}
	if nets := n.Networks(); len(nets) != 1 || nets[0] != "HBO" {
		t.Fatalf("networks %#v", nets)
	}
	n.Ended = "2020-01-01"
	n.Network = []string{"BBC"}
	if n.EndedOn() != "2020-01-01" || n.Networks()[0] != "BBC" {
		t.Fatalf("%#v", n)
	}
}

func TestEpisodeNFOOmitsEmptyRuntime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ep.nfo")
	if err := WriteEpisodeNFO(p, EpisodeNFO{Title: "Pilot", Season: 1, Episode: 1, Plot: "Hello", Aired: "2024-07-07"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "<runtime>") {
		t.Fatalf("runtime written:\n%s", s)
	}
	if !strings.Contains(s, "<plot>Hello</plot>") || !strings.Contains(s, "<aired>2024-07-07</aired>") {
		t.Fatalf("%s", s)
	}
	got, err := ReadEpisodeNFO(p)
	if err != nil || got.Plot != "Hello" || got.Aired != "2024-07-07" || got.Runtime != 0 {
		t.Fatalf("%#v %v", got, err)
	}
	if err := WriteEpisodeNFO(p, EpisodeNFO{Title: "Pilot", Season: 1, Episode: 1, Plot: "Hello", Runtime: 24, Aired: "2024-07-07"}); err != nil {
		t.Fatal(err)
	}
	got, err = ReadEpisodeNFO(p)
	if err != nil || got.Runtime != 24 {
		t.Fatalf("%#v %v", got, err)
	}
}
