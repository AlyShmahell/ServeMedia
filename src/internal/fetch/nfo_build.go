package fetch

import (
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func movieNFOFromCatalog(title string, year int, plot string, attrs map[string]string, prev *metadata.MovieNFO) metadata.MovieNFO {
	nfo := metadata.MovieNFO{Title: title, Year: year, Plot: plot}
	if prev != nil {
		nfo = *prev
		if strings.TrimSpace(title) != "" {
			nfo.Title = title
		}
		if year > 0 {
			nfo.Year = year
		}
		if strings.TrimSpace(plot) != "" {
			nfo.Plot = plot
		}
	}
	nfo.ApplyFacts(metadata.FactsFromAttrs(attrs))
	return nfo
}

func showNFOFromCatalog(title string, year int, plot string, attrs map[string]string, prev *metadata.TVShowNFO) metadata.TVShowNFO {
	nfo := metadata.TVShowNFO{Title: title, Year: year, Plot: plot}
	if prev != nil {
		nfo = *prev
		if strings.TrimSpace(title) != "" {
			nfo.Title = title
		}
		if year > 0 {
			nfo.Year = year
		}
		if strings.TrimSpace(plot) != "" {
			nfo.Plot = plot
		}
	}
	nfo.ApplyFacts(metadata.FactsFromAttrs(attrs))
	return nfo
}

func episodeNFOFromCatalog(title string, season, episode int, ce *matchmedia.Episode, prev *metadata.EpisodeNFO) metadata.EpisodeNFO {
	nfo := metadata.EpisodeNFO{Title: title, Season: season, Episode: episode}
	if prev != nil {
		nfo.Plot = prev.Plot
		nfo.Runtime = prev.Runtime
		nfo.Aired = prev.Aired
	}
	if ce == nil {
		return nfo
	}
	if plot := strings.TrimSpace(ce.Synopsis); plot != "" {
		nfo.Plot = plot
	}
	if rt := metadata.ParseRuntime(ce.Runtime); rt > 0 {
		nfo.Runtime = rt
	}
	if aired := strings.TrimSpace(ce.Aired); aired != "" {
		nfo.Aired = aired
	}
	return nfo
}
