package metadata_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func TestTitleNFOFactsFromDir(t *testing.T) {
	dir := t.TempDir()
	body := `<tvshow><title>Steins;Gate</title><year>2011</year><premiered>2011-04-06</premiered><plot>Plot</plot><rating>9.1</rating></tvshow>`
	if err := os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, ok := metadata.TitleNFOFactsFromDir(dir)
	if !ok {
		t.Fatal("expected facts")
	}
	if f.Title != "Steins;Gate" || f.Year != "2011" || f.Premiered != "2011-04-06" || f.Plot != "Plot" || f.Rating < 9 {
		t.Fatalf("%#v", f)
	}
}

func TestTitleNFOFactsYearFromPremiered(t *testing.T) {
	f := metadata.TitleNFOFactsFromMovie(metadata.MovieNFO{
		Title: "Film", Premiered: "2016-07-16", Plot: "p",
	})
	if f.Year != "2016" || f.Premiered != "2016-07-16" {
		t.Fatalf("%#v", f)
	}
}
