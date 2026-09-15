package server

import (
	"bytes"
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/web"
)

func TestMustParseTemplates(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	for _, name := range []string{
		"home.html",
		"library.html",
		"movie.html",
		"show.html",
		"partials/match_dialog.html",
		"partials/scan_modal.html",
		"partials/card_actions.html",
		"partials/items.html",
		"partials/entry_scan_progress.html",
	} {
		if tpl.Lookup(name) == nil {
			t.Fatalf("missing template %s", name)
		}
	}
}

func TestShowTemplateTitlesGrid(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	data := map[string]any{
		"Item": db.MediaItem{ID: 1, LibraryID: 2, Title: "KonoSuba", Kind: "show"},
		"SeasonCards": []struct {
			Season       db.Season
			EpisodeCount int
			ProgressPct  int
		}{
			{Season: db.Season{SeasonNumber: 1, Title: sql.NullString{String: "Season 1", Valid: true}}, EpisodeCount: 10},
			{Season: db.Season{SeasonNumber: 0, Title: sql.NullString{String: "Ghost", Valid: true}}, EpisodeCount: 0},
		},
		"ChildItems": []itemCard{
			{Item: db.MediaItem{ID: 9, Kind: "movie", Title: "Explosion", Plot: sql.NullString{String: "secret plot", Valid: true}}},
		},
		"MetaReady": true,
		"MetaDisabledReason": "",
	}
	if err := tpl.ExecuteTemplate(&buf, "show.html", data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, ">Titles<") {
		t.Fatal("expected Titles heading")
	}
	if strings.Contains(html, "Related") {
		t.Fatal("related section should be gone")
	}
	if strings.Contains(html, "Ghost") {
		t.Fatal("empty season should be omitted")
	}
	if !strings.Contains(html, "Season 1") {
		t.Fatal("season with episodes")
	}
	if !strings.Contains(html, "Explosion") {
		t.Fatal("child title")
	}
	if strings.Contains(html, "secret plot") {
		t.Fatal("nested plot should be omitted")
	}
	if strings.Contains(html, "No synopsis") {
		t.Fatal("no synopsis placeholder")
	}
}

func TestEntryScanProgressTemplate(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)

	var running bytes.Buffer
	if err := tpl.ExecuteTemplate(&running, "partials/entry_scan_progress.html", entryScanProgressData(
		&db.ScanJob{ID: 3, Status: "running", ProgressPct: 40, Message: sql.NullString{String: "Matching…", Valid: true}},
		true, 12, true, "",
	)); err != nil {
		t.Fatal(err)
	}
	got := running.String()
	if !strings.Contains(got, `data-media-id="12"`) {
		t.Fatalf("media id: %s", got)
	}
	if !strings.Contains(got, "/hx/scan/3/entry-status?media=12") {
		t.Fatalf("poll url: %s", got)
	}
	if !strings.Contains(got, "matchmedia=1") {
		t.Fatalf("matchmedia flag: %s", got)
	}
	if strings.Contains(got, "data-need-pick") || strings.Contains(got, "data-scan-reload") {
		t.Fatalf("running job should not finish: %s", got)
	}

	var pick bytes.Buffer
	if err := tpl.ExecuteTemplate(&pick, "partials/entry_scan_progress.html", entryScanProgressData(
		&db.ScanJob{ID: 3, Status: "done", ProgressPct: 100},
		false, 12, true, "manual",
	)); err != nil {
		t.Fatal(err)
	}
	picked := pick.String()
	if !strings.Contains(picked, `data-need-pick="1"`) {
		t.Fatalf("need pick: %s", picked)
	}
	if strings.Contains(picked, "data-scan-reload") {
		t.Fatalf("manual should not reload: %s", picked)
	}
	if !strings.Contains(picked, "Finished.") {
		t.Fatalf("finished copy: %s", picked)
	}

	var reload bytes.Buffer
	if err := tpl.ExecuteTemplate(&reload, "partials/entry_scan_progress.html", entryScanProgressData(
		&db.ScanJob{ID: 4, Status: "done", ProgressPct: 100},
		false, 12, false, "",
	)); err != nil {
		t.Fatal(err)
	}
	reloaded := reload.String()
	if !strings.Contains(reloaded, `data-scan-reload="1"`) {
		t.Fatalf("local reload: %s", reloaded)
	}
	if strings.Contains(reloaded, "data-need-pick") {
		t.Fatalf("local should not pick: %s", reloaded)
	}
}

func TestMatchDialogBusyMarkup(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	data := map[string]any{
		"Item": db.MediaItem{ID: 5, Title: "Show", Path: "/media/Show"},
		"Candidates": []struct {
			Provider string
			ID       string
			Title    string
			Year     int
			Score    float64
			Poster   string
			Synopsis string
		}{{Provider: "omdb", ID: "tt1", Title: "Hit", Score: 0.9}},
	}
	if err := tpl.ExecuteTemplate(&buf, "partials/match_dialog.html", data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="match-pick-dialog"`) {
		t.Fatal("dialog id")
	}
	if !strings.Contains(html, `class="match-busy"`) {
		t.Fatal("busy overlay")
	}
	if !strings.Contains(html, "match-busy-bar") {
		t.Fatal("busy bar")
	}
}

func TestCardActionsErrorBadgeNoMatchModal(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/card_actions.html", map[string]any{
		"ID": int64(12), "Title": "Witch Hat Atelier", "MatchStatus": "error",
		"MatchError": "tvmaze: cooldown; jikan: cooldown", "MetaReady": true,
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `class="match-fail"`) {
		t.Fatalf("error badge: %s", html)
	}
	if !strings.Contains(html, "tvmaze: cooldown; jikan: cooldown") {
		t.Fatalf("tooltip: %s", html)
	}
	if strings.Contains(html, "openMatchModal") {
		t.Fatalf("error must not open picker: %s", html)
	}
	if strings.Contains(html, `class="match-bang"`) {
		t.Fatalf("error must not use choose-match bang: %s", html)
	}

	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/card_actions.html", map[string]any{
		"ID": int64(12), "Title": "Unknown", "MatchStatus": "unmatched", "MatchError": "", "MetaReady": true,
	}); err != nil {
		t.Fatal(err)
	}
	unmatched := buf.String()
	if !strings.Contains(unmatched, `class="match-bang"`) || !strings.Contains(unmatched, "openMatchModal") {
		t.Fatalf("unmatched still needs pick: %s", unmatched)
	}
	if strings.Contains(unmatched, `class="match-fail"`) {
		t.Fatalf("unmatched must not use error badge: %s", unmatched)
	}
}

