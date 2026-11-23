package server

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/web"
)

func TestLibraryTitleSeasonSort(t *testing.T) {
	ctx := context.Background()
	media := t.TempDir()
	d := openSortDB(t)
	defer d.Close()
	u, lib := sortLibrary(t, d, media)

	show := filepath.Join(media, "Show")
	writeSortFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Show</title></tvshow>`)
	writeSortFile(t, filepath.Join(show, "Season 1", "season.nfo"), `<season><title>Zed</title></season>`)
	writeSortFile(t, filepath.Join(show, "Season 1", "early.mkv"), "")
	writeSortFile(t, filepath.Join(show, "Season 1", "early.nfo"), `<episodedetails><title>Early</title><aired>2009-05-01</aired></episodedetails>`)
	writeSortFile(t, filepath.Join(show, "Season 1", "late.mkv"), "")
	writeSortFile(t, filepath.Join(show, "Season 1", "late.nfo"), `<episodedetails><title>Late</title><aired>2020-01-01</aired></episodedetails>`)
	writeSortFile(t, filepath.Join(show, "Season 2", "season.nfo"), `<season><title>Mid</title><premiered>2012-06-01</premiered></season>`)
	writeSortFile(t, filepath.Join(show, "Season 10", "season.nfo"), `<season><title>Aaa</title><premiered>2011-03-01</premiered></season>`)

	showID, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, showID, db.NodeGroup{Kind: "show", Title: "Show"}); err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1", "dir")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 2", "dir")
	if err != nil {
		t.Fatal(err)
	}
	s10, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 10", "dir")
	if err != nil {
		t.Fatal(err)
	}
	setDateAdded(t, d, s1, "2020-01-01T00:00:00Z")
	setDateAdded(t, d, s2, "2020-06-01T00:00:00Z")
	setDateAdded(t, d, s10, "2021-01-01T00:00:00Z")

	film := filepath.Join(media, "Film")
	writeSortFile(t, filepath.Join(film, "movie.nfo"), `<movie><title>Film</title></movie>`)
	filmID, err := d.UpsertNodeChain(ctx, lib.ID, "Film", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, filmID, db.NodeGroup{Kind: "movie", Title: "Film"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Film/Season 10", "dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Film/Season 2", "dir"); err != nil {
		t.Fatal(err)
	}

	s := &Server{Cfg: &config.Config{}, DB: d}
	byName := s.libraryPageData(ctx, u, lib, "name", "", "Show", false)
	if byName["Season"] == true {
		t.Fatal("title page marked as a season")
	}
	if got := cardNames(pageNodes(t, byName)); got != "Season 1,Season 2,Season 10" {
		t.Fatalf("title order %s", got)
	}
	if titles := cardTitles(pageNodes(t, byName)); titles != "Zed,Mid,Aaa" {
		t.Fatalf("displayed titles %s", titles)
	}

	byYear := s.libraryPageData(ctx, u, lib, "year", "", "Show", false)
	if got := cardNames(pageNodes(t, byYear)); got != "Season 2,Season 10,Season 1" {
		t.Fatalf("released order %s", got)
	}
	byReleased := s.libraryPageData(ctx, u, lib, "released", "", "Show", false)
	if got := cardNames(pageNodes(t, byReleased)); got != "Season 2,Season 10,Season 1" {
		t.Fatalf("released alias %s", got)
	}

	byAdded := s.libraryPageData(ctx, u, lib, "date_added", "", "Show", false)
	if got := cardNames(pageNodes(t, byAdded)); got != "Season 10,Season 2,Season 1" {
		t.Fatalf("added order %s", got)
	}

	movie := s.libraryPageData(ctx, u, lib, "name", "", "Film", false)
	if got := cardNames(pageNodes(t, movie)); got != "Season 10,Season 2" {
		t.Fatalf("movie folder title order %s", got)
	}
	movieYear := s.libraryPageData(ctx, u, lib, "year", "", "Film", false)
	if got := cardNames(pageNodes(t, movieYear)); got != "Season 10,Season 2" {
		t.Fatalf("movie folder released order %s", got)
	}
}

func TestLibraryEpisodeSort(t *testing.T) {
	ctx := context.Background()
	media := t.TempDir()
	d := openSortDB(t)
	defer d.Close()
	u, lib := sortLibrary(t, d, media)

	showDir := filepath.Join(media, "Show", "Season 1")
	writeSortFile(t, filepath.Join(showDir, "e10.nfo"), `<episodedetails><title>Aaa</title><season>1</season><episode>10</episode><aired>2010-01-01</aired></episodedetails>`)
	writeSortFile(t, filepath.Join(showDir, "e2.nfo"), `<episodedetails><title>Mid</title><season>1</season><episode>2</episode><aired>2011-06-01</aired></episodedetails>`)
	writeSortFile(t, filepath.Join(showDir, "e1.nfo"), `<episodedetails><title>Zed</title><season>1</season><episode>1</episode></episodedetails>`)

	showNode, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, showNode, db.NodeGroup{Kind: "show", Title: "Show"}); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: filepath.Join(media, "Show"), Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, showID, showNode); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1", "dir"); err != nil {
		t.Fatal(err)
	}
	e10, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1/e10.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1/e2.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	e1, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1/e1.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, e10, showNode, "1", "10", "Zed", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, e2, showNode, "1", "2", "Aaa", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, e1, showNode, "1", "1", "Mid", "", ""); err != nil {
		t.Fatal(err)
	}
	setDateAdded(t, d, e10, "2020-06-01T00:00:00Z")
	setDateAdded(t, d, e2, "2021-03-01T00:00:00Z")
	setDateAdded(t, d, e1, "2020-01-01T00:00:00Z")

	s := &Server{Cfg: &config.Config{}, DB: d}
	byName := s.libraryPageData(ctx, u, lib, "name", "", "Show/Season 1", false)
	if byName["Season"] != true {
		t.Fatal("season page missing Season flag")
	}
	if got := cardNames(pageFiles(t, byName)); got != "e10.mkv,e2.mkv,e1.mkv" {
		t.Fatalf("title order %s", got)
	}
	if titles := cardTitles(pageFiles(t, byName)); titles != "Aaa,Mid,Zed" {
		t.Fatalf("displayed titles %s", titles)
	}

	byYear := s.libraryPageData(ctx, u, lib, "year", "", "Show/Season 1", false)
	if got := cardNames(pageFiles(t, byYear)); got != "e2.mkv,e10.mkv,e1.mkv" {
		t.Fatalf("released order %s", got)
	}
	byAdded := s.libraryPageData(ctx, u, lib, "added", "", "Show/Season 1", false)
	if got := cardNames(pageFiles(t, byAdded)); got != "e2.mkv,e10.mkv,e1.mkv" {
		t.Fatalf("added order %s", got)
	}
	byEpisode := s.libraryPageData(ctx, u, lib, "episode", "", "Show/Season 1", false)
	if got := cardNames(pageFiles(t, byEpisode)); got != "e1.mkv,e2.mkv,e10.mkv" {
		t.Fatalf("episode order %s", got)
	}
}

func TestEpisodeSortTies(t *testing.T) {
	aired := []nodeCard{
		{Title: "Zed", Rel: "b", Facts: []fileFact{{Label: "Aired", Value: "2011-01-01"}}, Node: db.LibraryNode{EpisodeNum: sql.NullString{String: "10", Valid: true}}},
		{Title: "Aaa", Rel: "a", Facts: []fileFact{{Label: "Aired", Value: "2011-01-01"}}, Node: db.LibraryNode{EpisodeNum: sql.NullString{String: "2", Valid: true}}},
	}
	sortEpisodeCards(aired, "year")
	if aired[0].Title != "Aaa" || aired[1].Title != "Zed" {
		t.Fatalf("same aired date %#v", cardTitles(aired))
	}

	titles := []nodeCard{
		{Title: "Pilot", Rel: "b", Node: db.LibraryNode{EpisodeNum: sql.NullString{String: "10", Valid: true}}},
		{Title: "Pilot", Rel: "a", Node: db.LibraryNode{EpisodeNum: sql.NullString{String: "2", Valid: true}}},
		{Title: "Pilot", Rel: "c", Node: db.LibraryNode{}},
	}
	sortEpisodeCards(titles, "name")
	if cardRels(titles) != "a,b,c" {
		t.Fatalf("same title %#v", cardRels(titles))
	}
}

func TestSeasonSortOption(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var season bytes.Buffer
	if err := tpl.ExecuteTemplate(&season, "library.html", map[string]any{
		"Library": db.Library{ID: 1, Name: "Lib"}, "ItemCount": 3, "Q": "", "Sort": "episode",
		"Path": "Show/Season 1", "Poll": false, "Tree": true, "Season": true,
	}); err != nil {
		t.Fatal(err)
	}
	html := season.String()
	if !strings.Contains(html, `value="episode"`) || !strings.Contains(html, "Episode number") || !strings.Contains(html, "selected") {
		t.Fatalf("episode option: %s", html)
	}

	var title bytes.Buffer
	if err := tpl.ExecuteTemplate(&title, "library.html", map[string]any{
		"Library": db.Library{ID: 1, Name: "Lib"}, "ItemCount": 3, "Q": "", "Sort": "name",
		"Path": "Show", "Poll": false, "Tree": true,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(title.String(), "Episode number") {
		t.Fatal("episode option on a title page")
	}
}

func openSortDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func sortLibrary(t *testing.T, d *db.DB, media string) (*db.User, *db.Library) {
	t.Helper()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureLibraryRoot(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	return u, lib
}

func writeSortFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setDateAdded(t *testing.T, d *db.DB, id int64, at string) {
	t.Helper()
	if _, err := d.SQL.ExecContext(context.Background(), `UPDATE library_nodes SET date_added=? WHERE id=?`, at, id); err != nil {
		t.Fatal(err)
	}
}

func pageNodes(t *testing.T, page map[string]any) []nodeCard {
	t.Helper()
	dirs, ok := page["Nodes"].([]nodeCard)
	if !ok {
		t.Fatalf("nodes %#v", page["Nodes"])
	}
	return dirs
}

func pageFiles(t *testing.T, page map[string]any) []nodeCard {
	t.Helper()
	files, ok := page["Files"].([]nodeCard)
	if !ok {
		t.Fatalf("files %#v", page["Files"])
	}
	return files
}

func cardNames(cards []nodeCard) string {
	parts := make([]string, len(cards))
	for i, card := range cards {
		parts[i] = card.Node.Name
	}
	return strings.Join(parts, ",")
}

func cardTitles(cards []nodeCard) string {
	parts := make([]string, len(cards))
	for i, card := range cards {
		parts[i] = card.Title
	}
	return strings.Join(parts, ",")
}

func cardRels(cards []nodeCard) string {
	parts := make([]string, len(cards))
	for i, card := range cards {
		parts[i] = card.Rel
	}
	return strings.Join(parts, ",")
}
