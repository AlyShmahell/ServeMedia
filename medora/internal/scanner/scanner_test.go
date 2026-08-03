package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLooksLikeShowDir_strayNFOFilm(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Stray NFO Film (2007)")
	touch(t, filepath.Join(film, "tvshow.nfo"))
	touch(t, filepath.Join(film, "Stray NFO Film (2007).mp4"))
	if looksLikeShowDir(film) {
		t.Fatal("single-video film with stray tvshow.nfo must not be a show")
	}
}

func TestLooksLikeShowDir_seasonChildPack(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Season Pack Show")
	touch(t, filepath.Join(show, "Season 2", "ep-a.mp4"))
	touch(t, filepath.Join(show, "Season 2", "ep-b.mp4"))
	if !looksLikeShowDir(show) {
		t.Fatal("expected season child pack to be a show")
	}
}

func TestLooksLikeShowDir_packShowSxxEyy(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Pack Show")
	touch(t, filepath.Join(show, "[Grp] Pack Show S01", "[Grp] Pack Show S01E01.mp4"))
	if !looksLikeShowDir(show) {
		t.Fatal("expected nested SxxEyy pack to be a show")
	}
}

func TestLooksLikeShowDir_filmPlusTrailerNotShow(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Plain Film (2024)")
	touch(t, filepath.Join(film, "Plain Film (2024).mp4"))
	touch(t, filepath.Join(film, "Plain Film (2024)-trailer.mp4"))
	if looksLikeShowDir(film) {
		t.Fatal("film + trailer must not be a show")
	}
}

func TestLooksLikeShowDir_flatDashPack(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Flat Dash Show")
	touch(t, filepath.Join(show, "[Grp] Flat Dash Show - 01.mkv"))
	touch(t, filepath.Join(show, "[Grp] Flat Dash Show - 02.mkv"))
	touch(t, filepath.Join(show, "[Grp] Flat Dash Show - 03.mkv"))
	if !looksLikeShowDir(show) {
		t.Fatal("flat multi-ep pack without SxxEyy must be a show")
	}
}

func TestLooksLikeShowDir_nestedCourPack(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Nested Cour Show")
	touch(t, filepath.Join(show, "Nested Cour Show", "[Grp] Nested Cour Show - 01.mkv"))
	touch(t, filepath.Join(show, "Nested Cour Show", "[Grp] Nested Cour Show - 02.mkv"))
	if !looksLikeShowDir(show) {
		t.Fatal("single nested cour with 2+ videos must be a show")
	}
}

func TestLooksLikeShowDir_extrasSiblingPack(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Extras Sibling Show")
	touch(t, filepath.Join(show, "Extras Sibling Show - 01.mkv"))
	touch(t, filepath.Join(show, "Extras Sibling Show - 02.mkv"))
	touch(t, filepath.Join(show, "Extras", "Extras Sibling Show - NCOP1.mkv"))
	if !looksLikeShowDir(show) {
		t.Fatal("root episodes + Extras NCOP must still be a show")
	}
}

func TestIngestAnimeEpisodes_flatDashAndExtras(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Extras Sibling Show")
	paths := []string{
		filepath.Join(show, "Extras Sibling Show - 01.mkv"),
		filepath.Join(show, "Extras Sibling Show - 02.mkv"),
		filepath.Join(show, "Extras", "Extras Sibling Show - NCOP1.mkv"),
	}
	for _, p := range paths {
		touch(t, p)
	}
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, paths); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("expected 2 episodes (NCOP skipped), got %#v err=%v", eps, err)
	}
}

func TestFranchisePack_detectionAndScan(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "Franchise Pack Show")
	touch(t, filepath.Join(pack, "Main Series Show", "Season 1", "Main - 01.mkv"))
	touch(t, filepath.Join(pack, "Main Series Show", "Season 2", "Main - 01.mkv"))
	touch(t, filepath.Join(pack, "Spinoff Series Show", "Season 1", "S01E01.mkv"))
	touch(t, filepath.Join(pack, "Spinoff Series Show", "Season 1", "S01E02.mkv"))

	if !isFranchisePack(pack) {
		t.Fatal("expected franchise pack")
	}
	roots := expandShowRoots(pack)
	if len(roots) != 2 {
		t.Fatalf("expected 2 show roots, got %#v", roots)
	}

	// Dual Show: season children only — not a franchise.
	dual := filepath.Join(root, "Dual Show")
	touch(t, filepath.Join(dual, "tvshow.nfo"))
	touch(t, filepath.Join(dual, "Season 1", "S01E01.mkv"))
	touch(t, filepath.Join(dual, "Season 2", "S02E01.mkv"))
	if isFranchisePack(dual) {
		t.Fatal("Dual Show must not be a franchise")
	}

	// Complex Show: irregular cours, no strong nested — not a franchise.
	complexShow := filepath.Join(root, "Complex Show")
	touch(t, filepath.Join(complexShow, "tvshow.nfo"))
	touch(t, filepath.Join(complexShow, "Cour One", "Complex Show S04E01.mkv"))
	touch(t, filepath.Join(complexShow, "Cour Two", "Complex Show - 01.mkv"))
	touch(t, filepath.Join(complexShow, "Cour Three", "Complex Show - 01.mkv"))
	if isFranchisePack(complexShow) {
		t.Fatal("Complex Show must not be a franchise")
	}

	// Season Pack Show: Season 2 child only — not a franchise.
	seasonPack := filepath.Join(root, "Season Pack Show")
	touch(t, filepath.Join(seasonPack, "Season 2", "ep-a.mkv"))
	touch(t, filepath.Join(seasonPack, "Season 2", "ep-b.mkv"))
	if isFranchisePack(seasonPack) {
		t.Fatal("Season Pack Show must not be a franchise")
	}

	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	libPack := filepath.Join(media, "Franchise Pack Show")
	touch(t, filepath.Join(libPack, "Main Series Show", "Season 1", "Main - 01.mkv"))
	touch(t, filepath.Join(libPack, "Main Series Show", "Season 2", "Main - 01.mkv"))
	touch(t, filepath.Join(libPack, "Spinoff Series Show", "Season 1", "S01E01.mkv"))
	touch(t, filepath.Join(libPack, "Spinoff Series Show", "Season 1", "S01E02.mkv"))

	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	sc.ScanLibrary(ctx, lib, jobID)
	items, err := d.ListMediaItems(ctx, lib.ID, "name", "")
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, it := range items {
		titles[it.Title] = it.Kind
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 shows from franchise, got %#v", titles)
	}
	if titles["Main Series Show"] != "show" || titles["Spinoff Series Show"] != "show" {
		t.Fatalf("unexpected items: %#v", titles)
	}
	if _, ok := titles["Franchise Pack Show"]; ok {
		t.Fatal("franchise parent must not be a media item")
	}
}

func TestLooksLikeShowDir_dualShowNFO(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Dual Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	touch(t, filepath.Join(show, "Season 1", "S01E01.mp4"))
	touch(t, filepath.Join(show, "Season 1", "alt-01.mp4"))
	if !looksLikeShowDir(show) {
		t.Fatal("expected classic nfo show")
	}
}

func TestResolveTVEpisode_singleFileOVA(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Single File Show")
	vid := filepath.Join(show, "Single File Show.mkv")
	touch(t, vid)
	s, e, ok := resolveTVEpisode(show, vid)
	if !ok || s != 1 || e != 1 {
		t.Fatalf("got S%02dE%02d ok=%v", s, e, ok)
	}
}

func TestResolveTVEpisode_parseSxxEyy(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Some Show")
	vid := filepath.Join(show, "Season 1", "Some Show S01E02.mkv")
	touch(t, vid)
	touch(t, filepath.Join(show, "Season 1", "Some Show S01E01.mkv"))
	s, e, ok := resolveTVEpisode(show, vid)
	if !ok || s != 1 || e != 2 {
		t.Fatalf("got S%02dE%02d ok=%v", s, e, ok)
	}
}

func TestResolveTVEpisode_multiUnmarkedSkipped(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Ambiguous")
	a := filepath.Join(show, "part-a.mkv")
	b := filepath.Join(show, "part-b.mkv")
	touch(t, a)
	touch(t, b)
	_, _, ok := resolveTVEpisode(show, a)
	if ok {
		t.Fatal("multi unmarked videos must not invent S01E01")
	}
}

func TestIngestAnimeEpisodes_sequentialFallback(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Flat Pack Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	a := filepath.Join(show, "ep-a.mkv")
	b := filepath.Join(show, "ep-b.mkv")
	touch(t, a)
	touch(t, b)

	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, []string{b, a}); err != nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 || seasons[0].SeasonNumber != 1 {
		t.Fatalf("seasons=%#v err=%v", seasons, err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("eps=%#v err=%v", eps, err)
	}
	byPath := map[string]int{}
	for _, ep := range eps {
		byPath[ep.Path] = ep.EpisodeNumber
	}
	if byPath[a] != 1 || byPath[b] != 2 {
		t.Fatalf("expected sorted S01E01/E02, got %#v", byPath)
	}
}

func TestIngestMovie_preservesMetaMatched(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Film Title (2016)")
	vid := filepath.Join(showDir, "Film Title (2016).mkv")
	touch(t, vid)
	touch(t, filepath.Join(showDir, "movie.nfo"))
	touch(t, filepath.Join(showDir, "poster.jpg"))
	if err := os.WriteFile(filepath.Join(showDir, "movie.nfo"), []byte(
		`<?xml version="1.0"?><movie><title>Wrong NFO Title</title><year>2020</year></movie>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film Title.", SortTitle: "Film Title.",
		Path: vid, Mtime: 1,
		PosterPath: sql.NullString{String: "metadata/movies/Film Title. (2016)/poster.jpg", Valid: true},
		MetaID:     sql.NullString{String: "tt1111111", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.UpdateMediaItemMeta(ctx, id, "Film Title.", 2016, "plot",
		"metadata/movies/Film Title. (2016)/poster.jpg", "", "", 0, "omdb", "tt1111111")

	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	if err := sc.ingestMovie(ctx, lib, vid); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title != "Film Title." || !got.MetaID.Valid || got.MetaID.String != "tt1111111" {
		t.Fatalf("meta poisoned on scan: %#v", got)
	}
	if !got.PosterPath.Valid || got.PosterPath.String != "metadata/movies/Film Title. (2016)/poster.jpg" {
		t.Fatalf("poster overwritten: %q", got.PosterPath.String)
	}
}

func TestIngestAnimeEpisodes_irregularSeasonDirs(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Complex Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	// Filename S04 must not collapse everything into season 4.
	paths := []string{
		filepath.Join(show, "Cour One", "Complex Show S04E01.mkv"),
		filepath.Join(show, "Cour Two", "Complex Show - 01.mkv"),
		filepath.Join(show, "Cour Three", "Complex Show - 01.mkv"),
		filepath.Join(show, "OVA", "Complex Show OVA - 01.mkv"),
	}
	for _, p := range paths {
		touch(t, p)
	}
	if !looksLikeShowDir(show) {
		t.Fatal("expected irregular multi-cour tree to be a show")
	}

	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, paths); err != nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	nums := map[int]bool{}
	for _, se := range seasons {
		nums[se.SeasonNumber] = true
	}
	for _, want := range []int{0, 1, 2, 3} {
		if !nums[want] {
			t.Fatalf("missing season %d in %#v", want, nums)
		}
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 4 {
		t.Fatalf("expected 4 episodes, got %#v err=%v", eps, err)
	}
}

func TestEnsureSeason_showRootPoster(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Poster Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	touch(t, filepath.Join(show, "season02-poster.jpg"))
	ep := filepath.Join(show, "02. Cour", "Show - 01.mkv")
	touch(t, ep)

	store := t.TempDir()
	sc := &Scanner{DB: d, StorePath: store, MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, []string{ep}); err != nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons: %#v err=%v", seasons, err)
	}
	se := seasons[0]
	if se.SeasonNumber != 2 {
		t.Fatalf("season number: %d", se.SeasonNumber)
	}
	if !se.PosterPath.Valid || se.PosterPath.String == "" {
		t.Fatal("expected show-root season02-poster.jpg to be copied")
	}
	if _, err := os.Stat(filepath.Join(store, se.PosterPath.String)); err != nil {
		t.Fatalf("poster file missing: %v", err)
	}
}

func TestIngestAnimeEpisodes_numberedDirsAndRootOVA(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Numbered Dir Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	touch(t, filepath.Join(show, "season01-poster.jpg"))
	paths := []string{
		filepath.Join(show, "01. Series Title", "Series Title - 01.mkv"),
		filepath.Join(show, "02. Series Title Arc", "Series Title - 01.mkv"),
		filepath.Join(show, "03. re Part 1", "Series Title - 01.mkv"),
		filepath.Join(show, "04. re Part 2", "Series Title - 01.mkv"),
		filepath.Join(show, "[X] Series Title - OVA 01 - Extra.mkv"),
		filepath.Join(show, "[X] Series Title - OVA 02 - Extra.mkv"),
	}
	for _, p := range paths {
		touch(t, p)
	}
	// Pretend old bad ingest put first ep under season 4 only.
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	s4, err := d.UpsertSeason(ctx, showID, 4, "Season 4", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s4, ShowID: showID, EpisodeNumber: 1, Path: paths[0], Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, paths); err != nil {
		t.Fatalf("rescan after season reassign: %v", err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	nums := map[int]bool{}
	for _, se := range seasons {
		nums[se.SeasonNumber] = true
		if se.SeasonNumber == 1 && (!se.PosterPath.Valid || se.PosterPath.String == "") {
			t.Fatal("season 1 should pick up show-root season01-poster.jpg")
		}
	}
	for _, want := range []int{0, 1, 2, 3, 4} {
		if !nums[want] {
			t.Fatalf("missing season %d in %#v", want, nums)
		}
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 6 {
		t.Fatalf("expected 6 episodes, got %d err=%v", len(eps), err)
	}
}

func TestIngestAnimeEpisodes_keepsSxxEyy(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Pack Show")
	touch(t, filepath.Join(show, "tvshow.nfo"))
	vid := filepath.Join(show, "Pack Show S01E02.mkv")
	loose := filepath.Join(show, "unmarked.mkv")
	touch(t, vid)
	touch(t, loose)

	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, []string{vid, loose}); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 || eps[0].EpisodeNumber != 2 {
		t.Fatalf("expected only S01E02, got %#v err=%v", eps, err)
	}
}

func TestIngestAnimeEpisodes_mixedParsedAndLoose(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Mixed Season Show")
	paths := []string{
		filepath.Join(show, "Season 1", "Show_-_01_.mkv"),
		filepath.Join(show, "Season 1", "Show_-_02_.mkv"),
		filepath.Join(show, "Season 1", "Show S01E11 OVA.mkv"),
	}
	for _, p := range paths {
		touch(t, p)
	}
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.ingestAnimeEpisodes(ctx, showID, show, paths); err != nil {
		t.Fatal(err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 3 {
		t.Fatalf("expected 3 episodes (loose+parsed), got %#v err=%v", eps, err)
	}
}

func TestCollectShowVideos_rootAndMoviesDir(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show With Films")
	touch(t, filepath.Join(show, "Season 1", "S01E01.mkv"))
	touch(t, filepath.Join(show, "Legend Film.mkv"))
	touch(t, filepath.Join(show, "Movies", "Pack Film.mkv"))
	eps := collectShowVideos(show)
	if len(eps) != 3 {
		t.Fatalf("episodes: %#v", eps)
	}
}

func TestScanAnime_filmsUnderShow(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "Show With Films")
	touch(t, filepath.Join(show, "Season 1", "S01E01.mkv"))
	touch(t, filepath.Join(show, "Legend Film.mkv"))
	touch(t, filepath.Join(show, "Movies", "Pack Film.mkv"))
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	sc.ScanLibrary(ctx, lib, jobID)
	items, err := d.ListMediaItems(ctx, lib.ID, "name", "")
	if err != nil {
		t.Fatal(err)
	}
	shows, movies := 0, 0
	var showID int64
	for _, it := range items {
		switch it.Kind {
		case "show":
			shows++
			showID = it.ID
		case "movie":
			movies++
		}
	}
	if shows != 1 || movies != 0 {
		t.Fatalf("want 1 show 0 movies, got shows=%d movies=%d items=%#v", shows, movies, items)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	var s0eps int
	for _, se := range seasons {
		if se.SeasonNumber != 0 {
			continue
		}
		eps, err := d.ListEpisodes(ctx, se.ID)
		if err != nil {
			t.Fatal(err)
		}
		s0eps = len(eps)
	}
	if s0eps != 2 {
		t.Fatalf("want 2 season-0 films, got %d seasons=%#v", s0eps, seasons)
	}
}

func TestRescanMediaItem_movie(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	film := filepath.Join(media, "Film (2020)")
	vid := filepath.Join(film, "Film (2020).mkv")
	touch(t, vid)
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	if err := sc.ingestMovie(ctx, lib, vid); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItemByPath(ctx, lib.ID, vid)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.RescanMediaItem(ctx, lib, it, jobID); err != nil {
		t.Fatal(err)
	}
}

func TestIsFilmPack(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "Film Pack")
	touch(t, filepath.Join(pack, "Film One", "Film One.mkv"))
	touch(t, filepath.Join(pack, "Film Two", "Film Two.mkv"))
	if !isFilmPack(pack) {
		t.Fatal("expected film pack")
	}
	if isFranchisePack(pack) {
		t.Fatal("film pack must not be franchise")
	}
}

func TestIsFranchisePack_showLikeOnly(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "Franchise")
	touch(t, filepath.Join(pack, "Show A", "Show.A.S01.1080p", "Show A S01E01.mkv"))
	touch(t, filepath.Join(pack, "Show B", "Show.B.S01.1080p", "Show B S01E01.mkv"))
	if !isFranchisePack(pack) {
		t.Fatal("expected franchise with mid-name seasons")
	}
	roots := expandShowRoots(pack)
	if len(roots) != 2 {
		t.Fatalf("roots: %#v", roots)
	}
}

func TestIsFranchisePack_seasonMoviesOVABuckets(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "Series Pack")
	touch(t, filepath.Join(pack, "Series Pack - Season 1", "ep01.mkv"))
	touch(t, filepath.Join(pack, "Series Pack - Season 2", "ep01.mkv"))
	touch(t, filepath.Join(pack, "[Group] Series Pack Movies", "film.mkv"))
	touch(t, filepath.Join(pack, "[Group] Series Pack OVAs", "ova.mkv"))
	if isFranchisePack(pack) {
		t.Fatal("season/Movies/OVAs buckets must not franchise-expand")
	}
	if !looksLikeShowDir(pack) {
		t.Fatal("parent should still look like one show")
	}
}

func TestIngestShow_cleansReleaseGroupTitle(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", media)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(media, "[Group] Series Title [1080p]")
	touch(t, filepath.Join(show, "Season 1", "S01E01.mkv"))
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	showID, err := sc.ingestShow(ctx, lib, show)
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.Title != "Series Title" {
		t.Fatalf("got title %q", it.Title)
	}
}

func TestCollectMovieFiles_multiVersionOne(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Movie (2020)")
	touch(t, filepath.Join(folder, "Movie (2020) - 1080p.mkv"))
	touch(t, filepath.Join(folder, "Movie (2020) - 2160p.mkv"))
	files := collectMovieFiles(root)
	if len(files) != 1 {
		t.Fatalf("want 1 primary, got %#v", files)
	}
}

func TestCollectMovieFiles_flatKeepsAll(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	_ = os.MkdirAll(movies, 0o755)
	touch(t, filepath.Join(movies, "Film A (2016).mp4"))
	touch(t, filepath.Join(movies, "Film B (2011).mp4"))
	files := collectMovieFiles(movies)
	if len(files) != 2 {
		t.Fatalf("want 2, got %#v", files)
	}
}

func TestScanTV_filmPack(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "TV", "tv", media)
	if err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(media, "Film Pack")
	touch(t, filepath.Join(pack, "Film One", "Film One.mkv"))
	touch(t, filepath.Join(pack, "Film Two", "Film Two.mkv"))
	sc := &Scanner{DB: d, StorePath: t.TempDir(), MediaRoot: media}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	sc.ScanLibrary(ctx, lib, jobID)
	items, err := d.ListMediaItems(ctx, lib.ID, "name", "")
	if err != nil {
		t.Fatal(err)
	}
	shows, movies := 0, 0
	for _, it := range items {
		switch it.Kind {
		case "show":
			shows++
		case "movie":
			movies++
		}
	}
	if shows != 0 || movies != 2 {
		t.Fatalf("want 0 shows 2 movies, got shows=%d movies=%d %#v", shows, movies, items)
	}
}
