package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestListChildNodesSort(t *testing.T) {
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
	zeta, err := d.UpsertNodeChain(ctx, lib.ID, "Zeta", "dir")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := d.UpsertNodeChain(ctx, lib.ID, "Alpha", "dir")
	if err != nil {
		t.Fatal(err)
	}
	mid, err := d.UpsertNodeChain(ctx, lib.ID, "Mid", "dir")
	if err != nil {
		t.Fatal(err)
	}
	title := "Zebra Show"
	year := "2010"
	if err := d.SetNodeDisplay(ctx, zeta, false, &title, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET mm_year=?, date_added=? WHERE id=?`, year, time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339), zeta); err != nil {
		t.Fatal(err)
	}
	title = "Alpha Show"
	year = "2020"
	if err := d.SetNodeDisplay(ctx, alpha, false, &title, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET mm_year=?, date_added=? WHERE id=?`, year, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), alpha); err != nil {
		t.Fatal(err)
	}
	title = "Mid Show"
	year = "2015"
	if err := d.SetNodeDisplay(ctx, mid, false, &title, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET mm_year=?, date_added=? WHERE id=?`, year, time.Now().UTC().Format(time.RFC3339), mid); err != nil {
		t.Fatal(err)
	}

	byName, err := d.ListChildNodes(ctx, lib.ID, "", "name")
	if err != nil || len(byName) != 3 {
		t.Fatalf("name %#v %v", byName, err)
	}
	if byName[0].ID != alpha || byName[1].ID != mid || byName[2].ID != zeta {
		t.Fatalf("name order %d %d %d", byName[0].ID, byName[1].ID, byName[2].ID)
	}

	byYear, err := d.ListChildNodes(ctx, lib.ID, "", "year")
	if err != nil || len(byYear) != 3 {
		t.Fatalf("year %#v %v", byYear, err)
	}
	if byYear[0].ID != alpha || byYear[1].ID != mid || byYear[2].ID != zeta {
		t.Fatalf("year order %d %d %d want alpha mid zeta", byYear[0].ID, byYear[1].ID, byYear[2].ID)
	}

	byDate, err := d.ListChildNodes(ctx, lib.ID, "", "date_added")
	if err != nil || len(byDate) != 3 {
		t.Fatalf("date %#v %v", byDate, err)
	}
	if byDate[0].ID != mid || byDate[1].ID != alpha || byDate[2].ID != zeta {
		t.Fatalf("date order %d %d %d want mid alpha zeta", byDate[0].ID, byDate[1].ID, byDate[2].ID)
	}
}

func TestListChildNodesSortFileTitle(t *testing.T) {
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
	lateName, err := d.UpsertNodeChain(ctx, lib.ID, "Season/zzz.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	earlyName, err := d.UpsertNodeChain(ctx, lib.ID, "Season/aaa.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, lateName, 0, "1", "2", "Alpha", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, earlyName, 0, "1", "1", "Zulu", "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := d.ListChildNodes(ctx, lib.ID, "Season", "name")
	if err != nil || len(got) != 2 {
		t.Fatalf("files %#v %v", got, err)
	}
	if got[0].ID != lateName || got[1].ID != earlyName {
		t.Fatalf("file title order %d %d want alpha then zulu", got[0].ID, got[1].ID)
	}
}

func TestListChildNodesSortPremiered(t *testing.T) {
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
	older, err := d.UpsertNodeChain(ctx, lib.ID, "Older", "dir")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := d.UpsertNodeChain(ctx, lib.ID, "Newer", "dir")
	if err != nil {
		t.Fatal(err)
	}
	yearOnly, err := d.UpsertNodeChain(ctx, lib.ID, "YearOnly", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyNodeTitleFacts(ctx, older, db.NodeTitleFacts{Title: "Older", Year: "2014", Premiered: "2014-12-01"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyNodeTitleFacts(ctx, newer, db.NodeTitleFacts{Title: "Newer", Year: "2014", Premiered: "2014-01-15"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyNodeTitleFacts(ctx, yearOnly, db.NodeTitleFacts{Title: "YearOnly", Year: "2015"}); err != nil {
		t.Fatal(err)
	}
	byYear, err := d.ListChildNodes(ctx, lib.ID, "", "year")
	if err != nil || len(byYear) != 3 {
		t.Fatalf("year %#v %v", byYear, err)
	}
	// 2015-01-01 (year-only) > 2014-12-01 > 2014-01-15
	if byYear[0].ID != yearOnly || byYear[1].ID != older || byYear[2].ID != newer {
		t.Fatalf("premiere order %d %d %d want yearOnly older newer", byYear[0].ID, byYear[1].ID, byYear[2].ID)
	}
}

func TestApplyNodeTitleFactsFromNFOFields(t *testing.T) {
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
	id, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyNodeTitleFacts(ctx, id, db.NodeTitleFacts{
		Title: "Show", Year: "2011", Premiered: "2011-04-06", Plot: "Plot", Rating: 9.1,
	}); err != nil {
		t.Fatal(err)
	}
	var title, year, premiered, plot string
	var rating float64
	err = d.SQL.QueryRowContext(ctx, `SELECT COALESCE(mm_title,''), COALESCE(mm_year,''), COALESCE(mm_premiered,''), COALESCE(plot,''), COALESCE(rating,0)
		FROM library_nodes WHERE id=?`, id).Scan(&title, &year, &premiered, &plot, &rating)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Show" || year != "2011" || premiered != "2011-04-06" || plot != "Plot" || rating < 9 {
		t.Fatalf("%q %q %q %q %v", title, year, premiered, plot, rating)
	}
}
