package fetch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestPipelineShowsTreeThenFetchesDuringMatch(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := &overlapMeta{
		db: d, libID: lib.ID, scanID: scanID,
		filmDir: filmDir, filmPath: filmPath, showDir: showDir, epPath: epPath,
	}
	w := &Worker{DB: d, Store: store, Meta: meta}
	if err := w.MatchPath(ctx, lib, media, nil, Opts{ScanJobID: scanID, ScanMode: "rescan"}); err != nil {
		t.Fatal(err)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, filmPath)
	if err != nil || film == nil || film.Title != "Film Title" {
		t.Fatalf("film %#v %v", film, err)
	}
	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.Title != "Sample Show" {
		t.Fatalf("show %#v %v", show, err)
	}
	id, err := d.NodeIDByRel(ctx, lib.ID, "Film Title")
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	node, err := d.GetLibraryNode(ctx, id)
	if err != nil || node == nil || node.MMTitle.String != "Film Title" {
		t.Fatalf("node %#v %v", node, err)
	}
}

type overlapMeta struct {
	db       *db.DB
	libID    int64
	scanID   int64
	filmDir  string
	filmPath string
	showDir  string
	epPath   string
}

func (m *overlapMeta) Status() (matchmedia.Status, error) {
	return matchmedia.Status{Ready: true}, nil
}

func (m *overlapMeta) Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]matchmedia.Job, error) {
	if report != nil {
		report(1, 1, "scan 1/1 Film Title")
	}
	return []matchmedia.Job{{
		Path: m.filmDir, Title: "Film Title", Kind: "movie", Status: "pending", Source: "scan",
		Files: []matchmedia.JobFile{{Path: m.filmPath}},
	}}, nil
}

func (m *overlapMeta) Match(ctx context.Context, jobs []matchmedia.Job, report func(done, total int, name string), onJob func(matchmedia.Job)) ([]matchmedia.Job, error) {
	id, err := m.db.NodeIDByRel(ctx, m.libID, "Film Title")
	if err != nil || id == 0 {
		return nil, fmt.Errorf("directory missing before match")
	}
	node, err := m.db.GetLibraryNode(ctx, id)
	if err != nil || node == nil {
		return nil, fmt.Errorf("node: %v", err)
	}
	if strings.TrimSpace(node.MMTitle.String) != "" {
		return nil, fmt.Errorf("title filled before fetch: %q", node.MMTitle.String)
	}
	if report != nil {
		report(1, 2, "match 1/2 matched:1 manual:0 unmatched:0 error:0 Film Title")
	}
	movie := jobs[0]
	movie.Status = "matched"
	movie.Match = &matchmedia.Candidate{Provider: "tmdb", ID: "11", Title: "Film Title", Year: "2016", Synopsis: "plot"}
	onJob(movie)
	parent := matchmedia.Job{
		Path: m.showDir, Title: "Sample Show", Kind: "show", Status: "matched", Source: "scan",
		Match: &matchmedia.Candidate{Provider: "tmdb", ID: "22", Title: "Sample Show", Year: "2020", Synopsis: "plot"},
	}
	onJob(parent)
	deadline := time.Now().Add(3 * time.Second)
	var saw string
	for time.Now().Before(deadline) {
		job, err := m.db.GetScanJob(ctx, m.scanID)
		if err == nil && job != nil && job.Message.Valid {
			saw = job.Message.String
			if strings.Contains(saw, "match ") && strings.Contains(saw, "\nfetch ") {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !strings.Contains(saw, "match ") || !strings.Contains(saw, "\nfetch ") {
		return nil, fmt.Errorf("progress %q", saw)
	}
	if it, _ := m.db.GetMediaItemByPath(ctx, m.libID, m.showDir); it != nil {
		return nil, fmt.Errorf("parent fetched before fold")
	}
	parent.Files = []matchmedia.JobFile{{Path: m.epPath, Season: "1", Episode: "1"}}
	return []matchmedia.Job{movie, parent}, nil
}

func (m *overlapMeta) Run(context.Context, string, string, func(int, int, string)) ([]matchmedia.Job, string, error) {
	return nil, "", fmt.Errorf("unused")
}
func (m *overlapMeta) RunIngest(context.Context, []matchmedia.IngestRow, func(int, int, string)) ([]matchmedia.Job, string, error) {
	return nil, "", fmt.Errorf("unused")
}
func (m *overlapMeta) Select(string, string, string, string) (matchmedia.Job, error) {
	return matchmedia.Job{}, fmt.Errorf("unused")
}
func (m *overlapMeta) Job(string, string) (matchmedia.Job, error) {
	return matchmedia.Job{}, fmt.Errorf("unused")
}
func (m *overlapMeta) Catalog(string, string, string) (matchmedia.Catalog, error) {
	return matchmedia.Catalog{}, fmt.Errorf("unused")
}
func (m *overlapMeta) DownloadURL(string, string) ([]byte, string, error) {
	return nil, "", fmt.Errorf("unused")
}
func (m *overlapMeta) ResolveURL(raw, session string) string { return raw }
func (m *overlapMeta) SecretsStatus() (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (m *overlapMeta) SetSecrets(map[string]string) error { return nil }
func (m *overlapMeta) Config() (map[string]any, error)    { return map[string]any{}, nil }
func (m *overlapMeta) SetConfig(map[string]any) error     { return nil }
func (m *overlapMeta) Reload() error                      { return nil }
