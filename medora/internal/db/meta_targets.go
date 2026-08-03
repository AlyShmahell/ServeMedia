package db

import (
	"context"
	"database/sql"
	"errors"
)

// MetaTarget is a metadata enrich scope + id for a library item.
type MetaTarget struct {
	Scope string
	ID    int64
}

// ListLibraryMetaTargets returns movies/shows/seasons/episodes for a library.
// If missingOnly, only rows that still lack provider id and store artwork
// (stub scan NFOs alone do not count as enriched).
// Order: movies (when applicable), shows, seasons, episodes.
func (d *DB) ListLibraryMetaTargets(ctx context.Context, libraryID int64, missingOnly bool) ([]MetaTarget, error) {
	lib, err := d.libraryByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	missMedia := ""
	missSeason := ""
	missEpisode := ""
	if missingOnly {
		// Skip items that already have a provider id or store artwork.
		// Stub scan NFOs alone must not block enrich (they always set nfo_path).
		missMedia = ` AND (meta_id IS NULL OR meta_id='')
			AND (poster_path IS NULL OR poster_path='')`
		missSeason = ` AND (s.meta_id IS NULL OR s.meta_id='')
			AND (s.poster_path IS NULL OR s.poster_path='')`
		// Episodes: only still artwork matters (ffmpeg can fill after a prior meta-only enrich).
		missEpisode = ` AND (e.still_path IS NULL OR e.still_path='')`
	}
	var out []MetaTarget

	appendMovies := func() error {
		rows, err := d.SQL.QueryContext(ctx, `
			SELECT id FROM media_items WHERE library_id=? AND kind='movie'`+missMedia+` ORDER BY title`, libraryID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, MetaTarget{Scope: "movie", ID: id})
		}
		return rows.Err()
	}

	appendShowsSeasonsEpisodes := func() error {
		showRows, err := d.SQL.QueryContext(ctx, `
			SELECT id FROM media_items WHERE library_id=? AND kind='show'`+missMedia+` ORDER BY title`, libraryID)
		if err != nil {
			return err
		}
		for showRows.Next() {
			var id int64
			if err := showRows.Scan(&id); err != nil {
				showRows.Close()
				return err
			}
			out = append(out, MetaTarget{Scope: "show", ID: id})
		}
		err = showRows.Err()
		showRows.Close()
		if err != nil {
			return err
		}
		srows, err := d.SQL.QueryContext(ctx, `
			SELECT s.id FROM seasons s
			JOIN media_items m ON m.id = s.show_id
			WHERE m.library_id=?`+missSeason+`
			ORDER BY m.title, s.season_number`, libraryID)
		if err != nil {
			return err
		}
		for srows.Next() {
			var id int64
			if err := srows.Scan(&id); err != nil {
				srows.Close()
				return err
			}
			out = append(out, MetaTarget{Scope: "season", ID: id})
		}
		err = srows.Err()
		srows.Close()
		if err != nil {
			return err
		}
		erows, err := d.SQL.QueryContext(ctx, `
			SELECT e.id FROM episodes e
			JOIN media_items m ON m.id = e.show_id
			WHERE m.library_id=?`+missEpisode+`
			ORDER BY m.title, e.season_id, e.episode_number`, libraryID)
		if err != nil {
			return err
		}
		defer erows.Close()
		for erows.Next() {
			var id int64
			if err := erows.Scan(&id); err != nil {
				return err
			}
			out = append(out, MetaTarget{Scope: "episode", ID: id})
		}
		return erows.Err()
	}

	switch lib.Type {
	case "movies":
		return out, appendMovies()
	case "tv", "anime":
		// Films are kind=movie (film packs / root films); series are shows/seasons/episodes.
		if err := appendMovies(); err != nil {
			return nil, err
		}
		if err := appendShowsSeasonsEpisodes(); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return out, nil
	}
}

// ListMediaMetaTargets returns enrich targets for one movie or one show (+ seasons/episodes).
func (d *DB) ListMediaMetaTargets(ctx context.Context, mediaItemID int64, missingOnly bool) ([]MetaTarget, error) {
	it, err := d.GetMediaItem(ctx, mediaItemID)
	if err != nil || it == nil {
		return nil, err
	}
	missMedia := ""
	missSeason := ""
	missEpisode := ""
	if missingOnly {
		missMedia = ` AND (meta_id IS NULL OR meta_id='')
			AND (poster_path IS NULL OR poster_path='')`
		missSeason = ` AND (s.meta_id IS NULL OR s.meta_id='')
			AND (s.poster_path IS NULL OR s.poster_path='')`
		missEpisode = ` AND (e.still_path IS NULL OR e.still_path='')`
	}
	var out []MetaTarget
	switch it.Kind {
	case "movie":
		rows, err := d.SQL.QueryContext(ctx, `
			SELECT id FROM media_items WHERE id=? AND kind='movie'`+missMedia, mediaItemID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, MetaTarget{Scope: "movie", ID: id})
		}
		return out, rows.Err()
	case "show":
		showRows, err := d.SQL.QueryContext(ctx, `
			SELECT id FROM media_items WHERE id=? AND kind='show'`+missMedia, mediaItemID)
		if err != nil {
			return nil, err
		}
		for showRows.Next() {
			var id int64
			if err := showRows.Scan(&id); err != nil {
				showRows.Close()
				return nil, err
			}
			out = append(out, MetaTarget{Scope: "show", ID: id})
		}
		err = showRows.Err()
		showRows.Close()
		if err != nil {
			return nil, err
		}
		srows, err := d.SQL.QueryContext(ctx, `
			SELECT s.id FROM seasons s WHERE s.show_id=?`+missSeason+`
			ORDER BY s.season_number`, mediaItemID)
		if err != nil {
			return nil, err
		}
		for srows.Next() {
			var id int64
			if err := srows.Scan(&id); err != nil {
				srows.Close()
				return nil, err
			}
			out = append(out, MetaTarget{Scope: "season", ID: id})
		}
		err = srows.Err()
		srows.Close()
		if err != nil {
			return nil, err
		}
		erows, err := d.SQL.QueryContext(ctx, `
			SELECT e.id FROM episodes e WHERE e.show_id=?`+missEpisode+`
			ORDER BY e.season_id, e.episode_number`, mediaItemID)
		if err != nil {
			return nil, err
		}
		defer erows.Close()
		for erows.Next() {
			var id int64
			if err := erows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, MetaTarget{Scope: "episode", ID: id})
		}
		return out, erows.Err()
	default:
		return out, nil
	}
}

func (d *DB) libraryByID(ctx context.Context, id int64) (*Library, error) {
	lib := &Library{}
	err := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, name, type, path, created_at, updated_at FROM libraries WHERE id=?`, id).
		Scan(&lib.ID, &lib.UserID, &lib.Name, &lib.Type, &lib.Path, &lib.CreatedAt, &lib.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return lib, nil
}
