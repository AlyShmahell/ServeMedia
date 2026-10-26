package matchmedia

import (
	"strconv"
	"strings"

	"github.com/alyshmahell/matchmedia/src/mod/engine/rank"
	mmfetch "github.com/alyshmahell/matchmedia/src/mod/fetch"
	"github.com/alyshmahell/matchmedia/src/mod/match"
)

// Job is MatchMedia's job. ServeMedia does not keep a second copy.
type (
	Job       = match.Job
	JobFile   = match.JobFile
	Candidate = match.Candidate
	Request   = rank.Request
	Season    = mmfetch.Season
	Episode   = mmfetch.Episode
)

type Status struct {
	Ready          bool
	Version        string
	DisabledReason string
	Hint           string
}

type ScanResult struct {
	Session string `json:"session"`
	Files   int    `json:"files"`
	Jobs    int    `json:"jobs"`
}

type IngestRow struct {
	Title string `json:"title"`
	Year  string `json:"year,omitempty"`
	Type  string `json:"type,omitempty"`
}

type ScanProgress struct {
	Files   int  `json:"files"`
	Done    int  `json:"done"`
	Chunks  int  `json:"chunks"`
	Chunk   int  `json:"chunk"`
	Running bool `json:"running"`
}

type Catalog struct {
	Provider string            `json:"provider"`
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Year     string            `json:"year,omitempty"`
	Type     string            `json:"type"`
	Synopsis string            `json:"synopsis,omitempty"`
	Poster   string            `json:"poster,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Seasons  []Season          `json:"seasons,omitempty"`
}

func (t Catalog) FindSeason(n int) *Season {
	for i := range t.Seasons {
		if atoi(t.Seasons[i].Number) == n {
			return &t.Seasons[i]
		}
	}
	return nil
}

func (t Catalog) FindEpisode(season, episode int) *Episode {
	s := t.FindSeason(season)
	if s == nil {
		return nil
	}
	for i := range s.Episodes {
		if atoi(s.Episodes[i].Number) == episode {
			return &s.Episodes[i]
		}
	}
	return nil
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		if n, err := strconv.Atoi(s[:4]); err == nil {
			return n
		}
	}
	n, _ := strconv.Atoi(s)
	return n
}
