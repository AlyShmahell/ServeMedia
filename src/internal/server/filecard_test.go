package server

import (
	"bytes"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/web"
)

func TestStripVideoExt(t *testing.T) {
	if stripVideoExt("Pilot.mkv") != "Pilot" {
		t.Fatal(stripVideoExt("Pilot.mkv"))
	}
	if stripVideoExt("Pilot") != "Pilot" {
		t.Fatal("title")
	}
}

func TestFileTitleOrder(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Show.S01E01.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(dir, "Show.S01E01.nfo")
	body := `<episodedetails><title>Pilot</title><plot>A start</plot><season>1</season><episode>1</episode><runtime>24</runtime><aired>2020-01-02</aired></episodedetails>`
	if err := os.WriteFile(nfo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	node := db.LibraryNode{Name: "Show.S01E01.mkv", RelPath: "Show.S01E01.mkv"}
	title, plot, still, facts := fileCardFields(dir, node, true)
	if title != "Pilot" || plot != "A start" || still != "" || len(facts) != 4 {
		t.Fatalf("nfo title=%q plot=%q still=%q facts=%#v", title, plot, still, facts)
	}
	node.FileTitle = sql.NullString{String: "Stored", Valid: true}
	node.FilePlot = sql.NullString{String: "From the library", Valid: true}
	title, plot, _, _ = fileCardFields(dir, node, true)
	if title != "Pilot" || plot != "From the library" {
		t.Fatalf("nfo title wins, db title=%q plot=%q", title, plot)
	}
	if err := os.WriteFile(nfo, []byte(`<episodedetails><title> </title><plot>A start</plot></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, _ = fileCardFields(dir, node, true)
	if title != "Stored" {
		t.Fatalf("empty nfo title falls back to stored %q", title)
	}
	node.FileTitle = sql.NullString{}
	title, _, _, _ = fileCardFields(dir, node, true)
	if title != "Show.S01E01" {
		t.Fatalf("filename fallback %q", title)
	}
}

func TestFileTitlePreservesNFOExtension(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clip.nfo"), []byte(`<movie><title>Foo.mkv</title></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, _ := fileCardFields(dir, db.LibraryNode{Name: "clip.mkv", RelPath: "clip.mkv"}, false)
	if title != "Foo.mkv" {
		t.Fatalf("nfo title with extension %q", title)
	}
}

func TestFolderWatchCountsDescendants(t *testing.T) {
	files := []db.FileWatch{
		{Rel: "Show/Season 1/a.mkv", Done: true},
		{Rel: "Show/Season 1/b.mkv", Done: false},
		{Rel: "Show/Season 2/c.mkv", Done: true},
		{Rel: "Loose.mkv", Done: false},
	}
	watched, total, pct := folderWatch(files, "Show/Season 1")
	if watched != 1 || total != 2 || pct != 50 {
		t.Fatalf("season %d/%d %d", watched, total, pct)
	}
	watched, total, pct = folderWatch(files, "Show")
	if watched != 2 || total != 3 || pct != 66 {
		t.Fatalf("show %d/%d %d", watched, total, pct)
	}
	watched, total, _ = folderWatch(files, "")
	if watched != 2 || total != 4 {
		t.Fatalf("library %d/%d", watched, total)
	}
}

func TestFileRowTemplate(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	err = tpl.ExecuteTemplate(&buf, "partials/items.html", map[string]any{
		"Tree": true,
		"Nodes": []nodeCard{{
			LibraryID: 3, Rel: "Show", Href: "/libraries/3/tree?path=Show", Title: "Show",
			Watched: 1, Total: 2, WatchPct: 50,
			MediaID: 7, MatchStatus: "manual", EditURL: "/hx/media/7/meta",
			Node: db.LibraryNode{ID: 4, PosterPath: sql.NullString{String: "metadata/tv/Show/poster.jpg", Valid: true}},
		}},
		"Files": []nodeCard{{
			LibraryID: 3, Rel: "Show/Pilot.mkv", Href: "/play/file/9", Title: "Pilot",
			Plot: "A start", Still: "metadata/tv/Show/S01/Pilot-thumb.jpg", ProgressPct: 40,
			EditURL: "/hx/episodes/9/meta", UploadURL: "/hx/nodes/9/still", GenerateURL: "/hx/nodes/9/still/generate",
			Facts: []fileFact{{Label: "Season", Value: "1"}, {Label: "Aired", Value: "2020-01-02"}},
			Node:  db.LibraryNode{ID: 9, MetaID: sql.NullString{String: "22", Valid: true}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		"file-card", "Pilot", "A start", "Season", "2020-01-02",
		"poster-progress-watch", "width:50%", "width:40%", "openEntryScanModal(",
		`data-media-id="7"`, "Pilot-thumb.jpg",
		`data-generate-url="/hx/nodes/9/still/generate"`, `data-role="still"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, `card-action-label">Edit`) || strings.Contains(html, `data-role="edit"`) || strings.Contains(html, "data-edit-url=") {
		t.Fatalf("edit control outside hero: %s", html)
	}
	if strings.Contains(html, "watched ") || strings.Contains(html, "openScanModal(") {
		t.Fatalf("library scan or watch chip on item: %s", html)
	}
	if strings.Contains(html, "file-card-title\">Pilot.mkv") || strings.Contains(html, ">Show.mkv<") {
		t.Fatalf("extension visible: %s", html)
	}
	if strings.Contains(html, "data-mid=") {
		t.Fatalf("card title should wrap, not ellipsize: %s", html)
	}
	if !strings.Contains(html, `class="card"`) {
		t.Fatalf("root folder tile: %s", html)
	}

	buf.Reset()
	err = tpl.ExecuteTemplate(&buf, "partials/items.html", map[string]any{
		"Tree": true,
		"Path": "Show",
		"Nodes": []nodeCard{{
			LibraryID: 3, Rel: "Show/Season 1", Href: "/libraries/3/tree?path=Show/Season%201", Title: "Season 1",
			EditURL: "/hx/shows/7/seasons/1/meta", Plot: "",
			Node: db.LibraryNode{ID: 5, PosterPath: sql.NullString{String: "metadata/tv/Show/S01/poster.jpg", Valid: true}},
		}},
		"Files": []nodeCard{{
			LibraryID: 3, Rel: "Show/Season 1/Pilot.mkv", Href: "/play/file/9", Title: "Pilot",
			Plot: "", EditURL: "/hx/episodes/9/meta",
			UploadURL: "/hx/nodes/9/still", GenerateURL: "/hx/nodes/9/still/generate",
			Node: db.LibraryNode{ID: 9},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	nested := buf.String()
	if strings.Contains(nested, `class="card"`) {
		t.Fatalf("nested dir should be full width: %s", nested)
	}
	if !strings.Contains(nested, "file-card") || !strings.Contains(nested, "Season 1") || !strings.Contains(nested, "Pilot") {
		t.Fatalf("nested cards: %s", nested)
	}
	if strings.Contains(nested, "data-mid=") {
		t.Fatalf("nested title should wrap, not ellipsize: %s", nested)
	}
	if strings.Contains(nested, `data-edit-field="plot"`) || strings.Contains(nested, `data-role="edit"`) {
		t.Fatalf("edit control outside hero: %s", nested)
	}
}

func TestFolderDisplayTitle(t *testing.T) {
	dir := t.TempDir()
	show := filepath.Join(dir, "Show Name")
	season := filepath.Join(show, "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "tvshow.nfo"), []byte(`<tvshow><title>Steins;Gate</title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(season, "season.nfo"), []byte(`<season><title>Season One</title></season>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mm := sql.NullString{String: "Stored Show", Valid: true}
	year := sql.NullString{String: "2011", Valid: true}
	if got := folderDisplayTitle(show, "Show Name", mm, year, 0); got != "Steins;Gate" {
		t.Fatalf("tvshow nfo title %q", got)
	}
	if got := folderDisplayTitle(season, "Season 01", mm, year, 0); got != "Season One" {
		t.Fatalf("season nfo title %q", got)
	}
	bare := filepath.Join(dir, "Bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, "tvshow.nfo"), []byte(`<tvshow><title> </title></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := folderDisplayTitle(bare, "Bare", mm, year, 0); got != "Stored Show (2011)" {
		t.Fatalf("empty nfo title %q", got)
	}
	if err := os.Remove(filepath.Join(bare, "tvshow.nfo")); err != nil {
		t.Fatal(err)
	}
	if got := folderDisplayTitle(bare, "Bare", sql.NullString{}, sql.NullString{}, 0); got != "Bare" {
		t.Fatalf("directory name %q", got)
	}
	movie := filepath.Join(dir, "Film (1999)")
	if err := os.MkdirAll(movie, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movie, "movie.nfo"), []byte(`<movie><title>The Film</title><year>1999</year></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := folderDisplayTitle(movie, "Film (1999)", mm, year, 0); got != "The Film" {
		t.Fatalf("movie nfo title %q", got)
	}
	numbered := filepath.Join(dir, "Season 02")
	if err := os.MkdirAll(numbered, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(numbered, "season02.nfo"), []byte(`<season><title>Second</title></season>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := folderDisplayTitle(numbered, "Season 02", sql.NullString{}, sql.NullString{}, 0); got != "Second" {
		t.Fatalf("seasonNN nfo title %q", got)
	}
}

func TestMovieFileOmitsEpisodeFact(t *testing.T) {
	node := db.LibraryNode{
		Name: "Film 2.mkv", RelPath: "Film 2.mkv",
		SeasonNum:  sql.NullString{String: "1", Valid: true},
		EpisodeNum: sql.NullString{String: "2", Valid: true},
	}
	_, _, _, facts := fileCardFields("/m", node, false)
	for _, f := range facts {
		if f.Label == "Season" || f.Label == "Episode" {
			t.Fatalf("movie facts %#v", facts)
		}
	}
	_, _, _, facts = fileCardFields("/m", node, true)
	if len(facts) != 2 {
		t.Fatalf("show facts %#v", facts)
	}
}

func TestFileCardOmitsSeasonZero(t *testing.T) {
	node := db.LibraryNode{
		Name: "NCOP.mkv", RelPath: "Specials/NCOP.mkv",
		SeasonNum:  sql.NullString{String: "0", Valid: true},
		EpisodeNum: sql.NullString{String: "1", Valid: true},
	}
	_, _, _, facts := fileCardFields("/m", node, true)
	for _, f := range facts {
		if f.Label == "Season" || f.Label == "Episode" {
			t.Fatalf("season 0 facts %#v", facts)
		}
	}
}
