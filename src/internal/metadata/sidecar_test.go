package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestNFO(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadVideoSidecarEpisode(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Show.S01E02.mkv")
	writeTestNFO(t, dir, "Show.S01E02.nfo",
		`<episodedetails><title>Broussard</title><plot>Story</plot><season>1</season><episode>2</episode><runtime>42</runtime><aired>2020-01-02</aired></episodedetails>`)
	side := ReadVideoSidecar(video)
	if side.Title != "Broussard" || side.Plot != "Story" || side.Season != 1 || side.Episode != 2 || side.Runtime != 42 || side.Aired != "2020-01-02" {
		t.Fatalf("sidecar %#v", side)
	}
	if got := VideoTitle(video); got != "Broussard" {
		t.Fatalf("title %q", got)
	}
}

func TestReadVideoSidecarBaseWinsOverEpisodeNFO(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "ep.mkv")
	writeTestNFO(t, dir, "episode.nfo", `<episodedetails><title>Shared</title></episodedetails>`)
	writeTestNFO(t, dir, "ep.nfo", `<episodedetails><title>Own</title></episodedetails>`)
	if got := VideoTitle(video); got != "Own" {
		t.Fatalf("title %q", got)
	}
}

func TestReadVideoSidecarMovieAndBlank(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Film.mkv")
	writeTestNFO(t, dir, "movie.nfo", `<movie><title>The Film</title><outline>Outline plot</outline><premiered>1999-01-01</premiered></movie>`)
	side := ReadVideoSidecar(video)
	if side.Title != "The Film" || side.Plot != "Outline plot" || side.Aired != "1999-01-01" {
		t.Fatalf("movie sidecar %#v", side)
	}
	writeTestNFO(t, dir, "movie.nfo", `<movie><title>  </title></movie>`)
	if got := VideoTitle(video); got != "" {
		t.Fatalf("blank title %q", got)
	}
}

func TestReadVideoSidecarMalformed(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Broken.mkv")
	writeTestNFO(t, dir, "Broken.nfo", `<episodedetails><title>oops`)
	if got := VideoTitle(video); got != "" {
		t.Fatalf("malformed title %q", got)
	}
}

func TestNFOTitleDispatch(t *testing.T) {
	dir := t.TempDir()
	ep := writeTestNFO(t, dir, "ep.nfo", `<episodedetails><title>Episode</title></episodedetails>`)
	tv := writeTestNFO(t, dir, "tvshow.nfo", `<tvshow><title>Show</title></tvshow>`)
	mv := writeTestNFO(t, dir, "movie.nfo", `<movie><title>Film</title></movie>`)
	blank := writeTestNFO(t, dir, "blank.nfo", `<movie><title> </title></movie>`)
	if got := NFOTitle(ep); got != "Episode" {
		t.Fatalf("episode %q", got)
	}
	if got := NFOTitle(tv); got != "Show" {
		t.Fatalf("tvshow %q", got)
	}
	if got := NFOTitle(mv); got != "Film" {
		t.Fatalf("movie %q", got)
	}
	if got := NFOTitle(blank); got != "" {
		t.Fatalf("blank %q", got)
	}
	if got := NFOTitle(filepath.Join(dir, "missing.nfo")); got != "" {
		t.Fatalf("missing %q", got)
	}
}

func TestFolderTitle(t *testing.T) {
	dir := t.TempDir()
	show := filepath.Join(dir, "Show")
	season := filepath.Join(show, "Season 2")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestNFO(t, show, "tvshow.nfo", `<tvshow><title>The Show</title></tvshow>`)
	writeTestNFO(t, season, "season.nfo", `<season><title>Second Season</title></season>`)
	if got := FolderTitle(show, 0); got != "The Show" {
		t.Fatalf("show %q", got)
	}
	if got := FolderTitle(season, 0); got != "Second Season" {
		t.Fatalf("season by name %q", got)
	}
	if got := FolderTitle(season, 2); got != "Second Season" {
		t.Fatalf("season by number %q", got)
	}
	if got := FolderTitle(filepath.Join(dir, "Bare"), 0); got != "" {
		t.Fatalf("bare %q", got)
	}
	series := filepath.Join(show, "Series 5")
	short := filepath.Join(show, "S01")
	if err := os.MkdirAll(series, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(short, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestNFO(t, series, "season05.nfo", `<season><title>Fifth</title></season>`)
	writeTestNFO(t, short, "season01.nfo", `<season><title>First</title></season>`)
	if got := FolderTitle(series, 0); got != "Fifth" {
		t.Fatalf("series folder %q", got)
	}
	if got := FolderTitle(short, 0); got != "First" {
		t.Fatalf("short folder %q", got)
	}
}
