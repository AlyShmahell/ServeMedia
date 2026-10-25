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

func TestChipsFromShowAndMovieNFO(t *testing.T) {
	dir := t.TempDir()
	show := filepath.Join(dir, "tvshow.nfo")
	body := `<?xml version="1.0"?><tvshow><title>Show</title><genre>Fantasy</genre><genre>Adventure</genre><rating>8.2</rating><runtime>24</runtime><language>ja</language><premiered>2024-07-07</premiered><ended>2024-09-22</ended><country>Japan</country><network>AT-X</network></tvshow>`
	if err := os.WriteFile(show, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	chips := chipsFromNFO(show)
	if len(chips) != 7 {
		t.Fatalf("chips %#v", chips)
	}
	if chips[0].Kind != "genre" || chips[0].Value != "Fantasy, Adventure" {
		t.Fatalf("%#v", chips[0])
	}
	var kinds []string
	for _, c := range chips {
		kinds = append(kinds, c.Kind+":"+c.Value)
	}
	joined := strings.Join(kinds, " ")
	for _, want := range []string{"rating:8.2", "runtime:24 min", "language:ja", "dates:2024-07-07 – 2024-09-22", "country:Japan", "network:AT-X"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if dateSpan("2016-07-16", "") != "2016-07-16" || dateSpan("", "2019-05-19") != "2019-05-19" {
		t.Fatal("single date")
	}
	ep := filepath.Join(dir, "S01E01.nfo")
	if err := os.WriteFile(ep, []byte(`<episodedetails><title>Pilot</title><plot>x</plot></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if chipsFromNFO(ep) != nil {
		t.Fatal("episode nfo is not a hero")
	}
	empty := filepath.Join(dir, "movie.nfo")
	if err := os.WriteFile(empty, []byte(`<movie><title>Film</title><plot>only</plot></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if chipsFromNFO(empty) != nil {
		t.Fatal("movie without facts")
	}
}

func TestLibraryCardBackfillsNFORating(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/m")
	if err != nil {
		t.Fatal(err)
	}
	highRel := "metadata/movies/High/movie.nfo"
	plainRel := "metadata/movies/Plain/movie.nfo"
	if err := os.MkdirAll(filepath.Join(store, filepath.Dir(highRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store, filepath.Dir(plainRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	highBody := `<?xml version="1.0"?><movie><title>High</title><rating>9.4</rating></movie>`
	if err := os.WriteFile(filepath.Join(store, highRel), []byte(highBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, plainRel), []byte(`<movie><title>Plain</title></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	highID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "High", Path: "/m/High.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/High/poster.jpg", Valid: true},
		NFOPath:    sql.NullString{String: highRel, Valid: true},
		Mtime:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	plainID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Plain", Path: "/m/Plain.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/Plain/poster.jpg", Valid: true},
		NFOPath:    sql.NullString{String: plainRel, Valid: true},
		Mtime:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	missingID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Missing", Path: "/m/Missing.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/Missing/poster.jpg", Valid: true},
		NFOPath:    sql.NullString{String: "metadata/movies/Missing/movie.nfo", Valid: true},
		Mtime:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET date_added=? WHERE id=?`, "2010-01-01T00:00:00Z", highID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET date_added=? WHERE id=?`, "2024-01-01T00:00:00Z", plainID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET date_added=? WHERE id=?`, "2020-01-01T00:00:00Z", missingID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Store.Path = store
	s := &Server{Cfg: &cfg, DB: d}
	card := s.libraryCardData(ctx, 0, lib, nil)
	want := []string{"metadata/movies/High/poster.jpg", "metadata/movies/Plain/poster.jpg", "metadata/movies/Missing/poster.jpg"}
	if len(card.Posters) != len(want) {
		t.Fatalf("posters %#v", card.Posters)
	}
	for i := range want {
		if card.Posters[i] != want[i] {
			t.Fatalf("posters %#v want %#v", card.Posters, want)
		}
	}
	var high, plain float64
	var missing sql.NullFloat64
	if err := d.SQL.QueryRowContext(ctx, `SELECT rating FROM media_items WHERE id=?`, highID).Scan(&high); err != nil || high != 9.4 {
		t.Fatalf("high rating %v %v", high, err)
	}
	if err := d.SQL.QueryRowContext(ctx, `SELECT rating FROM media_items WHERE id=?`, plainID).Scan(&plain); err != nil || plain != 0 {
		t.Fatalf("plain rating %v %v", plain, err)
	}
	if err := d.SQL.QueryRowContext(ctx, `SELECT rating FROM media_items WHERE id=?`, missingID).Scan(&missing); err != nil || missing.Valid {
		t.Fatalf("missing rating %#v %v", missing, err)
	}
}

func TestFolderHeroRendersChips(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	card := nodeCard{
		Title: "Show",
		Plot:  "A plot",
		Chips: []heroChip{
			{Kind: "genre", Value: "Fantasy", Title: "Genre", Tone: 0},
			{Kind: "rating", Label: "Rating", Value: "8.2", Title: "Rating"},
			{Kind: "runtime", Label: "Runtime", Value: "24 min", Title: "Runtime"},
		},
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/folder_hero.html", card); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, "hero-facts") || !strings.Contains(html, "hero-chip-genre") || !strings.Contains(html, "Fantasy") || !strings.Contains(html, "8.2") || !strings.Contains(html, "24 min") {
		t.Fatalf("%s", html)
	}
	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/folder_hero.html", nodeCard{Title: "Plain"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "hero-facts") {
		t.Fatalf("empty hero %s", buf.String())
	}
	if strings.Contains(html, "data-mid=") || strings.Contains(buf.String(), "data-mid=") {
		t.Fatalf("hero title should stay whole")
	}
}
