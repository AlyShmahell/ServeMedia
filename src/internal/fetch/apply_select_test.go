package fetch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

type selectStoredMeta struct {
	writeStatsMeta
	lastHint matchmedia.Job
	lastProv string
	lastID   string
	out      matchmedia.Job
	err      error
}

func (m *selectStoredMeta) SelectStored(_ context.Context, hint matchmedia.Job, provider, id string) (matchmedia.Job, error) {
	m.lastHint = hint
	m.lastProv = provider
	m.lastID = id
	if m.err != nil {
		return matchmedia.Job{}, m.err
	}
	out := m.out
	if out.ID == "" {
		out = hint
		out.Status = "matched"
		out.Match = &matchmedia.Candidate{
			Provider: provider, ID: id, Title: "Picked Title", Year: "2015", Synopsis: "plot",
		}
		out.Candidates = nil
	}
	return out, nil
}

func TestApplySelectWithoutJobIDUsesSnapshot(t *testing.T) {
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
	dir := filepath.Join(media, "Dark Matter (2015)")
	video := filepath.Join(dir, "Dark Matter.mkv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Dark Matter", Path: video, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := EncodePickerSnapshot(matchmedia.Job{
		Path:  dir,
		Title: "Dark Matter",
		Kind:  "movie",
		Files: []matchmedia.JobFile{{Path: video}},
		Candidates: []matchmedia.Candidate{
			{Provider: "tvmaze", ID: "29035", Title: "Dark Matter", Year: "2015", Score: 0.9},
		},
	})
	if err := d.SetMatchCandidates(ctx, id, snap); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, id, "", "", "manual", ""); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.MatchMediaJobID.Valid && it.MatchMediaJobID.String != "" {
		t.Fatalf("job id should be empty, got %q", it.MatchMediaJobID.String)
	}
	if !it.MatchCandidates.Valid || it.MatchCandidates.String == "" {
		t.Fatal("expected picker snapshot")
	}

	meta := &selectStoredMeta{}
	w := &Worker{DB: d, Store: store, Meta: meta}
	if err := w.ApplySelect(ctx, it, "tvmaze", "29035", false); err != nil {
		t.Fatal(err)
	}
	wantID := matchmedia.StableID(dir)
	if meta.lastHint.ID != wantID {
		t.Fatalf("hint id %q want StableID %q", meta.lastHint.ID, wantID)
	}
	if meta.lastProv != "tvmaze" || meta.lastID != "29035" {
		t.Fatalf("select %s:%s", meta.lastProv, meta.lastID)
	}
	picked, err := d.GetMediaItem(ctx, id)
	if err != nil || picked == nil {
		t.Fatal(err)
	}
	if picked.MatchStatus.String != "matched" || picked.MetaID.String != "29035" {
		t.Fatalf("picked %#v", picked)
	}
}

func TestApplySelectWithoutJobIDOrSnapshotFails(t *testing.T) {
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
	path := filepath.Join(media, "orphan.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Orphan", Path: path, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store, Meta: &selectStoredMeta{err: fmt.Errorf("should not call")}}
	err = w.ApplySelect(ctx, it, "tvmaze", "1", false)
	if err == nil || !strings.Contains(err.Error(), "no matchmedia job for this title") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetJobMatchStampsStableID(t *testing.T) {
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
	dir := filepath.Join(media, "Manual")
	video := filepath.Join(dir, "Manual.mkv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Manual", Path: video, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store, Meta: writeStatsMeta{}}
	j := matchmedia.Job{
		Path: dir, Title: "Manual", Kind: "movie",
		Files:      []matchmedia.JobFile{{Path: video}},
		Candidates: []matchmedia.Candidate{{Provider: "tmdb", ID: "1", Title: "Manual", Score: 0.5}},
	}
	if err := w.setJobMatch(ctx, it, "", "", "manual", "", j); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.GetMediaItem(ctx, id)
	if err != nil || fresh == nil {
		t.Fatal(err)
	}
	want := matchmedia.StableID(dir)
	if !fresh.MatchMediaJobID.Valid || fresh.MatchMediaJobID.String != want {
		t.Fatalf("job id %#v want %q", fresh.MatchMediaJobID, want)
	}
	if !fresh.MatchCandidates.Valid || fresh.MatchCandidates.String == "" {
		t.Fatal("expected candidates snapshot")
	}
}
