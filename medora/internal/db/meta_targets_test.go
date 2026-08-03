package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestListLibraryMetaTargetsMissingSkipsArtworkNotStubNFO(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	movies, err := d.CreateLibrary(ctx, u.ID, "Movies", "movies", "/media/m")
	if err != nil {
		t.Fatal(err)
	}

	stubID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: movies.ID, Kind: "movie", Title: "Stub Movie", Path: "/media/m/stub.mp4", Mtime: 1,
		NFOPath: sql.NullString{String: "metadata/movies/stub/movie.nfo", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	withPoster, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: movies.ID, Kind: "movie", Title: "Poster", Path: "/media/m/p.mp4", Mtime: 1,
		NFOPath:    sql.NullString{String: "metadata/movies/p/movie.nfo", Valid: true},
		PosterPath: sql.NullString{String: "metadata/movies/p/poster.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	withMeta, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: movies.ID, Kind: "movie", Title: "Meta", Path: "/media/m/meta.mp4", Mtime: 1,
		NFOPath: sql.NullString{String: "metadata/movies/meta/movie.nfo", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, withMeta, "Meta", 2007, "plot", "", "", "", 0, "omdb", "tt1"); err != nil {
		t.Fatal(err)
	}

	all, err := d.ListLibraryMetaTargets(ctx, movies.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("all targets: got %d want 3", len(all))
	}

	missing, err := d.ListLibraryMetaTargets(ctx, movies.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].ID != stubID || missing[0].Scope != "movie" {
		t.Fatalf("missing targets: %#v want only stub movie %d", missing, stubID)
	}
	_ = withPoster
}

func TestListLibraryMetaTargets_tvIncludesMovies(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	tv, err := d.CreateLibrary(ctx, u.ID, "TV", "tv", "/media/tv")
	if err != nil {
		t.Fatal(err)
	}
	movieID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: tv.ID, Kind: "movie", Title: "TV Library Film", Path: "/media/tv/Film/f.mkv", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: tv.ID, Kind: "show", Title: "Show", Path: "/media/tv/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := d.ListLibraryMetaTargets(ctx, tv.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var movieFound, showFound bool
	for _, mt := range all {
		if mt.Scope == "movie" && mt.ID == movieID {
			movieFound = true
		}
		if mt.Scope == "show" && mt.ID == showID {
			showFound = true
		}
	}
	if !movieFound || !showFound {
		t.Fatalf("tv targets should include movie+show: %#v", all)
	}

	missing, err := d.ListLibraryMetaTargets(ctx, tv.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	movieMissing := false
	for _, mt := range missing {
		if mt.Scope == "movie" && mt.ID == movieID {
			movieMissing = true
		}
	}
	if !movieMissing {
		t.Fatalf("missing-only should include TV library movie: %#v", missing)
	}
}

func TestListLibraryMetaTargetsMissingEpisodes(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	tv, err := d.CreateLibrary(ctx, u.ID, "TV", "tv", "/media/tv")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: tv.ID, Kind: "show", Title: "Show", Path: "/media/tv/Show", Mtime: 1,
		NFOPath: sql.NullString{String: "metadata/tv/Show/tvshow.nfo", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "S1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	stubEp, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: "/media/tv/Show/e1.mp4", Mtime: 1,
		NFOPath: sql.NullString{String: "metadata/tv/Show/S01/e1.nfo", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	withStill, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 2, Path: "/media/tv/Show/e2.mp4", Mtime: 1,
		NFOPath:   sql.NullString{String: "metadata/tv/Show/S01/e2.nfo", Valid: true},
		StillPath: sql.NullString{String: "metadata/tv/Show/S01/e2-thumb.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	metaNoStill, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 3, Path: "/media/tv/Show/e3.mp4", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateEpisodeMeta(ctx, metaNoStill, "E3", "plot", "", "metadata/tv/Show/S01/e3.nfo", "tvmaze", "99"); err != nil {
		t.Fatal(err)
	}

	missing, err := d.ListLibraryMetaTargets(ctx, tv.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	var ids []int64
	for _, mt := range missing {
		scopes = append(scopes, mt.Scope)
		ids = append(ids, mt.ID)
	}
	// show, season, stubEp, metaNoStill — not withStill
	if len(missing) != 4 {
		t.Fatalf("missing: scopes=%v ids=%v", scopes, ids)
	}
	foundStubEp, foundMetaNoStill := false, false
	for _, mt := range missing {
		if mt.Scope == "episode" && mt.ID == stubEp {
			foundStubEp = true
		}
		if mt.Scope == "episode" && mt.ID == metaNoStill {
			foundMetaNoStill = true
		}
		if mt.Scope == "episode" && mt.ID == withStill {
			t.Fatal("episode with still should be skipped")
		}
	}
	if !foundStubEp || !foundMetaNoStill {
		t.Fatalf("expected stub + meta-without-still episodes: %#v", missing)
	}
}
