package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestPerUserLibraries(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	a, err := d.CreateUser(ctx, "alice", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.CreateUser(ctx, "bob", "y", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	la, err := d.CreateLibrary(ctx, a.ID, "Movies", "movies", "/media/movies")
	if err != nil {
		t.Fatal(err)
	}
	lb, err := d.CreateLibrary(ctx, b.ID, "Movies", "movies", "/media/movies")
	if err != nil {
		t.Fatal(err)
	}
	if la.ID == lb.ID {
		t.Fatal("expected distinct libraries")
	}

	alist, err := d.ListLibraries(ctx, a.ID)
	if err != nil || len(alist) != 1 || alist[0].ID != la.ID {
		t.Fatalf("alice libs: %#v %v", alist, err)
	}
	blist, err := d.ListLibraries(ctx, b.ID)
	if err != nil || len(blist) != 1 || blist[0].ID != lb.ID {
		t.Fatalf("bob libs: %#v %v", blist, err)
	}

	got, err := d.GetLibrary(ctx, b.ID, la.ID)
	if err != nil || got != nil {
		t.Fatalf("bob should not own alice library: %#v %v", got, err)
	}

	all, err := d.ListAllLibraries(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("all libs: %#v %v", all, err)
	}
}
