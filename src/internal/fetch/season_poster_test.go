package fetch

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestApplyShowInheritsShowPosterForEmptySeason(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Barakamon")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	ep2 := filepath.Join(showDir, "Season 2", "S02E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ep2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showPoster := filepath.Join(showDir, "poster.jpg")
	if err := os.WriteFile(showPoster, []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(showDir, "Season 2", "poster.jpg")
	if err := os.WriteFile(own, []byte("s2"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(showDir, "Season 3", "custom.jpg")
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("s3"), 0o644); err != nil {
		t.Fatal(err)
	}
	ep3 := filepath.Join(showDir, "Season 3", "S03E01.mkv")
	if err := os.WriteFile(ep3, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Barakamon", Path: showDir,
		PosterPath: sql.NullString{String: showPoster, Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := d.UpsertSeason(ctx, showID, 2, "Season 2", own, "")
	if err != nil {
		t.Fatal(err)
	}
	s3, err := d.UpsertSeason(ctx, showID, 3, "Season 3", kept, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: s2, ShowID: showID, EpisodeNumber: 1, Path: ep2, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: s3, ShowID: showID, EpisodeNumber: 1, Path: ep3, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	cat := matchmedia.Catalog{
		Provider: "tvmaze", ID: "7906", Title: "Barakamon", Synopsis: "plot",
		Seasons: []matchmedia.Season{{Number: "1", Title: "Season 1"}},
	}
	if err := w.applyShow(ctx, it, matchmedia.Job{}, cat, ""); err != nil {
		t.Fatal(err)
	}
	got1, err := d.GetSeasonByShowNum(ctx, showID, 1)
	if err != nil || got1 == nil || got1.PosterPath.String != showPoster {
		t.Fatalf("season 1 poster %#v %v", got1, err)
	}
	got2, err := d.GetSeasonByShowNum(ctx, showID, 2)
	if err != nil || got2 == nil || got2.PosterPath.String != own {
		t.Fatalf("season 2 poster %#v %v", got2, err)
	}
	got3, err := d.GetSeasonByShowNum(ctx, showID, 3)
	if err != nil || got3 == nil || got3.PosterPath.String != kept {
		t.Fatalf("season 3 poster %#v %v", got3, err)
	}
	if _, err := os.Stat(showPoster); err != nil {
		t.Fatal(err)
	}
}

func TestFillMissingArtInheritsShowPoster(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Barakamon")
	if err := os.MkdirAll(filepath.Join(showDir, "Season 1"), 0o755); err != nil {
		t.Fatal(err)
	}
	showPoster := filepath.Join(showDir, "poster.jpg")
	if err := os.WriteFile(showPoster, []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Barakamon", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, showID, "Barakamon", 2014, "", showPoster, "", "", 0, "tvmaze", "7906"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", ""); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.fillMissingArt(ctx, it, false, ""); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetSeasonByShowNum(ctx, showID, 1)
	if err != nil || got == nil || got.PosterPath.String != showPoster {
		t.Fatalf("season poster %#v %v", got, err)
	}
}

func TestRemoveSeasonPosterKeepsSharedShowFile(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Barakamon")
	seasonDir := filepath.Join(showDir, "Season 1")
	epPath := filepath.Join(seasonDir, "S01E01.mkv")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showPoster := filepath.Join(showDir, "poster.jpg")
	if err := os.WriteFile(showPoster, []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Barakamon", Path: showDir,
		PosterPath: sql.NullString{String: showPoster, Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", showPoster, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		t.Fatal(err)
	}
	season, err := d.GetSeason(ctx, seasonID)
	if err != nil || season == nil {
		t.Fatal(err)
	}
	if err := w.RemoveSeasonPoster(ctx, show, season); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(showPoster); err != nil {
		t.Fatalf("show poster removed: %v", err)
	}
	got, err := d.GetSeason(ctx, seasonID)
	if err != nil || got == nil || got.PosterPath.Valid {
		t.Fatalf("season poster %#v %v", got, err)
	}
}
