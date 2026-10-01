package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSidecarSubtitles(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Show.S01E01.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.en.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.fr.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.en.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverSidecarSubtitles(video)
	if len(got) != 2 {
		t.Fatalf("got %d %#v", len(got), got)
	}
	ids := map[string]bool{}
	for _, sc := range got {
		ids[sc.ID] = true
	}
	if !ids["sc-en"] || !ids["sc-fr"] {
		t.Fatalf("ids %#v", ids)
	}
	if FindSidecarByID(video, "sc-en") == nil {
		t.Fatal("FindSidecarByID")
	}
}
