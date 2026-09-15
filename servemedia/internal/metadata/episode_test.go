package metadata_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/metadata"
)

func TestParseEpisode_leadingSxxEyy(t *testing.T) {
	s, e, ok := metadata.ParseEpisode("S02E01-TAG [AAAAAAAA].mp4")
	if !ok || s != 2 || e != 1 {
		t.Fatalf("got season=%d ep=%d ok=%v", s, e, ok)
	}
}

func TestParseEpisode_packStyle(t *testing.T) {
	s, e, ok := metadata.ParseEpisode("[Grp] Pack Show S01E02 [WEBRip].mp4")
	if !ok || s != 1 || e != 2 {
		t.Fatalf("got season=%d ep=%d ok=%v", s, e, ok)
	}
}

func TestParseEpisodeNumber_underNum(t *testing.T) {
	ep, ok := metadata.ParseEpisodeNumber("[Grp]Season Pack Show Season 2_-_01_(Dual).mp4")
	if !ok || ep != 1 {
		t.Fatalf("got ep=%d ok=%v", ep, ok)
	}
}

func TestParseEpisodeNumber_wordAndBracket(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"Show Episode 01.mkv", 1},
		{"Show Ep02.mkv", 2},
		{"Show E03 [1080p].mkv", 3},
		{"Show Name [04].mkv", 4},
	}
	for _, tc := range cases {
		ep, ok := metadata.ParseEpisodeNumber(tc.name)
		if !ok || ep != tc.want {
			t.Fatalf("%q: got ep=%d ok=%v want %d", tc.name, ep, ok, tc.want)
		}
	}
}

func TestResolveEpisodeLoose_seasonPack(t *testing.T) {
	show := "/media/Anime/Season Pack Show"
	video := filepath.Join(show, "Season 2", "[Grp]Season Pack Show Season 2_-_03_(Dual).mp4")
	s, e, ok := metadata.ResolveEpisodeLoose(video, show)
	if !ok || s != 2 || e != 3 {
		t.Fatalf("got season=%d ep=%d ok=%v", s, e, ok)
	}
}

func TestIsEpisodeExtra(t *testing.T) {
	if !metadata.IsEpisodeExtra("[Beta] Dual Show - Opening.mp4") {
		t.Fatal("expected Opening to be extra")
	}
	if metadata.IsEpisodeExtra("[Beta] Dual Show - 01.mp4") {
		t.Fatal("numbered dual pack is not an extra")
	}
}

func TestIsSeasonFolderName(t *testing.T) {
	if !metadata.IsSeasonFolderName("Season 2") || !metadata.IsSeasonFolderName("S01") {
		t.Fatal("expected season folder names")
	}
	if !metadata.IsSeasonFolderName("Season 1 - Root A") || !metadata.IsSeasonFolderName("S01 Something") {
		t.Fatal("expected prefixed season folder names")
	}
	if !metadata.IsSeasonFolderName("OVA") || !metadata.IsSeasonFolderName("Specials") {
		t.Fatal("expected specials folder names")
	}
	if metadata.IsSeasonFolderName("Season Pack Show") {
		t.Fatal("show name is not a season folder")
	}
}

func TestParseSeasonFolder_specialsAndPrefix(t *testing.T) {
	if sn, ok := metadata.ParseSeasonFolder("OVA"); !ok || sn != 0 {
		t.Fatalf("OVA: got %d ok=%v", sn, ok)
	}
	if sn, ok := metadata.ParseSeasonFolder("Season 3 - re"); !ok || sn != 3 {
		t.Fatalf("Season 3 - re: got %d ok=%v", sn, ok)
	}
	if sn, ok := metadata.ParseSeasonFolder("01. Series Title"); !ok || sn != 1 {
		t.Fatalf("01. prefix: got %d ok=%v", sn, ok)
	}
	if sn, ok := metadata.ParseSeasonFolder("02. Series Title Arc"); !ok || sn != 2 {
		t.Fatalf("02. prefix: got %d ok=%v", sn, ok)
	}
	if sn, ok := metadata.ParseSeasonFolder("03 - re Part 1"); !ok || sn != 3 {
		t.Fatalf("03 - prefix: got %d ok=%v", sn, ok)
	}
}

func TestSeasonIndexForShow_irregular(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Alpha Cour", "Beta Cour", "OVA"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+" - 01.mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := metadata.SeasonIndexForShow(root)
	if idx[filepath.Clean(filepath.Join(root, "OVA"))] != 0 {
		t.Fatalf("OVA season: %#v", idx)
	}
	a := idx[filepath.Clean(filepath.Join(root, "Alpha Cour"))]
	b := idx[filepath.Clean(filepath.Join(root, "Beta Cour"))]
	if a < 1 || b < 1 || a == b {
		t.Fatalf("irregular seasons: %#v", idx)
	}
}

func TestSeasonForPath_prefersSubdirIndex(t *testing.T) {
	root := t.TempDir()
	cour := filepath.Join(root, "Series Arc A")
	if err := os.MkdirAll(cour, 0o755); err != nil {
		t.Fatal(err)
	}
	vid := filepath.Join(cour, "Show S04E01.mkv")
	if err := os.WriteFile(vid, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sn := metadata.SeasonForPath(vid, root)
	if sn != 1 {
		t.Fatalf("expected mapped season 1, got %d", sn)
	}
}

func TestSeasonForPath_numberedPrefixAndRootOVA(t *testing.T) {
	root := t.TempDir()
	s1 := filepath.Join(root, "01. Series Title")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	vid := filepath.Join(s1, "Series Title - 01.mkv")
	if err := os.WriteFile(vid, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sn := metadata.SeasonForPath(vid, root); sn != 1 {
		t.Fatalf("01. folder: got %d", sn)
	}
	ova := filepath.Join(root, "[Group] Series Title - OVA 01 - Extra.mkv")
	if err := os.WriteFile(ova, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sn := metadata.SeasonForPath(ova, root); sn != 0 {
		t.Fatalf("root OVA: got %d", sn)
	}
	ep, ok := metadata.ParseEpisodeNumber(filepath.Base(ova))
	if !ok || ep != 1 {
		t.Fatalf("OVA episode number: got %d ok=%v", ep, ok)
	}
}

func TestSeasonIndexForShow_numberedPrefixes(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"01. Series Title", "02. Series Title Arc", "03. re Part 1", "04. re Part 2"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ep - 01.mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := metadata.SeasonIndexForShow(root)
	for name, want := range map[string]int{
		"01. Series Title":     1,
		"02. Series Title Arc": 2,
		"03. re Part 1":        3,
		"04. re Part 2":        4,
	} {
		got := idx[filepath.Clean(filepath.Join(root, name))]
		if got != want {
			t.Fatalf("%s: want %d got %d (%#v)", name, want, got, idx)
		}
	}
}
