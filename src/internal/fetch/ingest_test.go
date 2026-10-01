package fetch

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/matchmedia"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustItem(t *testing.T, d *db.DB, id int64) *db.MediaItem {
	t.Helper()
	it, err := d.GetMediaItem(context.Background(), id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	return it
}

func TestIngestNumberedEpisodes(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Numbered Show")
	ep1 := filepath.Join(show, "Season 1", "ep-a.mkv")
	ova := filepath.Join(show, "Specials", "OVA.mkv")
	touch(t, ep1)
	touch(t, ova)
	w := &Worker{DB: d, Store: t.TempDir(), MediaRoot: media}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Numbered Show", Path: show, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.IngestNumberedEpisodes(ctx, showID, []NumberedEpisode{
		{Path: ep1, Season: 1, Episode: 7},
		{Path: ova, Season: 0, Episode: 1},
	}); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	byPath := map[string]db.Episode{}
	for _, ep := range eps {
		byPath[ep.Path] = ep
	}
	if byPath[ep1].EpisodeNumber != 7 {
		t.Fatalf("ep1 %#v", byPath[ep1])
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	seasonByID := map[int64]int{}
	for _, s := range seasons {
		seasonByID[s.ID] = s.SeasonNumber
	}
	if seasonByID[byPath[ova].SeasonID] != 0 || byPath[ova].EpisodeNumber != 1 {
		t.Fatalf("ova %#v seasons %#v", byPath[ova], seasonByID)
	}
}

func TestIngestVideosAsSeason0(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Extras Show")
	a := filepath.Join(show, "trailer.mkv")
	b := filepath.Join(show, "featurette.mkv")
	touch(t, a)
	touch(t, b)
	w := &Worker{DB: d, Store: t.TempDir(), MediaRoot: media}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Extras Show", Path: show, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.IngestVideosAsSeason0(ctx, showID, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	for _, ep := range eps {
		if ep.EpisodeNumber < 1 || ep.EpisodeNumber > 2 {
			t.Fatalf("episode number %#v", ep)
		}
	}
}

func TestEnsureSeason_showRootPoster(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Poster Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	touch(t, filepath.Join(show, "season02-poster.jpg"))
	ep := filepath.Join(show, "02. Cour", "Show - 01.mkv")
	touch(t, ep)

	store := t.TempDir()
	w := &Worker{DB: d, Store: store, MediaRoot: media}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Poster Show", Path: show, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.ingestEpisodeAt(ctx, showID, ep, 2, 1); err != nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons: %#v err=%v", seasons, err)
	}
	se := seasons[0]
	if se.SeasonNumber != 2 {
		t.Fatalf("season number: %d", se.SeasonNumber)
	}
	if !se.PosterPath.Valid || se.PosterPath.String == "" {
		t.Fatal("expected show-root season02-poster.jpg to be copied")
	}
}

func TestDissociateMovieRemovesStoreAndBeside(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	filmDir := filepath.Join(media, "Film Title (2016)")
	vid := filepath.Join(filmDir, "Film Title (2016).mkv")
	touch(t, vid)
	nfoBeside := filepath.Join(filmDir, "movie.nfo")
	posterBeside := filepath.Join(filmDir, "poster.jpg")
	if err := os.WriteFile(nfoBeside, []byte(`<?xml version="1.0"?><movie><title>Wrong</title></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(posterBeside, []byte("poster"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoRel := "metadata/movies/Wrong Hit (2016)/movie.nfo"
	posterRel := "metadata/movies/Wrong Hit (2016)/poster.jpg"
	if err := os.MkdirAll(filepath.Join(store, filepath.Dir(nfoRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, nfoRel), []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, posterRel), []byte("store-poster"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Wrong Hit", Path: vid, Mtime: 1,
		Plot:       sql.NullString{String: "wrong plot", Valid: true},
		PosterPath: sql.NullString{String: posterRel, Valid: true},
		NFOPath:    sql.NullString{String: nfoRel, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, id, "Wrong Hit", 2016, "wrong plot", posterRel, "", nfoRel, 0, "tmdb", "99"); err != nil {
		t.Fatal(err)
	}
	_ = d.SetMatchMediaMatch(ctx, id, "sess", "job", "matched", "")

	w := &Worker{DB: d, Store: store, MediaRoot: media}
	if err := w.DissociateMediaItem(ctx, lib, mustItem(t, d, id), 0); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title != "Film Title" {
		t.Fatalf("title %q", got.Title)
	}
	if got.Plot.Valid || got.PosterPath.Valid || got.NFOPath.Valid || got.MetaID.Valid {
		t.Fatalf("meta still set %#v", got)
	}
	if !got.MatchSkipped() {
		t.Fatalf("status %#v", got.MatchStatus)
	}
	if _, err := os.Stat(nfoBeside); !os.IsNotExist(err) {
		t.Fatalf("beside nfo: %v", err)
	}
	if _, err := os.Stat(posterBeside); !os.IsNotExist(err) {
		t.Fatalf("beside poster: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, nfoRel)); !os.IsNotExist(err) {
		t.Fatalf("store nfo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, posterRel)); !os.IsNotExist(err) {
		t.Fatalf("store poster: %v", err)
	}
}

func TestDissociateMovieFlatDirKeepsSharedPoster(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	movies := filepath.Join(media, "Movies")
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", movies)
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(movies, "Film A (2016).mkv")
	b := filepath.Join(movies, "Film B (2017).mkv")
	touch(t, a)
	touch(t, b)
	shared := filepath.Join(movies, "poster.jpg")
	ownNFO := filepath.Join(movies, "Film A (2016).nfo")
	ownPoster := filepath.Join(movies, "Film A (2016)-poster.jpg")
	if err := os.WriteFile(shared, []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownNFO, []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownPoster, []byte("own"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Wrong", Path: a, Mtime: 1,
		PosterPath: sql.NullString{String: "metadata/movies/Wrong/poster.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store, MediaRoot: media}
	if err := w.DissociateMediaItem(ctx, lib, mustItem(t, d, id), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared poster removed: %v", err)
	}
	if _, err := os.Stat(ownNFO); !os.IsNotExist(err) {
		t.Fatalf("own nfo: %v", err)
	}
	if _, err := os.Stat(ownPoster); !os.IsNotExist(err) {
		t.Fatalf("own poster: %v", err)
	}
}

func TestDissociateShowClearsSeasonPosterFallback(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Sample Show")
	ep := filepath.Join(showDir, "Season 01", "Sample Show S01E01.mkv")
	touch(t, ep)
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "poster.jpg"), []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	seasonPosterRel := "metadata/tv/Wrong Show/S01/poster.jpg"
	if err := os.MkdirAll(filepath.Join(store, filepath.Dir(seasonPosterRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, seasonPosterRel), []byte("season"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Wrong Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, id, 1, "Season 1", seasonPosterRel, "plot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: id, EpisodeNumber: 1, Title: sql.NullString{String: "Pilot", Valid: true},
		Path: ep, Plot: sql.NullString{String: "ep plot", Valid: true},
		StillPath: sql.NullString{String: "metadata/tv/Wrong Show/S01/ep-thumb.jpg", Valid: true},
		NFOPath:   sql.NullString{String: "metadata/tv/Wrong Show/S01/ep.nfo", Valid: true},
		Mtime:     1,
	}); err != nil {
		t.Fatal(err)
	}

	w := &Worker{DB: d, Store: store, MediaRoot: media}
	if err := w.DissociateMediaItem(ctx, lib, mustItem(t, d, id), 0); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title != "Sample Show" {
		t.Fatalf("title %q", got.Title)
	}
	if got.PosterPath.Valid && got.PosterPath.String != "" {
		t.Fatalf("season poster still visible %#v", got.PosterPath)
	}
	if !got.MatchSkipped() {
		t.Fatalf("status %#v", got.MatchStatus)
	}
	seasons, err := d.ListSeasons(ctx, id)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	if seasons[0].PosterPath.Valid && seasons[0].PosterPath.String != "" {
		t.Fatalf("season poster %#v", seasons[0].PosterPath)
	}
	eps, err := d.ListEpisodesByShow(ctx, id)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	if eps[0].StillPath.Valid || eps[0].NFOPath.Valid || eps[0].Plot.Valid {
		t.Fatalf("episode meta %#v", eps[0])
	}
	if _, err := os.Stat(filepath.Join(showDir, "tvshow.nfo")); !os.IsNotExist(err) {
		t.Fatalf("tvshow.nfo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, seasonPosterRel)); !os.IsNotExist(err) {
		t.Fatalf("store season poster: %v", err)
	}
}

func TestReattachMovedPath_ambiguousTitles(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Movies", media)
	if err != nil {
		t.Fatal(err)
	}
	oldA := filepath.Join("/gone-a", "Dup Title", "Dup Title.mkv")
	oldB := filepath.Join("/gone-b", "Dup Title", "Dup Title.mkv")
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Dup Title", Path: oldA, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Dup Title", Path: oldB, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(media, "Dup Title", "Dup Title.mkv")
	touch(t, newPath)
	w := &Worker{DB: d, Store: t.TempDir(), MediaRoot: media}
	if err := w.ReattachMovedPath(ctx, lib, "movie", newPath); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, newPath); got != nil {
		t.Fatal("ambiguous titles must not merge")
	}
}

func TestPruneMissing_deletedMovie(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Movies", media)
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(media, "Keep.mkv")
	gone := filepath.Join(media, "Gone.mkv")
	touch(t, keep)
	touch(t, gone)
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Keep", Path: keep, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Gone", Path: gone, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: t.TempDir(), MediaRoot: media}
	if err := w.PruneMissing(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.GetMediaItem(ctx, id); got != nil {
		t.Fatal("gone movie still present")
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, keep); got == nil {
		t.Fatal("kept movie missing")
	}
}

func TestApplyEpisodeKeepsNFOTitlePilotWhenCatalogEmpty(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "TV", media)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 01", "Sample Show - S01E01.mkv")
	touch(t, epPath)
	nfo := filepath.Join(showDir, "Season 01", "Sample Show - S01E01.nfo")
	if err := os.WriteFile(nfo, []byte(`<?xml version="1.0"?>
<episodedetails>
  <title>Pilot</title>
  <season>1</season>
  <episode>1</episode>
  <plot>Episode plot for the sample pilot.</plot>
</episodedetails>
`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store, MediaRoot: media}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Sample Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.IngestNumberedEpisodes(ctx, showID, []NumberedEpisode{
		{Path: epPath, Season: 1, Episode: 1},
	}); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	if eps[0].Title.String != "Pilot" {
		t.Fatalf("ingest title %q", eps[0].Title.String)
	}
	w.applyEpisode(ctx, show, &seasons[0], &eps[0], &matchmedia.Episode{Title: "Sample Show - S01E01"}, false, "")
	eps, err = d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("after apply %#v %v", eps, err)
	}
	if eps[0].Title.String != "Pilot" {
		t.Fatalf("title %q want Pilot", eps[0].Title.String)
	}
	if !eps[0].Plot.Valid || eps[0].Plot.String != "Episode plot for the sample pilot." {
		t.Fatalf("plot %#v", eps[0].Plot)
	}
}

