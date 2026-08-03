package cascade

import (
	"fmt"
	"strings"

	"github.com/alyshmahell/medora/providers/omdb"
	"github.com/alyshmahell/medora/providers/rpcapi"
	"github.com/alyshmahell/medora/providers/tvmaze"
)

// Cascade tries TVmaze (tier-0) then OMDb (tier-1). Movies use OMDb only.
type Cascade struct {
	TVmaze *tvmaze.Client
	OMDb   *omdb.Client
}

func (c *Cascade) Status() rpcapi.StatusReply {
	reply := rpcapi.StatusReply{Ready: true}
	if c.OMDb == nil || !c.OMDb.Enabled() {
		reply.Hint = "Set OMDB_API_KEY for movie metadata. TVmaze handles TV without a key."
	} else {
		reply.Hint = "Metadata: TVmaze (TV) → OMDb (fallback / movies)."
	}
	return reply
}

func (c *Cascade) LookupMovie(title string, year int, libraryType string, durationMinutes int) (*rpcapi.Result, error) {
	if c.OMDb == nil {
		return nil, fmt.Errorf("OMDb API key not configured (set OMDB_API_KEY)")
	}
	return c.OMDb.LookupMovie(title, year, libraryType, durationMinutes)
}

func hasPoster(r *rpcapi.Result) bool {
	return r != nil && strings.TrimSpace(r.PosterURL) != ""
}

func hasStill(r *rpcapi.Result) bool {
	return r != nil && strings.TrimSpace(r.StillURL) != ""
}

func animeLibrary(libraryType string) bool {
	return strings.EqualFold(strings.TrimSpace(libraryType), "anime")
}

func (c *Cascade) LookupShow(title string, year int, libraryType string, excludeIDs ...string) (*rpcapi.Result, error) {
	var errs []string
	var thin *rpcapi.Result
	if c.TVmaze != nil {
		r, err := c.TVmaze.LookupShow(title, year, libraryType, excludeIDs...)
		if err == nil {
			if hasPoster(r) {
				return r, nil
			}
			thin = r
		} else {
			errs = append(errs, err.Error())
		}
	}
	// Anime libraries stay on TVmaze: OMDb often returns live-action remakes and
	// burns the daily key on non-matching fixture/library titles.
	if !animeLibrary(libraryType) && c.OMDb != nil && c.OMDb.Enabled() {
		r, err := c.OMDb.LookupShow(title, year, libraryType)
		if err == nil {
			return r, nil
		}
		errs = append(errs, err.Error())
	}
	if thin != nil {
		return thin, nil
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("no metadata providers available")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
}

func (c *Cascade) LookupSeason(showTitle string, season int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error) {
	var errs []string
	var thin *rpcapi.Result
	if c.TVmaze != nil {
		r, err := c.TVmaze.LookupSeason(showTitle, season, libraryType, showProvider, showProviderID)
		if err == nil {
			if hasPoster(r) {
				return r, nil
			}
			thin = r
		} else {
			errs = append(errs, err.Error())
		}
	}
	if !animeLibrary(libraryType) && c.OMDb != nil && c.OMDb.Enabled() {
		r, err := c.OMDb.LookupSeason(showTitle, season)
		if err == nil {
			if hasPoster(r) || thin == nil {
				return r, nil
			}
			if thin.Plot != "" && r.Plot == "" {
				r.Plot = thin.Plot
			}
			if thin.Title != "" && (r.Title == "" || strings.HasPrefix(r.Title, "Season ")) {
				r.Title = thin.Title
			}
			return r, nil
		}
		errs = append(errs, err.Error())
	}
	if thin != nil {
		return thin, nil
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("no metadata providers available")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
}

func (c *Cascade) LookupEpisode(showTitle string, season, episode int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error) {
	var errs []string
	var thin *rpcapi.Result
	if c.TVmaze != nil {
		r, err := c.TVmaze.LookupEpisode(showTitle, season, episode, libraryType, showProvider, showProviderID)
		if err == nil {
			if hasStill(r) {
				return r, nil
			}
			thin = r
		} else {
			errs = append(errs, err.Error())
		}
	}
	if !animeLibrary(libraryType) && c.OMDb != nil && c.OMDb.Enabled() {
		r, err := c.OMDb.LookupEpisode(showTitle, season, episode)
		if err == nil {
			if thin != nil {
				if r.Title == "" {
					r.Title = thin.Title
				}
				if r.Plot == "" {
					r.Plot = thin.Plot
				}
				if r.StillURL == "" {
					r.StillURL = thin.StillURL
				}
			}
			return r, nil
		}
		errs = append(errs, err.Error())
	}
	if thin != nil {
		return thin, nil
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("no metadata providers available")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
}
