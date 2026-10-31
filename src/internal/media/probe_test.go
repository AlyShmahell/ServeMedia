package media_test

import (
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/media"
)

func TestGuessDirectPlayByExt(t *testing.T) {
	if !media.GuessDirectPlayByExt("/a/b.mp4") {
		t.Fatal("mp4")
	}
	if media.GuessDirectPlayByExt("/a/b.mkv") {
		t.Fatal("mkv should not guess direct")
	}
}

func TestStreamMIME(t *testing.T) {
	if got := media.StreamMIME("/a/b.mp4"); got != "video/mp4" {
		t.Fatalf("mp4 mime: %s", got)
	}
	if got := media.StreamMIME("/a/b.webm"); got != "video/webm" {
		t.Fatalf("webm mime: %s", got)
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
	all := []int{2160, 1440, 1080, 720, 480, 360, 240, 144}
	qs := media.QualityOptions(2160, all)
	if len(qs) != 8 || qs[0].Label != "2160p" || qs[0].Height != 0 ||
		qs[1].Height != 1440 || qs[2].Height != 1080 || qs[3].Height != 720 || qs[4].Height != 480 ||
		qs[5].Height != 360 || qs[6].Height != 240 || qs[7].Height != 144 {
		t.Fatalf("qualities %#v", qs)
	}
	qs1080 := media.QualityOptions(1080, all)
	if len(qs1080) != 6 || qs1080[0].Label != "1080p" || qs1080[0].Height != 0 || qs1080[1].Height != 720 || qs1080[2].Height != 480 ||
		qs1080[3].Height != 360 || qs1080[4].Height != 240 || qs1080[5].Height != 144 {
		t.Fatalf("1080 source qualities %#v", qs1080)
	}
	qs720 := media.QualityOptions(720, all)
	if len(qs720) != 5 || qs720[0].Label != "720p" || qs720[0].Height != 0 || qs720[1].Height != 480 ||
		qs720[2].Height != 360 || qs720[3].Height != 240 || qs720[4].Height != 144 {
		t.Fatalf("720 source qualities %#v", qs720)
	}
	capped := media.QualityOptions(1080, []int{2160, 720, 480, 360, 240, 144})
	if len(capped) != 5 || capped[0].Height != 720 || capped[0].Label != "720p" ||
		capped[1].Height != 480 || capped[2].Height != 360 || capped[3].Height != 240 || capped[4].Height != 144 {
		t.Fatalf("1080 with 1080 off qualities %#v", capped)
	}
	for _, q := range capped {
		if q.Height == 0 || q.Height == 1080 || q.Height == 2160 {
			t.Fatalf("allowlist still offers source, 1080, or 2160: %#v", capped)
		}
	}
	withCustom := media.QualityOptions(1080, []int{1080, 800, 720})
	if len(withCustom) != 3 || withCustom[0].Height != 0 || withCustom[1].Height != 800 || withCustom[2].Height != 720 {
		t.Fatalf("custom height %#v", withCustom)
	}
}

func TestResolvePlaybackHeightCapsSource(t *testing.T) {
	below := []int{720, 480, 360, 240, 144}
	if h, force := media.ResolvePlaybackHeight(1080, 0, below, false); h != 720 || !force {
		t.Fatalf("source request: height %d force %v", h, force)
	}
	if h, force := media.ResolvePlaybackHeight(1080, 1080, below, true); h != 720 || !force {
		t.Fatalf("explicit 1080: height %d force %v", h, force)
	}
	if h, force := media.ResolvePlaybackHeight(1080, 480, below, false); h != 480 || !force {
		t.Fatalf("below cap: height %d force %v", h, force)
	}
	if h, force := media.ResolvePlaybackHeight(1080, 0, []int{2160, 720}, false); h != 720 || !force {
		t.Fatalf("1080 off with 2160 on: height %d force %v", h, force)
	}
	if h, force := media.ResolvePlaybackHeight(720, 0, []int{720}, false); h != 0 || force {
		t.Fatalf("at enabled height: height %d force %v", h, force)
	}
	all := []int{2160, 1440, 1080, 720, 480, 360, 240, 144}
	if h, force := media.ResolvePlaybackHeight(1080, 0, all, false); h != 0 || force {
		t.Fatalf("enabled source: height %d force %v", h, force)
	}
	if h, force := media.ResolvePlaybackHeight(480, 0, []int{1080, 720}, false); h != 0 || force {
		t.Fatalf("shorter than every height: height %d force %v", h, force)
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
