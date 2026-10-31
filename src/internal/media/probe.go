package media

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Probe struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
	Streams  []Stream  `json:"streams"`
	Chapters []Chapter `json:"chapters"`
}

type Stream struct {
	Index     int               `json:"index"`
	CodecType string            `json:"codec_type"`
	CodecName string            `json:"codec_name"`
	PixFmt    string            `json:"pix_fmt"`
	Channels  int               `json:"channels"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Tags      map[string]string `json:"tags"`
}

type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end,omitempty"`
	Title string  `json:"title,omitempty"`
}

type Track struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"` // when set, use for /sub/{id}.vtt (sidecars)
	Type  string `json:"type"`         // audio | subtitle
	Codec string `json:"codec"`
	Lang  string `json:"lang,omitempty"`
	Title string `json:"title,omitempty"`
}

type QualityOption struct {
	Height int    `json:"height"`
	Label  string `json:"label"`
}

// DurationSeconds returns format duration from ffprobe, or 0 if unknown.
func (p *Probe) DurationSeconds() float64 {
	if p == nil {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(p.Format.Duration), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f
}

// VideoPixFmt returns the first video stream pixel format, or "".
func (p *Probe) VideoPixFmt() string {
	if p == nil {
		return ""
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.PixFmt != "" {
			return s.PixFmt
		}
	}
	return ""
}

// VideoHeight returns the first video stream height, or 0.
func (p *Probe) VideoHeight() int {
	if p == nil {
		return 0
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.Height > 0 {
			return s.Height
		}
	}
	return 0
}

// VideoWidth returns the first video stream width, or 0.
func (p *Probe) VideoWidth() int {
	if p == nil {
		return 0
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.Width > 0 {
			return s.Width
		}
	}
	return 0
}

// ChapterList returns normalized chapters with start times in seconds.
func (p *Probe) ChapterList() []Chapter {
	if p == nil || len(p.Chapters) == 0 {
		return nil
	}
	out := make([]Chapter, 0, len(p.Chapters))
	for i, c := range p.Chapters {
		ch := Chapter{Start: c.Start, End: c.End, Title: c.Title}
		if ch.Title == "" {
			ch.Title = "Chapter " + strconv.Itoa(i+1)
		}
		out = append(out, ch)
	}
	return out
}

// MatchTrackByLang returns the stream index of the first track whose lang matches
// lang (case-insensitive, or prefix like en/eng), or -1 if none.
func MatchTrackByLang(tracks []Track, lang string) int {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return -1
	}
	for _, t := range tracks {
		if strings.EqualFold(strings.TrimSpace(t.Lang), lang) {
			return t.Index
		}
	}
	want := strings.ToLower(lang)
	for _, t := range tracks {
		have := strings.ToLower(strings.TrimSpace(t.Lang))
		if have != "" && (strings.HasPrefix(have, want) || strings.HasPrefix(want, have)) {
			return t.Index
		}
	}
	return -1
}

// QualityOptions returns playback heights for a file.
// Height 0 is the file itself. enabled is the allowlist, highest first after
// normalization. The file is offered when its height is enabled, when it is
// shorter than every enabled height, or when the allowlist is empty. Otherwise
// the top entry is the highest enabled height at or below the file.
func QualityOptions(sourceHeight int, enabled []int) []QualityOption {
	if sourceHeight <= 0 {
		return []QualityOption{{Height: 0, Label: "Source"}}
	}
	allowed := normalizeHeights(enabled)
	if sourcePlayable(sourceHeight, allowed) {
		out := []QualityOption{{Height: 0, Label: strconv.Itoa(sourceHeight) + "p"}}
		for _, h := range allowed {
			if h < sourceHeight {
				out = append(out, QualityOption{Height: h, Label: strconv.Itoa(h) + "p"})
			}
		}
		return out
	}
	top := highestAtOrBelow(sourceHeight, allowed)
	if top == 0 {
		return []QualityOption{{Height: 0, Label: strconv.Itoa(sourceHeight) + "p"}}
	}
	out := []QualityOption{{Height: top, Label: strconv.Itoa(top) + "p"}}
	for _, h := range allowed {
		if h < top {
			out = append(out, QualityOption{Height: h, Label: strconv.Itoa(h) + "p"})
		}
	}
	return out
}

// ResolvePlaybackHeight is the encode height for the next play session.
// encodeHeight 0 means the file's own height. A file whose height is not
// enabled encodes at the highest enabled height at or below it. A requested
// height that is not enabled is lowered the same way.
func ResolvePlaybackHeight(sourceHeight, wantHeight int, enabled []int, explicit bool) (encodeHeight int, forceTranscode bool) {
	allowed := normalizeHeights(enabled)
	if wantHeight > 0 && sourceHeight > 0 && wantHeight > sourceHeight {
		wantHeight = sourceHeight
	}
	playable := sourcePlayable(sourceHeight, allowed)
	if playable && wantHeight > 0 && sourceHeight > 0 && wantHeight == sourceHeight {
		wantHeight = 0
	}
	if !explicit && wantHeight > 0 {
		offered := false
		for _, q := range QualityOptions(sourceHeight, allowed) {
			if q.Height == wantHeight {
				offered = true
				break
			}
		}
		if !offered {
			wantHeight = 0
		}
	}
	if wantHeight > 0 && !heightEnabled(wantHeight, allowed) {
		lowered := highestAtOrBelow(wantHeight, allowed)
		if lowered > 0 {
			wantHeight = lowered
		} else if playable {
			wantHeight = 0
		} else {
			wantHeight = highestAtOrBelow(sourceHeight, allowed)
		}
	}
	if !playable && wantHeight <= 0 {
		if top := highestAtOrBelow(sourceHeight, allowed); top > 0 {
			return top, true
		}
	}
	if wantHeight > 0 {
		return wantHeight, true
	}
	return 0, false
}

func normalizeHeights(in []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(in))
	for _, h := range in {
		if h <= 0 {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

func sourcePlayable(source int, allowed []int) bool {
	if source <= 0 || len(allowed) == 0 {
		return true
	}
	min := allowed[0]
	for _, h := range allowed {
		if h == source {
			return true
		}
		if h < min {
			min = h
		}
	}
	return source < min
}

func heightEnabled(height int, allowed []int) bool {
	for _, h := range allowed {
		if h == height {
			return true
		}
	}
	return false
}

func highestAtOrBelow(limit int, allowed []int) int {
	top := 0
	for _, h := range allowed {
		if h <= limit && h > top {
			top = h
		}
	}
	return top
}

func (p *Probe) AudioTracks() []Track {
	if p == nil {
		return nil
	}
	var out []Track
	for _, s := range p.Streams {
		if s.CodecType != "audio" {
			continue
		}
		out = append(out, trackFromStream(s, "audio"))
	}
	return out
}

func (p *Probe) SubtitleTracks() []Track {
	if p == nil {
		return nil
	}
	var out []Track
	for _, s := range p.Streams {
		if s.CodecType != "subtitle" && s.CodecType != "subrip" {
			continue
		}
		if !isTextSubtitle(s.CodecName) {
			continue
		}
		out = append(out, trackFromStream(s, "subtitle"))
	}
	return out
}

func (p *Probe) HasSubtitleStream(index int) bool {
	for _, t := range p.SubtitleTracks() {
		if t.Index == index {
			return true
		}
	}
	return false
}

func (p *Probe) HasAudioStream(index int) bool {
	for _, t := range p.AudioTracks() {
		if t.Index == index {
			return true
		}
	}
	return false
}

// DefaultAudioIndex returns the first audio stream index, or -1 if none.
func (p *Probe) DefaultAudioIndex() int {
	ats := p.AudioTracks()
	if len(ats) == 0 {
		return -1
	}
	return ats[0].Index
}

func trackFromStream(s Stream, typ string) Track {
	t := Track{
		Index: s.Index,
		Type:  typ,
		Codec: strings.ToLower(s.CodecName),
	}
	if s.Tags != nil {
		t.Lang = firstTag(s.Tags, "language", "LANGUAGE", "lang")
		t.Title = firstTag(s.Tags, "title", "TITLE")
	}
	return t
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(tags[k]); v != "" {
			return v
		}
	}
	// case-insensitive fallback
	for k, v := range tags {
		for _, want := range keys {
			if strings.EqualFold(k, want) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func isTextSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "subrip", "ass", "ssa", "webvtt", "mov_text", "srt", "text":
		return true
	default:
		return false
	}
}

func Ffprobe(path string) (*Probe, error) {
	cmd := ffprobeCommand(nil, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", "-show_chapters", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var raw struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams  []Stream `json:"streams"`
		Chapters []struct {
			StartTime string            `json:"start_time"`
			EndTime   string            `json:"end_time"`
			Tags      map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	p := &Probe{Streams: raw.Streams}
	p.Format.FormatName = raw.Format.FormatName
	p.Format.Duration = raw.Format.Duration
	for _, c := range raw.Chapters {
		start, _ := strconv.ParseFloat(strings.TrimSpace(c.StartTime), 64)
		end, _ := strconv.ParseFloat(strings.TrimSpace(c.EndTime), 64)
		title := ""
		if c.Tags != nil {
			title = firstTag(c.Tags, "title", "TITLE")
		}
		if start < 0 {
			continue
		}
		ch := Chapter{Start: start, Title: title}
		if end > start {
			ch.End = end
		}
		p.Chapters = append(p.Chapters, ch)
	}
	return p, nil
}

// CanDirectPlay returns true for common browser-friendly MP4/H.264/AAC.
func CanDirectPlay(p *Probe) bool {
	if p == nil {
		return false
	}
	fmtName := strings.ToLower(p.Format.FormatName)
	extFriendly := false
	var video, audio string
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if video == "" {
				video = strings.ToLower(s.CodecName)
			}
		case "audio":
			if audio == "" {
				audio = strings.ToLower(s.CodecName)
			}
		}
	}
	if strings.Contains(fmtName, "mp4") || strings.Contains(fmtName, "mov") {
		extFriendly = true
	}
	videoOK := video == "h264" || video == "avc"
	audioOK := audio == "aac" || audio == "mp3" || audio == ""
	return extFriendly && videoOK && audioOK
}

func GuessDirectPlayByExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov", ".webm":
		return true
	default:
		return false
	}
}

// StreamMIME is the Content-Type for a direct-play file. Extensionless
// stream URLs need this so video.js can pick a tech.
func StreamMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}
