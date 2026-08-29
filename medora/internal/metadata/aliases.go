package metadata

import (
	"path/filepath"
	"regexp"
	"strings"
)

var aliasKeyRe = regexp.MustCompile(`[^a-z0-9]+`)

func aliasKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = aliasKeyRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

var movieTitleSuffixRe = regexp.MustCompile(`(?i)\s*(?:-\s*)?(?:the\s+)?movie\s*$`)

// StripMovieTitleSuffix removes a trailing " - The Movie" / "Movie" style suffix
// so NFO titles match path-derived names.
func StripMovieTitleSuffix(title string) string {
	t := strings.TrimSpace(title)
	stripped := strings.TrimSpace(movieTitleSuffixRe.ReplaceAllString(t, ""))
	if stripped == "" {
		return t
	}
	return stripped
}

var (
	leadingReleaseGroupRe = regexp.MustCompile(`^\[[^\]]+\]\s*`)
	trailingBracketTagRe  = regexp.MustCompile(`\s*\[[^\]]*\]\s*$`)
	trailingParenTagRe    = regexp.MustCompile(`(?i)\s*\((?:(?:\d{3,4}p)|(?:[xh]\.?26[45])|hevc|avc|aac|flac|opus|bluray|blu-ray|web-?dl|webrip|hdtv|dvdrip|remux|10.?bit)[^)]*\)\s*$`)
)

// CleanEpisodeTitle strips fansub group prefixes and trailing quality/hash
// brackets from a video filename for display (extension optional).
func CleanEpisodeTitle(name string) string {
	t := strings.TrimSpace(name)
	if ext := filepath.Ext(t); ext != "" && IsVideo(t) {
		t = strings.TrimSuffix(t, ext)
	}
	for leadingReleaseGroupRe.MatchString(t) {
		t = strings.TrimSpace(leadingReleaseGroupRe.ReplaceAllString(t, ""))
	}
	for trailingBracketTagRe.MatchString(t) {
		t = strings.TrimSpace(trailingBracketTagRe.ReplaceAllString(t, ""))
	}
	for trailingParenTagRe.MatchString(t) {
		t = strings.TrimSpace(trailingParenTagRe.ReplaceAllString(t, ""))
	}
	t = strings.ReplaceAll(t, "_", " ")
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return strings.TrimSpace(name)
	}
	return t
}
