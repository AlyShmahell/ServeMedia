package media_test

import (
	"testing"

	"github.com/alyshmahell/medora/internal/media"
)

func TestGuessDirectPlayByExt(t *testing.T) {
	if !media.GuessDirectPlayByExt("/a/b.mp4") {
		t.Fatal("mp4")
	}
	if media.GuessDirectPlayByExt("/a/b.mkv") {
		t.Fatal("mkv should not guess direct")
	}
}

func TestCanDirectPlay(t *testing.T) {
	p := &media.Probe{}
	p.Format.FormatName = "mov,mp4,m4a"
	p.Streams = []media.Stream{
		{Index: 0, CodecType: "video", CodecName: "h264"},
		{Index: 1, CodecType: "audio", CodecName: "aac"},
	}
	if !media.CanDirectPlay(p) {
		t.Fatal("expected direct play")
	}
}

func TestDurationSeconds(t *testing.T) {
	p := &media.Probe{}
	p.Format.Duration = "1423.5"
	if g := p.DurationSeconds(); g != 1423.5 {
		t.Fatalf("got %v", g)
	}
	if (&media.Probe{}).DurationSeconds() != 0 {
		t.Fatal("empty should be 0")
	}
}

func TestMatchTrackByLang(t *testing.T) {
	tracks := []media.Track{
		{Index: 1, Lang: "jpn"},
		{Index: 2, Lang: "eng"},
	}
	if media.MatchTrackByLang(tracks, "ENG") != 2 {
		t.Fatal("exact")
	}
	if media.MatchTrackByLang(tracks, "en") != 2 {
		t.Fatal("prefix")
	}
	if media.MatchTrackByLang(tracks, "deu") != -1 {
		t.Fatal("missing")
	}
}

func TestVideoHeightAndQualityOptions(t *testing.T) {
	p := &media.Probe{}
	p.Streams = []media.Stream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160},
	}
	if p.VideoHeight() != 2160 || p.VideoWidth() != 3840 {
		t.Fatalf("dims %dx%d", p.VideoWidth(), p.VideoHeight())
	}
	qs := media.QualityOptions(2160)
	if len(qs) != 5 || qs[0].Label != "2160p" || qs[0].Height != 0 ||
		qs[1].Height != 1440 || qs[2].Height != 1080 || qs[3].Height != 720 || qs[4].Height != 480 {
		t.Fatalf("qualities %#v", qs)
	}
	qs1080 := media.QualityOptions(1080)
	if len(qs1080) != 3 || qs1080[0].Label != "1080p" || qs1080[1].Height != 720 || qs1080[2].Height != 480 {
		t.Fatalf("1080 source qualities %#v", qs1080)
	}
	qs720 := media.QualityOptions(720)
	if len(qs720) != 2 || qs720[0].Label != "720p" || qs720[1].Height != 480 {
		t.Fatalf("720 source qualities %#v", qs720)
	}
}

func TestChapterList(t *testing.T) {
	p := &media.Probe{}
	p.Chapters = []media.Chapter{
		{Start: 0, End: 60, Title: "Intro"},
		{Start: 60},
	}
	chs := p.ChapterList()
	if len(chs) != 2 || chs[0].Title != "Intro" || chs[1].Title != "Chapter 2" {
		t.Fatalf("%#v", chs)
	}
}

func TestAudioAndSubtitleTracks(t *testing.T) {
	p := &media.Probe{}
	p.Streams = []media.Stream{
		{Index: 0, CodecType: "video", CodecName: "hevc"},
		{Index: 1, CodecType: "audio", CodecName: "aac", Tags: map[string]string{"language": "jpn", "title": "Japanese"}},
		{Index: 2, CodecType: "audio", CodecName: "aac", Tags: map[string]string{"language": "eng"}},
		{Index: 3, CodecType: "subtitle", CodecName: "ass", Tags: map[string]string{"language": "eng", "title": "Signs"}},
		{Index: 4, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle"},
	}
	ats := p.AudioTracks()
	if len(ats) != 2 || ats[0].Index != 1 || ats[0].Lang != "jpn" {
		t.Fatalf("audio %#v", ats)
	}
	sts := p.SubtitleTracks()
	if len(sts) != 1 || sts[0].Index != 3 || sts[0].Codec != "ass" {
		t.Fatalf("subs %#v", sts)
	}
	if p.DefaultAudioIndex() != 1 {
		t.Fatal("default audio")
	}
	if !p.HasSubtitleStream(3) || p.HasSubtitleStream(4) {
		t.Fatal("has subtitle")
	}
}
