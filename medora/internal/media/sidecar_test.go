package media

import (
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.it.whisper-tiny-q5_1.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.ja.whisper-large-v3-turbo.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.en.srt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverSidecarSubtitles(video)
	if len(got) != 4 {
		t.Fatalf("got %d %#v", len(got), got)
	}
	ids := map[string]bool{}
	for _, sc := range got {
		ids[sc.ID] = true
	}
	if !ids["sc-en"] || !ids["sc-fr"] || !ids["sc-it-whisper-tiny-q5_1"] || !ids["sc-ja-whisper-large-v3-turbo"] {
		t.Fatalf("ids %#v", ids)
	}
	tracks := SidecarTracks(got, 10000)
	foundAI := false
	for _, tr := range tracks {
		if strings.Contains(tr.Title, "AI whisper-tiny-q5_1") {
			foundAI = true
		}
	}
	if !foundAI {
		t.Fatalf("tracks %#v", tracks)
	}
	if FindSidecarByID(video, "sc-en") == nil {
		t.Fatal("FindSidecarByID")
	}
}
