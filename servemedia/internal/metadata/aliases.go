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

// AliasKey is the exported form of the title key used for merge/attach.
func AliasKey(s string) string {
	return aliasKey(s)
}

var titleStop = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true, "in": true, "on": true,
	"at": true, "is": true, "and": true, "or": true, "my": true, "for": true, "with": true,
	"from": true, "as": true, "by": true,
}

// ContentTokens are aliasKey fields after dropping stop words and tokens shorter than 3.
func ContentTokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(aliasKey(s)) {
		if len(t) < 3 || titleStop[t] {
			continue
		}
		out = append(out, t)
	}
	return out
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
