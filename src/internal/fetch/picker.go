package fetch

import (
	"encoding/json"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

type pickerSnapshot struct {
	Match      *matchmedia.Candidate  `json:"match,omitempty"`
	Candidates []matchmedia.Candidate `json:"candidates,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Year       string                 `json:"year,omitempty"`
	Parent     string                 `json:"parent,omitempty"`
	Kind       string                 `json:"kind,omitempty"`
	Path       string                 `json:"path,omitempty"`
	Files      []matchmedia.JobFile   `json:"files,omitempty"`
}

func EncodePickerSnapshot(j matchmedia.Job) string {
	if j.Match == nil && len(j.Candidates) == 0 {
		return ""
	}
	b, err := json.Marshal(pickerSnapshot{
		Match: j.Match, Candidates: j.Candidates,
		Title: j.Title, Year: j.Year, Parent: j.Parent, Kind: j.Kind, Path: j.Path, Files: j.Files,
	})
	if err != nil {
		return ""
	}
	return string(b)
}

func DecodePickerSnapshot(raw string) matchmedia.Job {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return matchmedia.Job{}
	}
	var snap pickerSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return matchmedia.Job{}
	}
	return matchmedia.Job{
		Match: snap.Match, Candidates: snap.Candidates,
		Title: snap.Title, Year: snap.Year, Parent: snap.Parent, Kind: snap.Kind, Path: snap.Path, Files: snap.Files,
	}
}

func JobHasPickerCandidates(j matchmedia.Job) bool {
	if len(j.Candidates) > 0 {
		return true
	}
	return j.Match != nil && strings.TrimSpace(j.Match.Provider) != "" && strings.TrimSpace(j.Match.ID) != ""
}

func jobApplyFingerprint(j matchmedia.Job) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(strings.TrimSpace(j.Status)))
	b.WriteByte('|')
	if j.Match != nil {
		b.WriteString(strings.TrimSpace(j.Match.Provider))
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(j.Match.ID))
	}
	b.WriteByte('|')
	for i, c := range j.Candidates {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strings.TrimSpace(c.Provider))
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(c.ID))
	}
	return b.String()
}
