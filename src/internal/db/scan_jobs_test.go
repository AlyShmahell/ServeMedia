package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/internal/db"
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

func TestFailRunningScanJobs(t *testing.T) {
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
	libA, err := d.CreateLibrary(ctx, u.ID, "A", "/media/a")
	if err != nil {
		t.Fatal(err)
	}
	libB, err := d.CreateLibrary(ctx, u.ID, "B", "/media/b")
	if err != nil {
		t.Fatal(err)
	}
	jobA, err := d.CreateScanJob(ctx, libA.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := d.CreateScanJob(ctx, libB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.FailRunningScanJobs(ctx, "Interrupted (server restarted)"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{jobA, jobB} {
		j, err := d.GetScanJob(ctx, id)
		if err != nil || j == nil || j.Status != "error" || !j.FinishedAt.Valid {
			t.Fatalf("job %d: %#v %v", id, j, err)
		}
		if j.Message.String != "Interrupted (server restarted)" {
			t.Fatalf("job %d message=%q", id, j.Message.String)
		}
	}
	if got, _ := d.RunningScanForLibrary(ctx, libA.ID); got != nil {
		t.Fatalf("expected no running job: %#v", got)
	}
}

func TestFailRunningScanJobsForLibrary(t *testing.T) {
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
	libA, err := d.CreateLibrary(ctx, u.ID, "A", "/media/a")
	if err != nil {
		t.Fatal(err)
	}
	libB, err := d.CreateLibrary(ctx, u.ID, "B", "/media/b")
	if err != nil {
		t.Fatal(err)
	}
	jobA, err := d.CreateScanJob(ctx, libA.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := d.CreateScanJob(ctx, libB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.FailRunningScanJobsForLibrary(ctx, libA.ID, "Superseded by a new scan"); err != nil {
		t.Fatal(err)
	}
	a, err := d.GetScanJob(ctx, jobA)
	if err != nil || a == nil || a.Status != "error" {
		t.Fatalf("libA job: %#v %v", a, err)
	}
	b, err := d.GetScanJob(ctx, jobB)
	if err != nil || b == nil || b.Status != "running" {
		t.Fatalf("libB job should still run: %#v %v", b, err)
	}
}

func TestCreateScanJobSupersedesRunning(t *testing.T) {
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
	oldID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateScanJob(ctx, oldID, "running", 11, "Episode 136/4285"); err != nil {
		t.Fatal(err)
	}
	newID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newID == oldID {
		t.Fatal("expected a new job id")
	}
	old, err := d.GetScanJob(ctx, oldID)
	if err != nil || old == nil || old.Status != "error" || old.Message.String != "Superseded by a new scan" {
		t.Fatalf("old job: %#v %v", old, err)
	}
	got, err := d.RunningScanForLibrary(ctx, lib.ID)
	if err != nil || got == nil || got.ID != newID {
		t.Fatalf("running should be new job: %#v %v", got, err)
	}
}
