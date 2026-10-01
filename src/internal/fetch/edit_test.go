package fetch

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func tinyPNG() []byte {
	return []byte("\x89PNG\r\n\x1a\n" + "0123456789ab")
}

func testWorker(t *testing.T) (ctx context.Context, store, media string, d *db.DB, lib *db.Library, w *Worker) {
	t.Helper()
	ctx = context.Background()
	store = t.TempDir()
	media = t.TempDir()
	var err error
	d, err = db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err = d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	w = &Worker{DB: d, Store: store}
	return ctx, store, media, d, lib, w
}

func TestEditMovieMetaWritesStoreAndBeside(t *testing.T) {
	ctx, store, media, d, lib, w := testWorker(t)
	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film Title", Path: filmPath, Mtime: 1,
		Plot: sql.NullString{String: "old plot", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	beside := filepath.Join(filmDir, "movie.nfo")
	if err := metadata.WriteMovieNFO(beside, metadata.MovieNFO{
		Title: "Film Title", OriginalTitle: "Kept Original", Plot: "old plot",
		Actor: []metadata.NFOActor{{Name: "Star"}},
	}); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	title, plot := "New Title", "New plot"
	if err := w.EditMediaMeta(ctx, it, &title, &plot); err != nil {
		t.Fatal(err)
	}
	it, err = d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "New Title" {
		t.Fatalf("db title %q", it.Title)
	}
	if !it.Plot.Valid || it.Plot.String != "New plot" {
		t.Fatalf("db plot %#v", it.Plot)
	}
	if !it.NFOPath.Valid {
		t.Fatal("nfo_path")
	}
	storeNFO, err := metadata.ReadMovieNFO(filepath.Join(store, it.NFOPath.String))
	if err != nil {
		t.Fatal(err)
	}
	if storeNFO.Title != "New Title" || storeNFO.Plot != "New plot" {
		t.Fatalf("store %#v", storeNFO)
	}
	if storeNFO.OriginalTitle != "Kept Original" || len(storeNFO.Actor) != 1 || storeNFO.Actor[0].Name != "Star" {
		t.Fatalf("merge lost fields %#v", storeNFO)
	}
	besideNFO, err := metadata.ReadMovieNFO(beside)
	if err != nil {
		t.Fatal(err)
	}
	if besideNFO.Title != "New Title" || besideNFO.Plot != "New plot" {
		t.Fatalf("beside %#v", besideNFO)
	}

	empty := ""
	if err := w.EditMediaMeta(ctx, it, nil, &empty); err != nil {
		t.Fatal(err)
	}
	it, err = d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Plot.String != "" {
		t.Fatalf("cleared plot %q", it.Plot.String)
	}
	besideNFO, err = metadata.ReadMovieNFO(beside)
	if err != nil {
		t.Fatal(err)
	}
	if besideNFO.Plot != "" {
		t.Fatalf("beside plot %q", besideNFO.Plot)
	}
}

func TestEditMoviePosterWritesStoreAndBeside(t *testing.T) {
	ctx, store, media, d, lib, w := testWorker(t)
	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film Title", Path: filmPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := w.EditMediaPoster(ctx, it, tinyPNG())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(store, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, tinyPNG()) {
		t.Fatal("store poster")
	}
	beside, err := os.ReadFile(filepath.Join(filmDir, "poster.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beside, tinyPNG()) {
		t.Fatal("beside poster")
	}
}

func TestEditShowSeasonEpisodeMetaAndArt(t *testing.T) {
	ctx, store, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Sample Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "season plot")
	if err != nil {
		t.Fatal(err)
	}
	epID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1,
		Title: sql.NullString{String: "Pilot", Valid: true},
		Plot:  sql.NullString{String: "ep plot", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	stitle, splot := "Renamed Show", "Show synopsis"
	if err := w.EditMediaMeta(ctx, show, &stitle, &splot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(showDir, "tvshow.nfo")); err != nil {
		t.Fatal(err)
	}
	show, _ = d.GetMediaItem(ctx, showID)
	if _, err := w.EditMediaPoster(ctx, show, tinyPNG()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(showDir, "poster.png")); err != nil {
		t.Fatal(err)
	}

	season, err := d.GetSeason(ctx, seasonID)
	if err != nil || season == nil {
		t.Fatal(err)
	}
	t1, p1 := "Season One", "new season plot"
	if err := w.EditSeasonMeta(ctx, show, season, &t1, &p1); err != nil {
		t.Fatal(err)
	}
	seasonNFO := filepath.Join(showDir, "Season 1", "season01.nfo")
	sn, err := metadata.ReadSeasonNFO(seasonNFO)
	if err != nil {
		t.Fatal(err)
	}
	if sn.Title != "Season One" || sn.Plot != "new season plot" {
		t.Fatalf("season nfo %#v", sn)
	}
	if _, err := w.EditSeasonPoster(ctx, show, season, tinyPNG()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(showDir, "Season 1", "poster.png")); err != nil {
		t.Fatal(err)
	}

	ep, err := d.GetEpisode(ctx, epID)
	if err != nil || ep == nil {
		t.Fatal(err)
	}
	et, epPlot := "Pilot Two", "new ep plot"
	if err := w.EditEpisodeMeta(ctx, show, season, ep, &et, &epPlot); err != nil {
		t.Fatal(err)
	}
	en, err := metadata.ReadEpisodeNFO(filepath.Join(showDir, "Season 1", "S01E01.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if en.Title != "Pilot Two" || en.Plot != "new ep plot" {
		t.Fatalf("episode nfo %#v", en)
	}
	if _, err := w.EditEpisodePoster(ctx, show, season, ep, tinyPNG()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(showDir, "Season 1", "S01E01-thumb.png")); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if err := w.EditEpisodeMeta(ctx, show, season, ep, nil, &empty); err != nil {
		t.Fatal(err)
	}
	ep, _ = d.GetEpisode(ctx, epID)
	if ep.Plot.String != "" {
		t.Fatalf("episode plot %q", ep.Plot.String)
	}

	storeShow, err := metadata.ReadTVShowNFO(filepath.Join(store, show.NFOPath.String))
	if err != nil {
		t.Fatal(err)
	}
	if storeShow.Title != "Renamed Show" {
		t.Fatalf("store show %q", storeShow.Title)
	}
}

func TestRemoveNestedMoviePosterLeavesShowPoster(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "KonoSuba")
	moviePath := filepath.Join(showDir, "Legend of Crimson.mkv")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moviePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte("<tvshow/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	showPoster := filepath.Join(showDir, "poster.jpg")
	if err := os.WriteFile(showPoster, []byte("show-art"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "KonoSuba", Path: showDir, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Legend of Crimson", Path: moviePath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.EditMediaPoster(ctx, it, tinyPNG()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(showPoster)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "show-art" {
		t.Fatalf("upload overwrote show poster: %q", got)
	}
	named := filepath.Join(showDir, "Legend of Crimson-poster.png")
	if _, err := os.Stat(named); err != nil {
		t.Fatal(err)
	}
	it, _ = d.GetMediaItem(ctx, id)
	if err := w.RemoveMediaPoster(ctx, it); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(showPoster)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "show-art" {
		t.Fatalf("remove overwrote show poster: %q", got)
	}
	if _, err := os.Stat(named); !os.IsNotExist(err) {
		t.Fatalf("named sidecar remains: %v", err)
	}
	it, _ = d.GetMediaItem(ctx, id)
	if it.PosterPath.Valid && it.PosterPath.String != "" {
		t.Fatalf("db poster %#v", it.PosterPath)
	}
}
