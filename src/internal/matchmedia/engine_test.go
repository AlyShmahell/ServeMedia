package matchmedia

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mmconfig "github.com/alyshmahell/matchmedia/src/mod/config"
	mmfetch "github.com/alyshmahell/matchmedia/src/mod/fetch"
	"github.com/alyshmahell/matchmedia/src/mod/match"
)

func TestRequestRoundTrip(t *testing.T) {
	req := Request{
		Section: "detail", Provider: "tvmaze", NFO: "tvmaze",
		URL: "https://api.example/shows/1", Query: "grimgar", Season: "1",
	}
	job := Job{
		ID: "j", Path: "/shows/Grimgar", Kind: "show", Status: "pending", Ranker: "replay", Ngrams: 2,
		Requests: []Request{req},
		Match: &Candidate{
			Provider: "tvmaze", ID: "1", Title: "Grimgar",
			Requests: []Request{req},
		},
	}
	rawJob, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var back Job
	if err := json.Unmarshal(rawJob, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Requests) != 1 || back.Requests[0] != req || back.Ngrams != 2 {
		t.Fatalf("job requests %+v ngrams %d", back.Requests, back.Ngrams)
	}
	if back.Match == nil || len(back.Match.Requests) != 1 || back.Match.Requests[0] != req {
		t.Fatalf("hit requests %+v", back.Match)
	}
	if !strings.Contains(string(rawJob), `"provider"`) {
		t.Fatalf("hit json %s", rawJob)
	}
	raw, err := json.Marshal(job.Match)
	if err != nil {
		t.Fatal(err)
	}
	var got Candidate
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 1 || got.Requests[0] != req {
		t.Fatalf("json requests %+v", got.Requests)
	}
	alt := []byte(`{"provider":"tvmaze","id":"1","Requests":[{"Section":"detail","Provider":"tvmaze","NFO":"tvmaze","URL":"https://api.example/shows/1","Query":"grimgar","Season":"1"}]}`)
	var exported Candidate
	if err := json.Unmarshal(alt, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Requests) != 1 || exported.Requests[0] != req || exported.ID != "1" {
		t.Fatalf("exported requests %+v", exported)
	}
}

func TestTallyMatchKeepsSidecarAndReplayApart(t *testing.T) {
	var matched, sidecar, replay, manual, unmatched, errored int
	tallyMatch(match.Job{Title: "S01", Parent: "Show", Path: "/Show/S01", Status: "unmatched"}, &matched, &sidecar, &replay, &manual, &unmatched, &errored)
	tallyMatch(match.Job{Title: "Saved", Status: "matched", Ranker: "sidecar"}, &matched, &sidecar, &replay, &manual, &unmatched, &errored)
	tallyMatch(match.Job{Title: "Again", Status: "matched", Ranker: "replay"}, &matched, &sidecar, &replay, &manual, &unmatched, &errored)
	tallyMatch(match.Job{Title: "Found", Status: "matched", Ranker: "set"}, &matched, &sidecar, &replay, &manual, &unmatched, &errored)
	if matched != 1 || sidecar != 1 || replay != 1 || unmatched != 0 {
		t.Fatalf("matched=%d sidecar=%d replay=%d unmatched=%d", matched, sidecar, replay, unmatched)
	}
}

func TestSidecarComplete(t *testing.T) {
	dir := t.TempDir()
	show := filepath.Join(dir, "Show")
	ep := filepath.Join(show, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "tvshow.nfo"), []byte("<tvshow><title>Show</title></tvshow>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "poster.jpg"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := Job{Path: show, Kind: "show", Files: []JobFile{{Path: ep, Season: "1", Episode: "1"}}}
	if SidecarComplete(job) {
		t.Fatal("show missing an episode nfo was treated as complete")
	}
	if err := os.WriteFile(strings.TrimSuffix(ep, filepath.Ext(ep))+".nfo", []byte("<episodedetails><title>Ep</title></episodedetails>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if SidecarComplete(job) {
		t.Fatal("show missing season files was treated as complete")
	}
	if err := os.WriteFile(filepath.Join(show, "Season 1", "season.nfo"), []byte("<season><title>Season 1</title></season>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "Season 1", "poster.jpg"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !SidecarComplete(job) {
		t.Fatal("show with season files was not complete")
	}
	movie := filepath.Join(dir, "Movie")
	vid := filepath.Join(movie, "movie.mkv")
	if err := os.MkdirAll(movie, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vid, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movie, "movie.nfo"), []byte("<movie><title>Movie</title></movie>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movie, "poster.jpg"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !SidecarComplete(Job{Path: movie, Kind: "movie", Files: []JobFile{{Path: vid}}}) {
		t.Fatal("movie with nfo and poster was not complete")
	}
}

func TestEngineWriteKeepsPartialShowNFO(t *testing.T) {
	cfg, err := mmconfig.New("")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	show := filepath.Join(dir, "Show")
	ep := filepath.Join(show, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoPath := filepath.Join(show, "tvshow.nfo")
	nfoBody := []byte("<tvshow><title>Show</title><plot>keep-me</plot></tvshow>")
	if err := os.WriteFile(nfoPath, nfoBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(show, "poster.jpg"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{fetch: mmfetch.New(cfg), jobs: map[string]Job{}}
	job := Job{
		Path: show, Kind: "show", Status: "matched", Title: "Show",
		Files: []JobFile{{Path: ep, Season: "1", Episode: "1"}},
		Match: &Candidate{Provider: "tvmaze", ID: "1", Title: "Show"},
	}
	reported := 0
	out, st, err := e.Write(context.Background(), "changes", []Job{job}, func(done, total int, _ Job, stats WriteStats) {
		reported++
		if done != 1 || total != 1 || stats.TitleNFO != 0 || stats.EpisodeNFO != 0 || stats.Posters != 0 {
			t.Fatalf("progress %d/%d stats %+v", done, total, stats)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if reported != 1 || st.TitleNFO != 0 || st.EpisodeNFO != 0 || st.Posters != 0 || len(out) != 1 {
		t.Fatalf("reported %d stats %+v jobs %d", reported, st, len(out))
	}
	got, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(nfoBody) {
		t.Fatalf("nfo rewritten: %s", got)
	}
}
