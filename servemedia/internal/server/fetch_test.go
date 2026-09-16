package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/internal/config"
	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/matchmedia"
	"github.com/alyshmahell/servemedia/web"
	"github.com/go-chi/chi/v5"
)

func TestPickerCandidatesSeedsMatch(t *testing.T) {
	got := pickerCandidates(matchmedia.Job{
		Match: &matchmedia.Candidate{Provider: "tmdb", ID: "11", Title: "Hit", Score: 0.99},
	})
	if len(got) != 1 || got[0].ID != "11" || got[0].Provider != "tmdb" {
		t.Fatalf("%#v", got)
	}

	got = pickerCandidates(matchmedia.Job{
		Match: &matchmedia.Candidate{Provider: "tmdb", ID: "11", Title: "Hit"},
		Candidates: []matchmedia.Candidate{
			{Provider: "tmdb", ID: "11", Title: "Hit", Score: 0.99},
			{Provider: "tvmaze", ID: "22", Title: "Other", Score: 0.4},
		},
	})
	if len(got) != 2 {
		t.Fatalf("dup match %#v", got)
	}

	got = pickerCandidates(matchmedia.Job{})
	if len(got) != 0 {
		t.Fatalf("empty %#v", got)
	}
}

func TestHandleMatchPostSkip(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/media")
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/media/Film.mkv", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, id, "sess", "job", "manual", ""); err != nil {
		t.Fatal(err)
	}
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS)}
	rtr := chi.NewRouter()
	rtr.Post("/hx/media/{id}/match", s.handleMatchPost)
	form := url.Values{"skip": {"1"}}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/media/%d/match", id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil || !got.MatchSkipped() {
		t.Fatalf("skip %#v %v", got, err)
	}
	if got.MatchMediaJobID.Valid {
		t.Fatalf("job still set %#v", got.MatchMediaJobID)
	}
}
