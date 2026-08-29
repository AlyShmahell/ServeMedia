package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestListLibraryPostersStable(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/m")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
			LibraryID:  lib.ID,
			Kind:       "movie",
			Title:      fmt.Sprintf("Title %d", i),
			Path:       fmt.Sprintf("/m/%d.mkv", i),
			PosterPath: sql.NullString{String: fmt.Sprintf("posters/%d.jpg", i), Valid: true},
			Mtime:      int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 {
		t.Fatalf("got %d posters, want 3: %#v", len(a), a)
	}
	b, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 3 {
		t.Fatalf("second call %d posters: %#v", len(b), b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("collage shuffled: first %#v second %#v", a, b)
		}
	}
}
