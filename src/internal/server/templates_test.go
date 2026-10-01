package server

import (
	"bytes"
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/web"
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
		"partials/recent.html",
		"partials/items_live.html",
		"partials/entry_scan_progress.html",
		"partials/head_scripts.html",
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
		"MetaReady":          true,
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
	if !strings.Contains(html, `data-edit-url="/hx/media/1/meta"`) {
		t.Fatal("show title/plot edit")
	}
	if !strings.Contains(html, `data-upload-url="/hx/media/1/poster"`) {
		t.Fatal("show poster upload")
	}
	if !strings.Contains(html, `id="poster-edit-dialog"`) {
		t.Fatal("poster modal")
	}
	if !strings.Contains(html, `id="poster-edit-save"`) {
		t.Fatal("poster save")
	}
	if !strings.Contains(html, `/hx/shows/1/seasons/1/meta`) {
		t.Fatal("season card edit")
	}
	if strings.Contains(html, `/hx/media/9/meta`) {
		t.Fatal("child movie cards stay click-through")
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
	if !strings.Contains(html, "Don't add") {
		t.Fatal("don't add")
	}
	if !strings.Contains(html, "Applying match…") {
		t.Fatal("applying copy")
	}
}

func TestMatchDialogEmptyScanButton(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	data := map[string]any{
		"Item": db.MediaItem{ID: 7, Title: "Orphan Show", Path: "/media/Orphan"},
	}
	if err := tpl.ExecuteTemplate(&buf, "partials/match_dialog.html", data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, "openEntryScanModal") {
		t.Fatal("empty match must offer title scan")
	}
	if strings.Contains(html, "Rescan") {
		t.Fatal("Rescan copy should be gone")
	}
	if !strings.Contains(html, `type="button"`) {
		t.Fatal("scan control must not submit the dialog form")
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

func TestEntryScanModalDissociateOption(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/entry_scan_modal.html", map[string]any{
		"MetaReady": true, "MetaHint": "",
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `value="dissociate"`) {
		t.Fatal("dissociate option")
	}
	if !strings.Contains(html, "Dissociate current NFO+poster") {
		t.Fatal("dissociate label")
	}
	if !strings.Contains(html, "entry-scan-dissociate-hint") {
		t.Fatal("dissociate hint")
	}
}

func TestScanModalMatchMediaModes(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/scan_modal.html", map[string]any{
		"MetaReady": true, "MetaHint": "",
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, "Scan with MatchMedia (Rescan)") {
		t.Fatal("rescan label")
	}
	if !strings.Contains(html, "Scan with MatchMedia (Changes)") {
		t.Fatal("changes label")
	}
	if !strings.Contains(html, `value="matchmedia-changes" selected`) {
		t.Fatal("changes selected when ready")
	}
	if !strings.Contains(html, `id="scan-overwrite-wrap"`) {
		t.Fatal("overwrite wrap")
	}
}

func TestHomeAddLibraryScanModes(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "home.html", map[string]any{
		"LibraryCards":       []libraryCard{},
		"MetaReady":          true,
		"MetaDisabledReason": "",
		"Recent":             []itemCard{},
		"RecentTrigger":      "load, every 30s",
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="add-library-scan-mode"`) {
		t.Fatal("add-library mode")
	}
	if !strings.Contains(html, `value="local"`) || !strings.Contains(html, `value="matchmedia"`) || !strings.Contains(html, `value="matchmedia-changes"`) {
		t.Fatal("add-library mode values")
	}
	if !strings.Contains(html, `id="add-library-overwrite-wrap"`) {
		t.Fatal("add-library overwrite")
	}
	if !strings.Contains(html, `value="matchmedia-changes" selected`) {
		t.Fatal("changes selected when ready")
	}
}

func TestHomeRecentPollMarkup(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "home.html", map[string]any{
		"Recent": []itemCard{
			{Item: db.MediaItem{ID: 7, Kind: "movie", Title: "Fresh Title"}},
		},
		"RecentTrigger":      "load, every 2s",
		"LibraryCards":       []libraryCard{},
		"MetaReady":          true,
		"MetaDisabledReason": "",
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-get="/hx/home/recent"`) {
		t.Fatalf("recent endpoint: %s", html)
	}
	if !strings.Contains(html, `id="recent"`) {
		t.Fatalf("recent wrapper: %s", html)
	}
	if !strings.Contains(html, `hx-trigger="load, every 2s"`) {
		t.Fatalf("scan poll trigger: %s", html)
	}
	if !strings.Contains(html, "Fresh Title") {
		t.Fatalf("recent card: %s", html)
	}
}

func TestItemsLivePollsOnlyWhenFlagged(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	data := map[string]any{
		"Library":            db.Library{ID: 3, Name: "Anime"},
		"Items":              []itemCard{{Item: db.MediaItem{ID: 9, Kind: "show", Title: "New Show"}}},
		"ItemCount":          1,
		"Poll":               false,
		"MetaReady":          true,
		"MetaDisabledReason": "",
	}
	var idle bytes.Buffer
	if err := tpl.ExecuteTemplate(&idle, "partials/items_live.html", data); err != nil {
		t.Fatal(err)
	}
	idleHTML := idle.String()
	if !strings.Contains(idleHTML, `id="items-live"`) {
		t.Fatalf("wrapper: %s", idleHTML)
	}
	if strings.Contains(idleHTML, `hx-trigger="every 2s"`) {
		t.Fatalf("idle must not poll: %s", idleHTML)
	}
	if strings.Contains(idleHTML, `hx-swap-oob`) {
		t.Fatalf("full page must not oob count: %s", idleHTML)
	}

	data["Poll"] = true
	data["SwapCount"] = true
	data["ItemCount"] = 2
	var live bytes.Buffer
	if err := tpl.ExecuteTemplate(&live, "partials/items_live.html", data); err != nil {
		t.Fatal(err)
	}
	liveHTML := live.String()
	if !strings.Contains(liveHTML, `hx-trigger="every 2s"`) {
		t.Fatalf("scan must poll: %s", liveHTML)
	}
	if !strings.Contains(liveHTML, `hx-get="/hx/libraries/3/items"`) {
		t.Fatalf("items url: %s", liveHTML)
	}
	if !strings.Contains(liveHTML, `id="library-count"`) || !strings.Contains(liveHTML, `hx-swap-oob="true"`) {
		t.Fatalf("oob count: %s", liveHTML)
	}
	if !strings.Contains(liveHTML, "2 titles") {
		t.Fatalf("count copy: %s", liveHTML)
	}
}

func TestMovieTemplateEditControls(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	data := map[string]any{
		"Item":               db.MediaItem{ID: 4, Kind: "movie", Title: "Film Title"},
		"MetaReady":          true,
		"MetaDisabledReason": "",
	}
	if err := tpl.ExecuteTemplate(&buf, "movie.html", data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-edit-url="/hx/media/4/meta"`) {
		t.Fatal("movie meta edit")
	}
	if !strings.Contains(html, `data-upload-url="/hx/media/4/poster"`) {
		t.Fatal("movie poster upload")
	}
	if !strings.Contains(html, `data-role="edit"`) {
		t.Fatal("edit icon")
	}
	if !strings.Contains(html, `id="poster-edit-dialog"`) {
		t.Fatal("poster modal")
	}
	if !strings.Contains(html, `id="poster-edit-save"`) {
		t.Fatal("poster save")
	}
}

func TestSeasonTemplateEditControls(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	data := map[string]any{
		"Item": db.MediaItem{ID: 2, Kind: "show", Title: "Sample Show"},
		"Season": db.Season{
			SeasonNumber: 1,
			Title:        sql.NullString{String: "Season 1", Valid: true},
			Plot:         sql.NullString{String: "Season plot", Valid: true},
		},
		"Episodes": []episodeCard{
			{Episode: db.Episode{ID: 8, EpisodeNumber: 1, Title: sql.NullString{String: "Pilot", Valid: true}}},
		},
	}
	if err := tpl.ExecuteTemplate(&buf, "season.html", data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-edit-url="/hx/shows/2/seasons/1/meta"`) {
		t.Fatal("season header edit")
	}
	if !strings.Contains(html, `data-upload-url="/hx/shows/2/seasons/1/poster"`) {
		t.Fatal("season header poster")
	}
	if !strings.Contains(html, `data-edit-url="/hx/episodes/8/meta"`) {
		t.Fatal("episode edit")
	}
	if !strings.Contains(html, `data-upload-url="/hx/episodes/8/poster"`) {
		t.Fatal("episode still upload")
	}
	if !strings.Contains(html, `id="poster-edit-dialog"`) {
		t.Fatal("poster modal")
	}
	if !strings.Contains(html, `class="muted">E1`) && !strings.Contains(html, `>E1<`) {
		if !strings.Contains(html, "E1") {
			t.Fatal("episode number prefix")
		}
	}
}
