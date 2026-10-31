package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestSyncNodeFromTitleNFO(t *testing.T) {
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
	root := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", root)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(root, "Cool Show")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<tvshow><title>Cool Show</title><year>2019</year><premiered>2019-03-01</premiered><plot>Hello</plot></tvshow>`
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertNodeChain(ctx, lib.ID, "Cool Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := syncNodeFromTitleNFO(ctx, d, id, showDir); err != nil {
		t.Fatal(err)
	}
	var year, premiered string
	err = d.SQL.QueryRowContext(ctx, `SELECT COALESCE(mm_year,''), COALESCE(mm_premiered,'') FROM library_nodes WHERE id=?`, id).
		Scan(&year, &premiered)
	if err != nil {
		t.Fatal(err)
	}
	if year != "2019" || premiered != "2019-03-01" {
		t.Fatalf("got %q %q", year, premiered)
	}
	nodes, err := d.ListChildNodes(ctx, lib.ID, "", "year")
	if err != nil || len(nodes) != 1 || nodes[0].ID != id {
		t.Fatalf("%#v %v", nodes, err)
	}
}

func TestChangesScanRefreshesPremieredWhenFilesUnchanged(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Cool Show")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(showDir, "ep.mkv")
	touch(t, video)
	showID, err := d.UpsertNodeChain(ctx, lib.ID, "Cool Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Cool Show/ep.mkv", "file"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET meta_id=? WHERE id=?`, "42", showID); err != nil {
		t.Fatal(err)
	}
	body := `<tvshow><title>Cool Show</title><year>2019</year><premiered>2019-03-01</premiered></tvshow>`
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	job := matchmedia.Job{Path: showDir, Files: []matchmedia.JobFile{{Path: video}}}
	if err := w.recordTree(ctx, lib, job, Opts{ScanMode: "changes"}); err != nil {
		t.Fatal(err)
	}
	var premiered string
	err = d.SQL.QueryRowContext(ctx, `SELECT COALESCE(mm_premiered,'') FROM library_nodes WHERE id=?`, showID).Scan(&premiered)
	if err != nil {
		t.Fatal(err)
	}
	if premiered != "2019-03-01" {
		t.Fatalf("premiered %q", premiered)
	}
}
