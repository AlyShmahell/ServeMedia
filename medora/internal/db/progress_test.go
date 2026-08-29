package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestWatchProgressPctHelpers(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Movies", "/media/m")
	if err != nil {
		t.Fatal(err)
	}
	movieID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/media/m/f.mp4", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchMovie(ctx, u.ID, movieID, 30, 100, false); err != nil {
		t.Fatal(err)
	}
	m, err := d.WatchProgressPctByMovieIDs(ctx, u.ID, []int64{movieID, 999})
	if err != nil {
		t.Fatal(err)
	}
	if m[movieID] != 30 || m[999] != 0 {
		t.Fatalf("movie pct: %#v", m)
	}

	tv, err := d.CreateLibrary(ctx, u.ID, "TV", "/media/tv")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: tv.ID, Kind: "show", Title: "Show", Path: "/media/tv/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "S1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ep1, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: "/media/tv/Show/e1.mp4", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep2, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 2, Path: "/media/tv/Show/e2.mp4", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchEpisode(ctx, u.ID, ep1, 50, 100, false); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchEpisode(ctx, u.ID, ep2, 0, 100, true); err != nil {
		t.Fatal(err)
	}
	epPct, err := d.WatchProgressPctByEpisodeIDs(ctx, u.ID, []int64{ep1, ep2})
	if err != nil {
		t.Fatal(err)
	}
	if epPct[ep1] != 50 || epPct[ep2] != 100 {
		t.Fatalf("episode pct: %#v", epPct)
	}
	seasonPct, err := d.SeasonWatchProgressPct(ctx, u.ID, seasonID)
	if err != nil {
		t.Fatal(err)
	}
	// (50+100)/2 = 75
	if seasonPct != 75 {
		t.Fatalf("season pct %d", seasonPct)
	}
	showPct, err := d.ShowWatchProgressPct(ctx, u.ID, showID)
	if err != nil {
		t.Fatal(err)
	}
	if showPct != 75 {
		t.Fatalf("show pct %d", showPct)
	}
}
