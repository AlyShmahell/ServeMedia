package fetch

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestFillSeasonNumbersDetectorists(t *testing.T) {
	show := filepath.Join("/media", "Detectorists")
	counts := []int{6, 7, 6}
	var files []matchmedia.JobFile
	var want []matchmedia.JobFile
	for si, n := range counts {
		for ep := 1; ep <= n; ep++ {
			path := filepath.Join(show, fmt.Sprintf("Season %d", si+1), fmt.Sprintf("Detectorists %d%02d.mp4", si+1, ep))
			files = append(files, matchmedia.JobFile{Path: path})
			want = append(want, matchmedia.JobFile{Path: path, Season: strconv.Itoa(si + 1), Episode: strconv.Itoa(ep)})
		}
	}
	got := fillSeasonNumbers(show, files)
	if len(got) != len(want) {
		t.Fatalf("got %d files", len(got))
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].Season != want[i].Season || got[i].Episode != want[i].Episode {
			t.Fatalf("file %d = %#v want %#v", i, got[i], want[i])
		}
	}
}

func TestFillSeasonNumbersPreservesProvidedNumbers(t *testing.T) {
	show := filepath.Join("/media", "Show")
	files := []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 2", "whatever.mkv"), Season: "2", Episode: "05"},
		{Path: filepath.Join(show, "Season 2", "Show.S02E06.mkv"), Season: "2"},
		{Path: filepath.Join(show, "Season 2", "Show.S02E07.mkv"), Episode: "7"},
		{Path: filepath.Join(show, "Season 2", "Show.S02E08.mkv")},
	}
	got := fillSeasonNumbers(show, files)
	if got[0] != files[0] {
		t.Fatalf("provider numbers changed: %#v", got[0])
	}
	if got[1].Season != "2" || got[1].Episode != "" {
		t.Fatalf("partial season changed: %#v", got[1])
	}
	if got[2].Season != "" || got[2].Episode != "7" {
		t.Fatalf("partial episode changed: %#v", got[2])
	}
	if got[3].Season != "2" || got[3].Episode != "8" {
		t.Fatalf("full-missing file not filled: %#v", got[3])
	}
}

func TestFillSeasonNumbersConflictsStayUnnumbered(t *testing.T) {
	show := filepath.Join("/media", "Show")
	nfo := filepath.Join(t.TempDir(), "Show.nfo")
	if err := os.WriteFile(nfo, []byte("<episodedetails><season>2</season><episode>4</episode></episodedetails>"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 2", "Show.S03E01.mkv")},
		{Path: filepath.Join(show, "Season 3", "Show.S03E01.mkv"), NFOPath: nfo},
	}
	got := fillSeasonNumbers(show, files)
	for i, f := range got {
		if f.Season != "" || f.Episode != "" {
			t.Fatalf("conflict %d numbered: %#v", i, f)
		}
	}
}

func TestFillSeasonNumbersUsesNFOSidecar(t *testing.T) {
	show := filepath.Join(t.TempDir(), "Show")
	dir := t.TempDir()
	nfo := filepath.Join(dir, "file.nfo")
	if err := os.WriteFile(nfo, []byte("<episodedetails><season>1</season><episode>4</episode></episodedetails>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fillSeasonNumbers(show, []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 1", "file.mkv"), NFOPath: nfo},
	})
	if got[0].Season != "1" || got[0].Episode != "4" {
		t.Fatalf("nfo not used: %#v", got[0])
	}

	beside := filepath.Join(show, "Season 1", "file.nfo")
	if err := os.MkdirAll(filepath.Dir(beside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(beside, []byte("<episodedetails><season>1</season><episode>9</episode></episodedetails>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = fillSeasonNumbers(show, []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 1", "file.mkv")},
	})
	if got[0].Season != "" || got[0].Episode != "" {
		t.Fatalf("sidecar scanned without NFOPath: %#v", got[0])
	}
}

func TestFillSeasonNumbersSkipsExtras(t *testing.T) {
	show := filepath.Join("/media", "Show")
	files := []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 1", "Show 101 sample.mkv")},
		{Path: filepath.Join(show, "Season 1", "Show trailer 102.mkv")},
		{Path: filepath.Join(show, "Season 1", "behind the scenes 103.mkv")},
		{Path: filepath.Join(show, "Season 1", "deleted scenes 104.mkv")},
		{Path: filepath.Join(show, "Season 1", "interviews 105.mkv")},
	}
	got := fillSeasonNumbers(show, files)
	for i, f := range got {
		if f.Season != "" || f.Episode != "" {
			t.Fatalf("extra %d numbered: %#v", i, f)
		}
	}
}

func TestFillSeasonNumbersWrapperDirs(t *testing.T) {
	show := filepath.Join("/media", "Show")
	got := fillSeasonNumbers(show, []matchmedia.JobFile{
		{Path: filepath.Join(show, "1080p", "Season 1", "Show 101.mkv")},
		{Path: filepath.Join(show, "Season 1", "Disc 1", "Show 102.mkv")},
	})
	if got[0].Season != "1" || got[0].Episode != "1" {
		t.Fatalf("wrapper %#v", got[0])
	}
	if got[1].Season != "1" || got[1].Episode != "2" {
		t.Fatalf("disc wrapper %#v", got[1])
	}
}

func TestFillSeasonNumbersFlatShow(t *testing.T) {
	show := filepath.Join("/media", "Show")
	got := fillSeasonNumbers(show, []matchmedia.JobFile{
		{Path: filepath.Join(show, "Show.S02E03.mkv")},
		{Path: filepath.Join(show, "Unmarked Movie.mkv")},
	})
	if got[0].Season != "2" || got[0].Episode != "3" {
		t.Fatalf("flat marker %#v", got[0])
	}
	if got[1].Season != "" || got[1].Episode != "" {
		t.Fatalf("flat file numbered: %#v", got[1])
	}
}

func TestFillSeasonNumbersRejectsAmbiguousShapes(t *testing.T) {
	show := filepath.Join("/media", "Show")
	files := []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 1-3", "Show 101.mkv")},
		{Path: filepath.Join(show, "Specials", "Show.S00E01.mkv")},
		{Path: filepath.Join(show, "Season 101", "Show 101.mkv")},
		{Path: filepath.Join(show, "Show.2019.mkv")},
		{Path: filepath.Join(show, "Season 1", "Show.1001.mkv")},
		{Path: filepath.Join(show, "Season 1", "Show 100.mkv")},
	}
	got := fillSeasonNumbers(show, files)
	for i, f := range got {
		if f.Season != "" || f.Episode != "" {
			t.Fatalf("shape %d numbered: %#v", i, f)
		}
	}
	got = fillSeasonNumbers(show, []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 1", "Show 199.mkv")},
	})
	if got[0].Season != "1" || got[0].Episode != "99" {
		t.Fatalf("199 bound %#v", got[0])
	}
}

func TestFillSeasonNumbersIgnoresResolutionAndYear(t *testing.T) {
	show := filepath.Join("/media", "Show")
	files := []matchmedia.JobFile{
		{Path: filepath.Join(show, "Season 4", "Show.480p.mkv")},
		{Path: filepath.Join(show, "Season 7", "Show.720p.mkv")},
		{Path: filepath.Join(show, "Season 10", "Show.1080p.mkv")},
		{Path: filepath.Join(show, "Season 20", "Show.2010.mkv")},
		{Path: filepath.Join(show, "Season 1", "Show 101.mkv")},
	}
	got := fillSeasonNumbers(show, files)
	for i := 0; i < 4; i++ {
		if got[i].Season != "" || got[i].Episode != "" {
			t.Fatalf("file %d numbered: %#v", i, got[i])
		}
	}
	if got[4].Season != "1" || got[4].Episode != "1" {
		t.Fatalf("real episode %#v", got[4])
	}
}

func TestParseSeasonDirName(t *testing.T) {
	tests := []struct {
		name string
		want int
		ok   bool
	}{
		{"Season 1", 1, true},
		{"Season.2", 2, true},
		{"Season_3", 3, true},
		{"S01", 1, true},
		{"S4", 4, true},
		{"Series 5", 5, true},
		{" Season 6 (2018) ", 6, true},
		{"season 07", 7, true},
		{"Season 1-3", 0, false},
		{"Seasons 1-3", 0, false},
		{"Complete Series", 0, false},
		{"Specials", 0, false},
		{"S00", 0, false},
		{"Extras", 0, false},
		{"Season Highlights", 0, false},
		{"S01E01", 0, false},
		{"Season 101", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseSeasonDirName(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseSeasonDirName(%q) = %d,%v want %d,%v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseEpisodeMarker(t *testing.T) {
	tests := []struct {
		name          string
		season, epNum int
		ok            bool
	}{
		{"Show.S01E02.mkv", 1, 2, true},
		{"s1e2", 1, 2, true},
		{"Show 1x02.mkv", 1, 2, true},
		{"Show.12x003.mkv", 12, 3, true},
		{"Show.S01E02.1080p", 1, 2, true},
		{"Show.S00E01.mkv", 0, 0, false},
		{"Show.S01E00.mkv", 0, 0, false},
		{"Show.S01E1000.mkv", 0, 0, false},
		{"Show 1920x1080.mkv", 0, 0, false},
		{"Show.mkv", 0, 0, false},
	}
	for _, tt := range tests {
		season, epNum, ok := parseEpisodeMarker(tt.name)
		if season != tt.season || epNum != tt.epNum || ok != tt.ok {
			t.Errorf("parseEpisodeMarker(%q) = %d,%d,%v want %d,%d,%v", tt.name, season, epNum, ok, tt.season, tt.epNum, tt.ok)
		}
	}
}

func TestCountFilledNumbers(t *testing.T) {
	before := []matchmedia.JobFile{
		{},
		{Season: "1"},
		{Season: "1", Episode: "2"},
	}
	after := []matchmedia.JobFile{
		{Season: "1", Episode: "1"},
		{Season: "1", Episode: "2"},
		{Season: "1", Episode: "2"},
	}
	if got := countFilledNumbers(before, after); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestSeasonFoldersIngestDetectorists(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Detectorists")
	counts := []int{6, 7, 6}
	var files []matchmedia.JobFile
	for si, n := range counts {
		for ep := 1; ep <= n; ep++ {
			path := filepath.Join(showDir, fmt.Sprintf("Season %d", si+1), fmt.Sprintf("Detectorists %d%02d.mp4", si+1, ep))
			touch(t, path)
			files = append(files, matchmedia.JobFile{Path: path})
		}
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Detectorists", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ingestShowFiles(ctx, w, showID, fillSeasonNumbers(showDir, files)); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 19 {
		t.Fatalf("got %d episodes", len(eps))
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	seasonNum := map[int64]int{}
	for _, se := range seasons {
		seasonNum[se.ID] = se.SeasonNumber
	}
	got := map[int]int{}
	for _, ep := range eps {
		got[seasonNum[ep.SeasonID]]++
	}
	if got[1] != 6 || got[2] != 7 || got[3] != 6 {
		t.Fatalf("season counts %#v", got)
	}
}

func TestApplyShowStampsExclusiveSeasonDir(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Detectorists")
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Detectorists", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	cat := matchmedia.Catalog{
		Title: "Detectorists", Synopsis: "show plot",
		Seasons: []matchmedia.Season{
			{Number: "1", Title: "Season One", Synopsis: "catalog one"},
			{Number: "2", Title: "Season Two", Synopsis: "catalog two"},
		},
	}
	nodeIDs := map[int]int64{}
	for season := 1; season <= 2; season++ {
		seasonDir := filepath.Join(showDir, fmt.Sprintf("Season %d", season))
		epPath := filepath.Join(seasonDir, fmt.Sprintf("Detectorists %d01.mp4", season))
		touch(t, epPath)
		nfo := filepath.Join(seasonDir, "season.nfo")
		if err := os.WriteFile(nfo, []byte(fmt.Sprintf("<season><plot>nfo plot %d</plot></season>", season)), 0o644); err != nil {
			t.Fatal(err)
		}
		sid, err := d.UpsertSeason(ctx, showID, season, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.UpsertEpisode(ctx, db.Episode{
			SeasonID: sid, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1,
		}); err != nil {
			t.Fatal(err)
		}
		id, err := d.UpsertNodeChain(ctx, lib.ID, fmt.Sprintf("Detectorists/Season %d", season), "dir")
		if err != nil {
			t.Fatal(err)
		}
		nodeIDs[season] = id
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.applyShow(ctx, it, matchmedia.Job{}, cat, ""); err != nil {
		t.Fatal(err)
	}
	for season := 1; season <= 2; season++ {
		seasonDir := filepath.Join(showDir, fmt.Sprintf("Season %d", season))
		if _, err := os.Stat(filepath.Join(seasonDir, "season.nfo")); err != nil {
			t.Fatalf("season %d nfo: %v", season, err)
		}
		node, err := d.GetLibraryNode(ctx, nodeIDs[season])
		if err != nil || node == nil {
			t.Fatalf("season %d node: %v", season, err)
		}
		if node.MMTitle.String != fmt.Sprintf("Season %s", []string{"", "One", "Two"}[season]) {
			t.Fatalf("season %d title %q", season, node.MMTitle.String)
		}
		if node.Plot.String != fmt.Sprintf("nfo plot %d", season) {
			t.Fatalf("season %d plot %q", season, node.Plot.String)
		}
		body, err := os.ReadFile(filepath.Join(seasonDir, "season.nfo"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, fmt.Sprintf("nfo plot %d", season)) || strings.Contains(text, "catalog") {
			t.Fatalf("season %d file plot %s", season, text)
		}
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 2 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	for _, season := range seasons {
		if season.Plot.String != fmt.Sprintf("nfo plot %d", season.SeasonNumber) {
			t.Fatalf("season row %d plot %q", season.SeasonNumber, season.Plot.String)
		}
	}
	if _, err := os.Stat(filepath.Join(showDir, "season.nfo")); !os.IsNotExist(err) {
		t.Fatalf("show root season.nfo should not exist: %v", err)
	}
}

func TestApplyShowSkipsFlatSeasonWrites(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Flat Show")
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Flat Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := d.UpsertNodeChain(ctx, lib.ID, "Flat Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	for season := 1; season <= 2; season++ {
		epPath := filepath.Join(showDir, fmt.Sprintf("Flat Show.S%02dE01.mkv", season))
		touch(t, epPath)
		sid, err := d.UpsertSeason(ctx, showID, season, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.UpsertEpisode(ctx, db.Episode{
			SeasonID: sid, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	cat := matchmedia.Catalog{
		Title: "Flat Show", Synopsis: "show plot",
		Seasons: []matchmedia.Season{
			{Number: "1", Synopsis: "season plot one"},
			{Number: "2", Synopsis: "season plot two"},
		},
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.applyShow(ctx, it, matchmedia.Job{}, cat, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(showDir, "season.nfo")); !os.IsNotExist(err) {
		t.Fatalf("flat show root season.nfo should not exist: %v", err)
	}
	node, err := d.GetLibraryNode(ctx, nodeID)
	if err != nil || node == nil {
		t.Fatalf("show root node: %v", err)
	}
	if node.Plot.String != "show plot" {
		t.Fatalf("flat show root plot %q, want show plot", node.Plot.String)
	}
}
