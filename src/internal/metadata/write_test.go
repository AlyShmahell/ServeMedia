package metadata

import (
	"path/filepath"
	"testing"
)

func TestWriteSeasonEpisodeNFO(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "season.nfo")
	if err := WriteSeasonNFO(sp, SeasonNFO{Title: "Season 1", Plot: "plot"}); err != nil {
		t.Fatal(err)
	}
	n, err := ReadSeasonNFO(sp)
	if err != nil || n.Title != "Season 1" {
		t.Fatalf("%v %#v", err, n)
	}
	ep := filepath.Join(dir, "episode.nfo")
	if err := WriteEpisodeNFO(ep, EpisodeNFO{Title: "Pilot", Season: 1, Episode: 1}); err != nil {
		t.Fatal(err)
	}
	e, err := ReadEpisodeNFO(ep)
	if err != nil || e.Title != "Pilot" {
		t.Fatalf("%v %#v", err, e)
	}
}
