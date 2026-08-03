package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestResolveAndSavePlaybackPrefs(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "u1", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "TV", "tv", "/media/tv")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/media/tv/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	vol := 0.4
	muted := true
	height := 720
	alang := "jpn"
	off := ""
	if err := d.SavePlaybackPrefs(ctx, u.ID, &db.PlaybackScopeIDs{
		LibraryID: lib.ID, ItemScope: db.PrefsScopeShow, ItemID: showID,
	}, db.PrefsPatch{
		Volume: &vol, Muted: &muted, Height: &height, AudioLang: &alang, SubtitleLang: &off,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := d.ResolvePlaybackPrefs(ctx, u.ID, &db.PlaybackScopeIDs{
		LibraryID: lib.ID, ItemScope: db.PrefsScopeShow, ItemID: showID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasVolume || got.Volume != 0.4 || !got.HasMuted || !got.Muted {
		t.Fatalf("volume/mute %#v", got)
	}
	if got.Height == nil || *got.Height != 720 || got.AudioLang != "jpn" {
		t.Fatalf("height/audio %#v", got)
	}
	if got.SubtitleLang == nil || *got.SubtitleLang != "" {
		t.Fatalf("subtitle %#v", got.SubtitleLang)
	}

	// Different show in same library inherits user/library prefs from Save.
	otherShow, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Other", Path: "/media/tv/Other", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	got2, err := d.ResolvePlaybackPrefs(ctx, u.ID, &db.PlaybackScopeIDs{
		LibraryID: lib.ID, ItemScope: db.PrefsScopeShow, ItemID: otherShow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2.AudioLang != "jpn" || got2.Height == nil || *got2.Height != 720 {
		t.Fatalf("inherited %#v", got2)
	}
}

func TestItemPrefsOverrideUser(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "u1", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "TV", "tv", "/media/tv")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/media/tv/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	eng := "eng"
	jpn := "jpn"
	h720 := 720
	h480 := 480
	scope := &db.PlaybackScopeIDs{LibraryID: lib.ID, ItemScope: db.PrefsScopeShow, ItemID: showID}
	if err := d.SavePlaybackPrefs(ctx, u.ID, scope, db.PrefsPatch{Height: &h720, AudioLang: &eng}); err != nil {
		t.Fatal(err)
	}
	if err := d.SavePlaybackPrefs(ctx, u.ID, scope, db.PrefsPatch{Height: &h480, AudioLang: &jpn}); err != nil {
		t.Fatal(err)
	}
	got, err := d.ResolvePlaybackPrefs(ctx, u.ID, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioLang != "jpn" || got.Height == nil || *got.Height != 480 {
		t.Fatalf("got %#v", got)
	}
}
