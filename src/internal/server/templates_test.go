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
		"partials/match_dialog.html",
		"partials/scan_modal.html",
		"partials/card_actions.html",
		"partials/items.html",
		"partials/recent.html",
		"partials/items_live.html",
		"partials/folder_hero.html",
		"partials/entry_scan_progress.html",
		"partials/head_scripts.html",
	} {
		if tpl.Lookup(name) == nil {
			t.Fatalf("missing template %s", name)
		}
	}
}

func TestRecentAndItemsCardsUseHref(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	cards := []itemCard{{
		Item: db.MediaItem{ID: 7, Kind: "show", Title: "Fresh Title"},
		Href: "/libraries/4/tree?path=Fresh%20Title",
	}}
	var recent bytes.Buffer
	if err := tpl.ExecuteTemplate(&recent, "partials/recent.html", map[string]any{
		"Recent": cards, "RecentTrigger": "every 30s", "MetaReady": true, "MetaDisabledReason": "",
	}); err != nil {
		t.Fatal(err)
	}
	html := recent.String()
	if !strings.Contains(html, `href="/libraries/4/tree?path=Fresh%20Title"`) {
		t.Fatalf("recent href %s", html)
	}
	if strings.Contains(html, "/shows/") || strings.Contains(html, "/movies/") {
		t.Fatalf("recent catalog url %s", html)
	}
	var items bytes.Buffer
	if err := tpl.ExecuteTemplate(&items, "partials/items.html", map[string]any{
		"Items": cards, "MetaReady": true, "MetaDisabledReason": "",
	}); err != nil {
		t.Fatal(err)
	}
	got := items.String()
	if !strings.Contains(got, `href="/libraries/4/tree?path=Fresh%20Title"`) {
		t.Fatalf("items href %s", got)
	}
	if strings.Contains(got, "/shows/") || strings.Contains(got, "/movies/") {
		t.Fatalf("items catalog url %s", got)
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
	if !strings.Contains(unmatched, `class="match-bang"`) || !strings.Contains(unmatched, `data-media-id="12"`) {
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
	if !strings.Contains(html, ">Rescan<") {
		t.Fatal("rescan label")
	}
	if !strings.Contains(html, ">Changes<") || !strings.Contains(html, ">Missing<") {
		t.Fatal("changes and missing labels")
	}
	if !strings.Contains(html, `value="changes" selected`) {
		t.Fatal("changes selected when ready")
	}
	if strings.Contains(html, `value="nfo"`) || strings.Contains(html, `name="persist"`) || strings.Contains(html, `name="overwrite"`) {
		t.Fatal("library scan dialog still exposes nfo or sidecar checkboxes")
	}
}

func TestParseScanProgress(t *testing.T) {
	scan := parseScanProgress("scan 195/195 somali and the forest spirit")
	if scan.Phase != "scan" || scan.Done != 195 || scan.Total != 195 || scan.Title != "somali and the forest spirit" || len(scan.Stats) != 0 {
		t.Fatalf("scan %#v", scan)
	}
	match := parseScanProgress("match 230/230 matched:185 manual:40 unmatched:5 error:0 dark matter")
	if match.Phase != "match" || match.Title != "dark matter" || len(match.Stats) != 4 || match.Stats[0].Value != 185 || match.Stats[3].Tone != "error" {
		t.Fatalf("match %#v", match)
	}
	full := parseScanProgress("match 12/40 matched:9 manual:2 unmatched:1 error:0 sidecar:4 replay:3 folded:6 Cowboy Bebop")
	if full.Title != "Cowboy Bebop" || len(full.Stats) != 7 || full.Stats[4].Label != "sidecar" || full.Stats[4].Value != 4 || full.Stats[5].Label != "replay" || full.Stats[6].Value != 6 || full.Stats[6].Tone != "folded" {
		t.Fatalf("full match %#v", full)
	}
	fetch := parseScanProgress("fetch 185/185 nfo:2467 poster:183 somali and the forest spirit")
	if fetch.Phase != "fetch" || len(fetch.Stats) != 2 || fetch.Stats[0].Label != "nfo" || fetch.Stats[0].Value != 2467 || fetch.Stats[1].Value != 183 {
		t.Fatalf("fetch %#v", fetch)
	}
	withEp := parseScanProgress("fetch 3/10 nfo:1 episode:12 poster:2 Show")
	if withEp.Title != "Show" || len(withEp.Stats) != 3 || withEp.Stats[0].Label != "nfo" || withEp.Stats[0].Value != 1 || withEp.Stats[1].Label != "episode" || withEp.Stats[1].Value != 12 || withEp.Stats[2].Label != "poster" || withEp.Stats[2].Value != 2 {
		t.Fatalf("fetch with episode %#v", withEp)
	}
	open := parseScanProgress("fetch 40 nfo:510 poster:38 other title")
	if open.Done != 40 || open.Total != 0 || open.Title != "other title" || open.Stats[0].Value != 510 {
		t.Fatalf("open fetch %#v", open)
	}
	lines := parseScanLines("match 120/230 matched:80 manual:10 unmatched:2 error:0 title\nfetch 40 nfo:510 poster:38 other title")
	if len(lines) != 2 || lines[0].Phase != "match" || lines[1].Phase != "fetch" || lines[1].Done != 40 {
		t.Fatalf("lines %#v", lines)
	}
	stills := parseScanProgress("stills 4/20 Legend of Crimson")
	if stills.Phase != "stills" || stills.Done != 4 || stills.Total != 20 || stills.Title != "Legend of Crimson" || len(stills.Stats) != 0 {
		t.Fatalf("stills %#v", stills)
	}
	if parseScanProgress("Complete").Phase != "" {
		t.Fatal("plain message parsed")
	}
}

func TestScanProgressStacksBothLines(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	lines := parseScanLines("match 120/230 matched:80 manual:10 unmatched:2 error:0 title\nfetch 40 nfo:510 poster:38 other title")
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/library_card.html", map[string]any{
		"Library":     db.Library{ID: 1, Name: "Lib"},
		"Lines":       lines,
		"Poll":        true,
		"Job":         &db.ScanJob{ID: 9, Status: "running", ProgressPct: 40},
		"ProgressPct": 40,
		"Total":       4439,
		"WatchPct":    0,
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Count(html, "scan-grid") != 2 {
		t.Fatalf("grids: %s", html)
	}
	if !strings.Contains(html, "match 120/230") || !strings.Contains(html, "fetch 40") {
		t.Fatalf("steps: %s", html)
	}
	if strings.Contains(html, "fetch 40/") {
		t.Fatalf("fetch denominator: %s", html)
	}
	if strings.Contains(html, "has-watch") || strings.Contains(html, "watched ") {
		t.Fatalf("watch during scan: %s", html)
	}

	buf.Reset()
	lines = parseScanLines("match 120/230 matched:80 manual:10 unmatched:2 error:0 title\nfetch 40 nfo:510 poster:38 other title\nstills 0/3 Legend of Crimson")
	if err := tpl.ExecuteTemplate(&buf, "partials/library_card.html", map[string]any{
		"Library":     db.Library{ID: 1, Name: "Lib"},
		"Lines":       lines,
		"Poll":        true,
		"Job":         &db.ScanJob{ID: 9, Status: "running", ProgressPct: 80},
		"ProgressPct": 80,
		"Total":       4439,
		"WatchPct":    0,
	}); err != nil {
		t.Fatal(err)
	}
	html = buf.String()
	if strings.Count(html, "scan-grid") != 3 {
		t.Fatalf("stills grids: %s", html)
	}
	if !strings.Contains(html, "stills 0/3") {
		t.Fatalf("stills step: %s", html)
	}

	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/library_card.html", map[string]any{
		"Library":  db.Library{ID: 1, Name: "Lib"},
		"Total":    4439,
		"WatchPct": 12,
	}); err != nil {
		t.Fatal(err)
	}
	idle := buf.String()
	if !strings.Contains(idle, "has-watch") || !strings.Contains(idle, "--watch-pct: 12%") {
		t.Fatalf("watch after scan: %s", idle)
	}
	if strings.Contains(idle, "watched ") {
		t.Fatalf("watch chip: %s", idle)
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
	if strings.Contains(html, `value="nfo"`) || strings.Contains(html, `name="persist"`) || strings.Contains(html, `name="overwrite"`) {
		t.Fatal("add-library still exposes nfo or sidecar checkboxes")
	}
	if !strings.Contains(html, `value="rescan"`) || !strings.Contains(html, `value="changes"`) || !strings.Contains(html, `value="missing"`) {
		t.Fatal("add-library mode values")
	}
	if !strings.Contains(html, `value="changes" selected`) {
		t.Fatal("changes selected when ready")
	}
	i := strings.Index(html, `id="add-library-dialog"`)
	j := strings.Index(html, `id="media-browser"`)
	if i < 0 || j < 0 || j < i {
		t.Fatal("folder browser must live inside add-library dialog")
	}
	if !strings.Contains(html, `hx-target="#media-browser"`) && !strings.Contains(html, `target: '#media-browser'`) {
		t.Fatal("media browser htmx target")
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
			{Item: db.MediaItem{ID: 7, Kind: "movie", Title: "Fresh Title"}, Href: "/libraries/1"},
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
	if strings.Contains(html, "/movies/") || strings.Contains(html, "/shows/") {
		t.Fatalf("recent catalog url: %s", html)
	}
}

func TestLibraryTemplatePosterDialog(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "library.html", map[string]any{
		"Library":   db.Library{ID: 1, Name: "Lib"},
		"ItemCount": 0,
		"Q":         "",
		"Sort":      "name",
		"Path":      "",
		"Poll":      false,
		"Tree":      true,
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="poster-edit-dialog"`) || !strings.Contains(html, `id="poster-edit-generate"`) {
		t.Fatalf("poster dialog: %s", html)
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
		"Items":              []itemCard{{Item: db.MediaItem{ID: 9, Kind: "show", Title: "New Show"}, Href: "/libraries/3/tree?path=New%20Show"}},
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
