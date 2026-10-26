package fetch

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestChangesSkipsFetchForFinishedSidecar(t *testing.T) {
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
	showDir := filepath.Join(media, "Known Show")
	ep := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(ep, filepath.Ext(ep))+".nfo", []byte("<episodedetails><title>Ep</title></episodedetails>"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(showDir, "tvshow.nfo")
	posterPath := filepath.Join(showDir, "poster.jpg")
	nfoBody := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tvshow><title>Known Show</title><plot>keep-me</plot><uniqueid type="tvmaze" default="true">7906</uniqueid></tvshow>
`)
	if err := os.WriteFile(nfoPath, nfoBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(posterPath, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := &finishedSidecarMeta{nfo: nfoPath, poster: posterPath, showDir: showDir, ep: ep}
	w := &Worker{DB: d, Store: store, Meta: meta}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: false, ScanMode: "changes"}); err != nil {
		t.Fatal(err)
	}
	if meta.catalog.Load() != 0 {
		t.Fatalf("catalog hits %d", meta.catalog.Load())
	}
	if meta.writes.Load() != 0 {
		t.Fatalf("write calls %d", meta.writes.Load())
	}
	got, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(nfoBody) {
		t.Fatalf("nfo rewritten: %s", got)
	}
	it, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.MetaProvider.String != "tvmaze" || it.MetaID.String != "7906" {
		t.Fatalf("identity %s %s", it.MetaProvider.String, it.MetaID.String)
	}
	eps, err := d.ListEpisodesByShow(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].Path != ep {
		t.Fatalf("episodes %#v", eps)
	}
}

func TestChangesFetchesShowMissingEpisodeNFO(t *testing.T) {
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
	showDir := filepath.Join(media, "Known Show")
	ep := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(showDir, "tvshow.nfo")
	posterPath := filepath.Join(showDir, "poster.jpg")
	nfoBody := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tvshow><title>Known Show</title><plot>keep-me</plot></tvshow>
`)
	if err := os.WriteFile(nfoPath, nfoBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(posterPath, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Known Show", Path: showDir, Mtime: 1,
		Plot: sql.NullString{String: "keep-plot", Valid: true},
		Year: sql.NullInt64{Int64: 2016, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET poster_path=NULL WHERE id=?`, showID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 1, "Season 1", posterPath, ""); err != nil {
		t.Fatal(err)
	}
	meta := &finishedSidecarMeta{nfo: nfoPath, showDir: showDir, ep: ep, matchID: "4242"}
	w := &Worker{DB: d, Store: store, Meta: meta}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: false, ScanMode: "changes"}); err != nil {
		t.Fatal(err)
	}
	if meta.writes.Load() != 0 {
		t.Fatalf("write calls %d", meta.writes.Load())
	}
	got, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(nfoBody) {
		t.Fatalf("nfo rewritten: %s", got)
	}
	var poster, plot sql.NullString
	var year sql.NullInt64
	if err := d.SQL.QueryRowContext(ctx, `SELECT poster_path, plot, year FROM media_items WHERE id=?`, showID).Scan(&poster, &plot, &year); err != nil {
		t.Fatal(err)
	}
	if poster.String != posterPath || plot.String != "keep-plot" || year.Int64 != 2016 {
		t.Fatalf("item poster %q plot %q year %d", poster.String, plot.String, year.Int64)
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.MetaProvider.String != "tvmaze" || it.MetaID.String != "4242" {
		t.Fatalf("identity %s %s", it.MetaProvider.String, it.MetaID.String)
	}
	showNode := nodePoster(t, d, lib.ID, "Known Show")
	seasonNode := nodePoster(t, d, lib.ID, "Known Show/Season 1")
	if showNode != posterPath || seasonNode != posterPath {
		t.Fatalf("nodes show %q season %q", showNode, seasonNode)
	}
}

func TestChangesKeepsExistingPosterWithoutMetaID(t *testing.T) {
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
	showDir := filepath.Join(media, "Known Show")
	ep := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(showDir, "tvshow.nfo")
	posterPath := filepath.Join(showDir, "poster.jpg")
	custom := filepath.Join(showDir, "custom-poster.jpg")
	if err := os.WriteFile(nfoPath, []byte("<tvshow><title>Known Show</title></tvshow>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(posterPath, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Known Show", Path: showDir, Mtime: 1,
		PosterPath: sql.NullString{String: custom, Valid: true},
		Plot:       sql.NullString{String: "keep-plot", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 1, "Season 1", posterPath, ""); err != nil {
		t.Fatal(err)
	}
	meta := &finishedSidecarMeta{nfo: nfoPath, poster: posterPath, showDir: showDir, ep: ep, matchID: "4242"}
	w := &Worker{DB: d, Store: store, Meta: meta}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: false, ScanMode: "changes"}); err != nil {
		t.Fatal(err)
	}
	var poster, plot sql.NullString
	if err := d.SQL.QueryRowContext(ctx, `SELECT poster_path, plot FROM media_items WHERE id=?`, showID).Scan(&poster, &plot); err != nil {
		t.Fatal(err)
	}
	if poster.String != custom || plot.String != "keep-plot" {
		t.Fatalf("item poster %q plot %q", poster.String, plot.String)
	}
	if nodePoster(t, d, lib.ID, "Known Show") != custom {
		t.Fatalf("show node %q", nodePoster(t, d, lib.ID, "Known Show"))
	}
	if nodePoster(t, d, lib.ID, "Known Show/Season 1") != posterPath {
		t.Fatalf("season node %q", nodePoster(t, d, lib.ID, "Known Show/Season 1"))
	}
}

func TestSetNodeGroupKeepsPoster(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodePoster(ctx, id, "/media/Show/poster.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, id, db.NodeGroup{Kind: "show", Title: "Show", Poster: ""}); err != nil {
		t.Fatal(err)
	}
	if got := nodePoster(t, d, lib.ID, "Show"); got != "/media/Show/poster.jpg" {
		t.Fatalf("poster %q", got)
	}
}

func nodePoster(t *testing.T, d *db.DB, libraryID int64, rel string) string {
	t.Helper()
	var poster sql.NullString
	err := d.SQL.QueryRowContext(context.Background(), `SELECT poster_path FROM library_nodes WHERE library_id=? AND rel_path=?`, libraryID, rel).Scan(&poster)
	if err != nil {
		t.Fatal(err)
	}
	return poster.String
}

func TestChangesKeepsExistingShowNFO(t *testing.T) {
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
	showDir := filepath.Join(media, "Known Show")
	ep1 := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	ep2 := filepath.Join(showDir, "Season 1", "S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(ep1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(showDir, "tvshow.nfo")
	nfoBody := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tvshow><title>Known Show</title><plot>keep-me</plot><uniqueid type="tvmaze">10772</uniqueid></tvshow>
`)
	if err := os.WriteFile(nfoPath, nfoBody, 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Known Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: ep1, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var catalogHits atomic.Int32
	srv := showScanServer(t, showDir, []string{ep1, ep2}, &catalogHits, "keep-me")
	defer srv.Close()
	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: false, ScanMode: "changes"}); err != nil {
		t.Fatal(err)
	}
	if catalogHits.Load() != 0 {
		t.Fatalf("catalog hits %d", catalogHits.Load())
	}
	got, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(nfoBody) {
		t.Fatalf("nfo rewritten: %s", got)
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.MetaProvider.String != "tvmaze" || it.MetaID.String != "10772" {
		t.Fatalf("identity %s %s", it.MetaProvider.String, it.MetaID.String)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 {
		t.Fatalf("episodes %d", len(eps))
	}
}

func TestRescanAppliesOverExistingNFO(t *testing.T) {
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
	showDir := filepath.Join(media, "Known Show")
	ep := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(showDir, "tvshow.nfo")
	if err := os.WriteFile(nfoPath, []byte("<tvshow><title>Known Show</title><plot>keep-me</plot></tvshow>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Known Show", Path: showDir, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var catalogHits atomic.Int32
	srv := showScanServer(t, showDir, []string{ep}, &catalogHits, "replaced")
	defer srv.Close()
	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: false, ScanMode: "rescan"}); err != nil {
		t.Fatal(err)
	}
	if catalogHits.Load() == 0 {
		t.Fatal("rescan did not apply")
	}
	got, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "replaced") {
		t.Fatalf("nfo %s", got)
	}
}

func showScanServer(t *testing.T, showDir string, eps []string, hits *atomic.Int32, synopsis string) *httptest.Server {
	t.Helper()
	files := make([]map[string]any, len(eps))
	for i, ep := range eps {
		files[i] = map[string]any{"path": ep, "season": "1", "episode": strconv.Itoa(i + 1)}
	}
	const sess = "20261023T000000Z-sidecar0000001"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1,"mode":"changes"}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "j-show", "status": "matched", "path": showDir, "kind": "show", "source": "scan",
			"files": files,
			"match": map[string]any{"provider": "tvmaze", "id": "10772", "title": "Known Show"},
		}})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": "10772", "title": "Known Show", "synopsis": synopsis,
		})
	})
	return httptest.NewServer(mux)
}

type finishedSidecarMeta struct {
	nfo, poster, showDir, ep string
	matchID                  string
	catalog                  atomic.Int32
	writes                   atomic.Int32
}

func (m *finishedSidecarMeta) Status() (matchmedia.Status, error) {
	return matchmedia.Status{Ready: true}, nil
}

func (m *finishedSidecarMeta) Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]matchmedia.Job, error) {
	if report != nil {
		report(1, 1, "Known Show")
	}
	id := m.matchID
	if id == "" {
		id = "1"
	}
	return []matchmedia.Job{{
		ID: "j-show", Path: m.showDir, Title: "Known Show", Kind: "show", Status: "matched", Source: "scan",
		NFOPath: m.nfo, PosterPath: m.poster,
		Files: []matchmedia.JobFile{{Path: m.ep, Season: "1", Episode: "1"}},
		Match: &matchmedia.Candidate{Provider: "tvmaze", ID: id, Title: "From Provider", Synopsis: "fetched", Poster: "https://img.example/poster.jpg"},
	}}, nil
}

func (m *finishedSidecarMeta) Write(context.Context, string, []matchmedia.Job) ([]matchmedia.Job, error) {
	m.writes.Add(1)
	return nil, nil
}

func (m *finishedSidecarMeta) Match(context.Context, []matchmedia.Job, func(int, int, string), func(matchmedia.Job)) ([]matchmedia.Job, error) {
	return nil, errMatchNotExpected
}

func (m *finishedSidecarMeta) SelectStored(context.Context, matchmedia.Job, string, string) (matchmedia.Job, error) {
	return matchmedia.Job{}, errMatchNotExpected
}

func (m *finishedSidecarMeta) Run(context.Context, string, string, func(int, int, string)) ([]matchmedia.Job, string, error) {
	return nil, "", errMatchNotExpected
}
func (m *finishedSidecarMeta) RunIngest(context.Context, []matchmedia.IngestRow, func(int, int, string)) ([]matchmedia.Job, string, error) {
	return nil, "", errMatchNotExpected
}
func (m *finishedSidecarMeta) Select(string, string, string, string) (matchmedia.Job, error) {
	return matchmedia.Job{}, errMatchNotExpected
}
func (m *finishedSidecarMeta) Job(string, string) (matchmedia.Job, error) {
	return matchmedia.Job{}, errMatchNotExpected
}
func (m *finishedSidecarMeta) Catalog(string, string, string) (matchmedia.Catalog, error) {
	m.catalog.Add(1)
	return matchmedia.Catalog{}, errMatchNotExpected
}
func (m *finishedSidecarMeta) DownloadURL(string, string) ([]byte, string, error) {
	m.catalog.Add(1)
	return nil, "", errMatchNotExpected
}
func (m *finishedSidecarMeta) ResolveURL(raw, _ string) string { return raw }
func (m *finishedSidecarMeta) SecretsStatus() (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (m *finishedSidecarMeta) SetSecrets(map[string]string) error { return nil }
func (m *finishedSidecarMeta) Config() (map[string]any, error)    { return map[string]any{}, nil }
func (m *finishedSidecarMeta) SetConfig(map[string]any) error     { return nil }
func (m *finishedSidecarMeta) Reload() error                      { return nil }

var errMatchNotExpected = errString("provider match")

type errString string

func (e errString) Error() string { return string(e) }
