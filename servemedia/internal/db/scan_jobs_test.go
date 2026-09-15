package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestRunningScanForLibrary(t *testing.T) {
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
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "/media/a")
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.RunningScanForLibrary(ctx, lib.ID)
	if err != nil || got != nil {
		t.Fatalf("expected nil running job: %#v %v", got, err)
	}

	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateScanJob(ctx, jobID, "running", 40, "Matching 2/5"); err != nil {
		t.Fatal(err)
	}
	got, err = d.RunningScanForLibrary(ctx, lib.ID)
	if err != nil || got == nil || got.ID != jobID || got.Status != "running" {
		t.Fatalf("running: %#v %v", got, err)
	}

	if err := d.UpdateScanJob(ctx, jobID, "done", 100, "Complete"); err != nil {
		t.Fatal(err)
	}
	got, err = d.RunningScanForLibrary(ctx, lib.ID)
	if err != nil || got != nil {
		t.Fatalf("done job must not be returned: %#v %v", got, err)
	}
}
