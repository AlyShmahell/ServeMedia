package metadata_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/medora/internal/metadata"
)

func TestParseEpisode(t *testing.T) {
	s, e, ok := metadata.ParseEpisode("Show.Name.S01E02.mkv")
	if !ok || s != 1 || e != 2 {
		t.Fatalf("got %d %d %v", s, e, ok)
	}
}

func TestParseTitleYear(t *testing.T) {
	title, year := metadata.ParseTitleYear("Some Film (1999).mp4")
	if title == "" || year != 1999 {
		t.Fatalf("got %q %d", title, year)
	}
}

func TestTitleYearFromVideoPath_prefersFolder(t *testing.T) {
	path := "/media/Anime/Film Title (2016)/Film Title Wrong Stem.mkv"
	title, year := metadata.TitleYearFromVideoPath(path)
	if title != "Film Title" || year != 2016 {
		t.Fatalf("got %q %d want Film Title 2016", title, year)
	}
}

func TestTitleYearFromVideoPath_keepsTheMovie(t *testing.T) {
	path := "/media/anime/Some Film - The Movie (2016)/Some Film (2016).mkv"
	title, year := metadata.TitleYearFromVideoPath(path)
	if title != "Some Film - The Movie" || year != 2016 {
		t.Fatalf("got %q %d", title, year)
	}
}

func TestTitleYearFromVideoPath_filenameYearFallback(t *testing.T) {
	path := "/media/tv/Root Film/Root.Film.1994.1080p.mkv"
	title, year := metadata.TitleYearFromVideoPath(path)
	if title != "Root Film" || year != 1994 {
		t.Fatalf("got %q %d want Root Film 1994", title, year)
	}
}

func TestStripMovieTitleSuffix(t *testing.T) {
	if metadata.StripMovieTitleSuffix("Some Film - The Movie") != "Some Film" {
		t.Fatal("strip failed")
	}
}

func TestCleanEpisodeTitle(t *testing.T) {
	got := metadata.CleanEpisodeTitle("[Group] Episode Title [1080p][HEVC].mkv")
	if got != "Episode Title" {
		t.Fatalf("got %q", got)
	}
	got = metadata.CleanEpisodeTitle("[Group] Show S01E01 (1080p).mkv")
	if !strings.Contains(got, "Show S01E01") || strings.Contains(got, "Group") || strings.Contains(got, "1080p") {
		t.Fatalf("got %q", got)
	}
}

func TestIsMoviesFolderName(t *testing.T) {
	if !metadata.IsMoviesFolderName("Movies") || !metadata.IsMoviesFolderName("Film") {
		t.Fatal("exact Movies/Film should match")
	}
	if !metadata.IsMoviesFolderName("[Group] Series Pack Movies") {
		t.Fatal("plural Movies token should match")
	}
	if metadata.IsMoviesFolderName("Some Film - The Movie") {
		t.Fatal("title ending in The Movie must not be a Movies pack")
	}
}

func TestIsSpecialsFolderName_wordToken(t *testing.T) {
	if !metadata.IsSpecialsFolderName("OVAs") || !metadata.IsSpecialsFolderName("[Group] Series Pack OVAs") {
		t.Fatal("OVA/OVAs pack should match")
	}
	if !metadata.IsSpecialsFolderName("Specials") {
		t.Fatal("Specials exact")
	}
}

func TestIsSeasonFolderName_midSeasonName(t *testing.T) {
	if !metadata.IsSeasonFolderName("Show Name - Season 1") {
		t.Fatal("mid-name Season N")
	}
	sn, ok := metadata.ParseSeasonFolder("Show Name - Season 2")
	if !ok || sn != 2 {
		t.Fatalf("got %d %v", sn, ok)
	}
}

func TestIsVideo(t *testing.T) {
	if !metadata.IsVideo("a.mkv") || metadata.IsVideo("a.txt") {
		t.Fatal("ext check failed")
	}
}

func TestReadSeasonNFO(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "season.nfo")
	if err := os.WriteFile(p, []byte(`<?xml version="1.0"?>
<season>
  <title>Season One</title>
  <plot>Intro arcs.</plot>
</season>
`), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := metadata.ReadSeasonNFO(p)
	if err != nil || n.Title != "Season One" || n.Plot != "Intro arcs." {
		t.Fatalf("%+v %v", n, err)
	}
	if metadata.FindSeasonNFO(dir, 1) != p {
		t.Fatal("FindSeasonNFO")
	}
}
