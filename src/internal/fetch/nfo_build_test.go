package fetch

import (
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func TestMovieNFOKeepsBlankFacts(t *testing.T) {
	prev := &metadata.MovieNFO{Title: "Old", Genre: []string{"Drama"}, Runtime: 90, Language: "ja"}
	nfo := movieNFOFromCatalog("Film", 2016, "plot", map[string]string{"rating": "8.2"}, prev)
	if nfo.Title != "Film" || nfo.Year != 2016 || nfo.Plot != "plot" || nfo.Rating != 8.2 {
		t.Fatalf("%#v", nfo)
	}
	if len(nfo.Genre) != 1 || nfo.Genre[0] != "Drama" || nfo.Runtime != 90 || nfo.Language != "ja" {
		t.Fatalf("kept %#v", nfo)
	}
}

func TestEpisodeNFOFromCatalog(t *testing.T) {
	ce := &matchmedia.Episode{Synopsis: "A start", Runtime: "24", Aired: "2024-07-07"}
	nfo := episodeNFOFromCatalog("Pilot", 1, 1, ce, nil)
	if nfo.Plot != "A start" || nfo.Runtime != 24 || nfo.Aired != "2024-07-07" {
		t.Fatalf("%#v", nfo)
	}
	kept := episodeNFOFromCatalog("Pilot", 1, 1, &matchmedia.Episode{Runtime: "0"}, &metadata.EpisodeNFO{Plot: "Keep", Runtime: 22, Aired: "2020-01-02"})
	if kept.Plot != "Keep" || kept.Runtime != 22 || kept.Aired != "2020-01-02" {
		t.Fatalf("kept %#v", kept)
	}
	empty := episodeNFOFromCatalog("Pilot", 1, 1, nil, nil)
	if empty.Plot != "" || empty.Runtime != 0 || empty.Aired != "" {
		t.Fatalf("%#v", empty)
	}
}
