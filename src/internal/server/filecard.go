package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

type fileFact struct {
	Label string
	Value string
}

type sideNFO struct {
	Title   string
	Plot    string
	Aired   string
	Season  int
	Episode int
	Runtime int
}

func stripVideoExt(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || !metadata.IsVideo(name) {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func parentRel(rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return ""
	}
	return rel[:i]
}

func folderWatch(files []db.FileWatch, rel string) (watched, total, pct int) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	prefix := rel + "/"
	for _, f := range files {
		if rel != "" && !strings.HasPrefix(f.Rel, prefix) {
			continue
		}
		total++
		if f.Done {
			watched++
		}
	}
	if total > 0 {
		pct = watched * 100 / total
	}
	return watched, total, pct
}

func fileTitle(node db.LibraryNode, side sideNFO) string {
	title := strings.TrimSpace(node.FileTitle.String)
	if title == "" {
		title = strings.TrimSpace(side.Title)
	}
	if title == "" {
		title = node.Name
	}
	return stripVideoExt(title)
}

func isShowKind(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	return k == "show" || k == "tv"
}

func fileCardFields(libPath string, node db.LibraryNode, show bool) (title, plot, still string, facts []fileFact) {
	abs := filepath.Join(libPath, filepath.FromSlash(node.RelPath))
	side := readSideNFO(abs)
	title = fileTitle(node, side)
	plot = strings.TrimSpace(node.FilePlot.String)
	if plot == "" {
		plot = strings.TrimSpace(side.Plot)
	}
	still = strings.TrimSpace(node.StillPath.String)
	if still == "" {
		still = strings.TrimSpace(node.PosterPath.String)
	}
	if show {
		season := strings.TrimSpace(node.SeasonNum.String)
		if season == "" && side.Season > 0 {
			season = strconv.Itoa(side.Season)
		}
		episode := strings.TrimSpace(node.EpisodeNum.String)
		if episode == "" && side.Episode > 0 {
			episode = strconv.Itoa(side.Episode)
		}
		if season == "0" {
			season, episode = "", ""
		}
		if season != "" {
			facts = append(facts, fileFact{Label: "Season", Value: season})
		}
		if episode != "" && season != "" {
			facts = append(facts, fileFact{Label: "Episode", Value: episode})
		}
	}
	if side.Runtime > 0 {
		facts = append(facts, fileFact{Label: "Runtime", Value: strconv.Itoa(side.Runtime) + " min"})
	}
	if aired := strings.TrimSpace(side.Aired); aired != "" {
		facts = append(facts, fileFact{Label: "Aired", Value: aired})
	}
	return title, plot, still, facts
}

func readSideNFO(abs string) sideNFO {
	dir := filepath.Dir(abs)
	base := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	for _, p := range []string{
		filepath.Join(dir, base+".nfo"),
		filepath.Join(dir, "episode.nfo"),
		filepath.Join(dir, "movie.nfo"),
	} {
		b, err := os.ReadFile(p)
		if err != nil || len(bytesTrim(b)) == 0 {
			continue
		}
		low := strings.ToLower(string(b))
		if strings.Contains(low, "<episodedetails") {
			n, err := metadata.ReadEpisodeNFO(p)
			if err != nil || n == nil {
				continue
			}
			return sideNFO{Title: n.Title, Plot: n.Plot, Aired: n.Aired, Season: n.Season, Episode: n.Episode, Runtime: n.Runtime}
		}
		if strings.Contains(low, "<movie") {
			n, err := metadata.ReadMovieNFO(p)
			if err != nil || n == nil {
				continue
			}
			plot := n.Plot
			if strings.TrimSpace(plot) == "" {
				plot = n.Outline
			}
			return sideNFO{Title: n.Title, Plot: plot, Aired: n.Premiered, Runtime: n.Runtime}
		}
	}
	return sideNFO{}
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
