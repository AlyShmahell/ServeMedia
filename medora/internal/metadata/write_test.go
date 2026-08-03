package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleSidecarName(t *testing.T) {
	got := SubtitleSidecarName("/media/Movies/Foo (2020)/Foo.mkv", "en")
	if got != "Foo.en.srt" {
		t.Fatalf("got %q", got)
	}
	got = SubtitleSidecarName("/x/Show.S01E01.mkv", "pt-BR")
	if got != "Show.S01E01.pt-br.srt" {
		t.Fatalf("got %q", got)
	}
	got = WhisperSidecarName("/x/Show.S01E01.mkv", "it", "tiny-q5_1")
	if got != "Show.S01E01.it.whisper-tiny-q5_1.srt" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteBesideVideo(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mkv")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, err := WriteBytesBesideVideo(video, SubtitleSidecarName(video, "fr"), []byte("1\n00:00:01,000 --> 00:00:02,000\nhi\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dst, "clip.fr.srt") {
		t.Fatalf("dst %q", dst)
	}
	b, err := os.ReadFile(dst)
	if err != nil || !strings.Contains(string(b), "hi") {
		t.Fatalf("read %v %q", err, b)
	}
}

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
