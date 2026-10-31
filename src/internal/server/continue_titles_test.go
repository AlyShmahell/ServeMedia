package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestContinueWatchingPrefersNFOTitle(t *testing.T) {
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
	media := t.TempDir()
	lib, err := d.CreateLibrary(ctx, u.ID, "TV", media)
	if err != nil {
		t.Fatal(err)
	}

	// File node with a sidecar NFO: the NFO title wins over the stored name.
	nodeDir := filepath.Join(media, "Colony", "Season 1")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(nodeDir, "Colony.S01E07.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "Colony.S01E07.nfo"),
		[]byte(`<episodedetails><title>Broussard</title><season>1</season><episode>7</episode></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := "Colony/Season 1/Colony.S01E07.mkv"
	nodeID, err := d.UpsertNodeChain(ctx, lib.ID, rel, "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, nodeID, 0, "1", "7", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchNode(ctx, u.ID, nodeID, 30, 100, false); err != nil {
		t.Fatal(err)
	}

	// Episode row with an absolute NFO path.
	showDir := filepath.Join(media, "Show")
	epDir := filepath.Join(showDir, "Season 1")
	if err := os.MkdirAll(epDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epVideo := filepath.Join(epDir, "Show.S01E02.mkv")
	if err := os.WriteFile(epVideo, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	epNFO := filepath.Join(epDir, "Show.S01E02.nfo")
	if err := os.WriteFile(epNFO, []byte(`<episodedetails><title>Episode NFO Title</title></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
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
	epID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 2, Path: epVideo, Mtime: 1,
		Title:   sql.NullString{String: "Stored Episode", Valid: true},
		NFOPath: sql.NullString{String: epNFO, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchEpisode(ctx, u.ID, epID, 20, 100, false); err != nil {
		t.Fatal(err)
	}

	// A node with no NFO keeps its stored/fallback title.
	bareID, err := d.UpsertNodeChain(ctx, lib.ID, "Bare/No.NFO.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, bareID, 0, "1", "1", "Stored Bare", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchNode(ctx, u.ID, bareID, 10, 100, false); err != nil {
		t.Fatal(err)
	}

	s := &Server{DB: d}
	rows, err := s.continueWatching(ctx, u.ID, 12)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.Kind+":"+row.PlayURL] = row.Title
	}
	var nodeTitle, epTitle, bareTitle string
	for key, title := range got {
		switch {
		case strings.Contains(key, "/play/file/"):
			if title == "Broussard" {
				nodeTitle = title
			}
			if title == "Stored Bare" {
				bareTitle = title
			}
		case strings.Contains(key, "/play/episode/"):
			epTitle = title
		}
	}
	if nodeTitle != "Broussard" {
		t.Fatalf("node title %q rows %#v", nodeTitle, got)
	}
	if epTitle != "Episode NFO Title" {
		t.Fatalf("episode title %q rows %#v", epTitle, got)
	}
	if bareTitle != "Stored Bare" {
		t.Fatalf("bare title %q rows %#v", bareTitle, got)
	}
}
