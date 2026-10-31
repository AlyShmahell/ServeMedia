package fetch

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func writeNFO(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRecordTreeStoresSidecarTitleOverCatalog(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Show")
	epDir := filepath.Join(showDir, "Season 1")
	video := filepath.Join(epDir, "Show.S01E01.mkv")
	touch(t, video)
	writeNFO(t, filepath.Join(epDir, "Show.S01E01.nfo"),
		`<episodedetails><title>Broussard</title><plot>NFO plot</plot><season>1</season><episode>1</episode></episodedetails>`)
	job := matchmedia.Job{
		Path: showDir, Kind: "show",
		Files: []matchmedia.JobFile{{Path: video, Season: "1", Episode: "1"}},
		Catalog: []matchmedia.Season{{
			Number:   "1",
			Episodes: []matchmedia.Episode{{Number: "1", Title: "Catalog Title", Synopsis: "Catalog plot"}},
		}},
	}
	if err := w.recordTree(ctx, lib, job, Opts{}); err != nil {
		t.Fatal(err)
	}
	id, err := d.NodeIDByRel(ctx, lib.ID, "Show/Season 1/Show.S01E01.mkv")
	if err != nil || id == 0 {
		t.Fatalf("node %d %v", id, err)
	}
	node, err := d.GetLibraryNode(ctx, id)
	if err != nil || node == nil {
		t.Fatal(err)
	}
	if node.FileTitle.String != "Broussard" {
		t.Fatalf("file title %q", node.FileTitle.String)
	}
	if node.FilePlot.String != "NFO plot" {
		t.Fatalf("file plot %q", node.FilePlot.String)
	}
}

func TestApplyEpisodeKeepsNFOTitleOverCatalog(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Show")
	epDir := filepath.Join(showDir, "Season 1")
	video := filepath.Join(epDir, "Show.S01E01.mkv")
	touch(t, video)
	nfo := filepath.Join(epDir, "Show.S01E01.nfo")
	writeNFO(t, nfo, `<episodedetails><title>Broussard</title><season>1</season><episode>1</episode></episodedetails>`)
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: video, Mtime: 1,
		Title: sql.NullString{String: "Old Title", Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	w.applyEpisode(ctx, show, &seasons[0], &eps[0], &matchmedia.Episode{Title: "Catalog Title"}, "", "")
	got, err := d.GetEpisode(ctx, eps[0].ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title.String != "Broussard" {
		t.Fatalf("episode title %q", got.Title.String)
	}
	n, err := metadata.ReadEpisodeNFO(nfo)
	if err != nil || n == nil || n.Title != "Broussard" {
		t.Fatalf("nfo title %#v %v", n, err)
	}
}

func TestApplyEpisodeKeepsNFOTitleThatMatchesFilename(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Show")
	epDir := filepath.Join(showDir, "Season 1")
	video := filepath.Join(epDir, "Pilot.mkv")
	touch(t, video)
	nfo := filepath.Join(epDir, "Pilot.nfo")
	writeNFO(t, nfo, `<episodedetails><title>Pilot</title><season>1</season><episode>1</episode></episodedetails>`)
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: video, Mtime: 1,
		Title: sql.NullString{String: "The Pilot Episode", Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	w.applyEpisode(ctx, show, &seasons[0], &eps[0], &matchmedia.Episode{Title: "Catalog Title"}, "", "")
	got, err := d.GetEpisode(ctx, eps[0].ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title.String != "Pilot" {
		t.Fatalf("episode title %q", got.Title.String)
	}
	n, err := metadata.ReadEpisodeNFO(nfo)
	if err != nil || n == nil || n.Title != "Pilot" {
		t.Fatalf("nfo title %#v %v", n, err)
	}
}

func TestApplyEpisodeDoesNotCopySharedEpisodeNFO(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	showDir := filepath.Join(media, "Show")
	epDir := filepath.Join(showDir, "Season 1")
	writeNFO(t, filepath.Join(epDir, "episode.nfo"), `<episodedetails><title>Shared</title><season>1</season><episode>1</episode></episodedetails>`)
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var videos []string
	for _, name := range []string{"One.mkv", "Two.mkv"} {
		video := filepath.Join(epDir, name)
		touch(t, video)
		videos = append(videos, video)
		if _, err := d.UpsertEpisode(ctx, db.Episode{
			SeasonID: seasonID, ShowID: showID, EpisodeNumber: len(videos), Path: video, Mtime: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		t.Fatal(err)
	}
	seasons, err := d.ListSeasons(ctx, showID)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	eps, err := d.ListEpisodes(ctx, seasons[0].ID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	for i := range eps {
		w.applyEpisode(ctx, show, &seasons[0], &eps[i], &matchmedia.Episode{Title: "Catalog Title"}, "", "")
	}
	shared, err := os.ReadFile(filepath.Join(epDir, "episode.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shared), "Shared") {
		t.Fatalf("shared nfo changed: %s", shared)
	}
	for _, video := range videos {
		beside := strings.TrimSuffix(video, filepath.Ext(video)) + ".nfo"
		n, err := metadata.ReadEpisodeNFO(beside)
		if err != nil || n == nil {
			t.Fatalf("beside %s: %#v %v", beside, n, err)
		}
		if n.Title == "Shared" {
			t.Fatalf("copied shared title into %s", beside)
		}
	}
}

func TestJobTitlePrefersNFO(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Scene.Name.mkv")
	touch(t, video)
	writeNFO(t, filepath.Join(dir, "tvshow.nfo"), `<tvshow><title>NFO Show</title></tvshow>`)
	job := matchmedia.Job{Path: dir, Title: "Catalog Show"}
	if got := jobTitle(job, video); got != "NFO Show" {
		t.Fatalf("job title %q", got)
	}
	writeNFO(t, filepath.Join(dir, "tvshow.nfo"), `<tvshow><title> </title></tvshow>`)
	if got := jobTitle(job, video); got != "Catalog Show" {
		t.Fatalf("fallback title %q", got)
	}
}
