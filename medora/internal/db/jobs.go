package db

import (
	"context"
)

func (d *DB) ListEpisodesByShow(ctx context.Context, showID int64) ([]Episode, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT e.id, e.season_id, e.show_id, e.episode_number, e.title, e.path, e.runtime_seconds, e.plot, e.still_path, e.nfo_path, e.mtime, e.date_added
		FROM episodes e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.show_id=?
		ORDER BY s.season_number, e.episode_number`, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		var e Episode
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.ShowID, &e.EpisodeNumber, &e.Title, &e.Path, &e.RuntimeSeconds, &e.Plot, &e.StillPath, &e.NFOPath, &e.Mtime, &e.DateAdded); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) UpdateMediaItemMeta(ctx context.Context, id int64, title string, year int, plot, poster, backdrop, nfo string, rating float64, metaProvider, metaID string) error {
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE media_items SET
			title=COALESCE(NULLIF(?,''), title),
			year=CASE WHEN ? > 0 THEN ? ELSE year END,
			plot=COALESCE(NULLIF(?,''), plot),
			poster_path=COALESCE(NULLIF(?,''), poster_path),
			backdrop_path=COALESCE(NULLIF(?,''), backdrop_path),
			nfo_path=COALESCE(NULLIF(?,''), nfo_path),
			rating=CASE WHEN ? > 0 THEN ? ELSE rating END,
			meta_provider=COALESCE(NULLIF(?,''), meta_provider),
			meta_id=COALESCE(NULLIF(?,''), meta_id)
		WHERE id=?`,
		title, year, year, plot, poster, backdrop, nfo, rating, rating, metaProvider, metaID, id)
	return err
}

func (d *DB) UpdateSeasonMeta(ctx context.Context, id int64, title, plot, poster, metaProvider, metaID string) error {
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE seasons SET
			title=COALESCE(NULLIF(?,''), title),
			plot=COALESCE(NULLIF(?,''), plot),
			poster_path=COALESCE(NULLIF(?,''), poster_path),
			meta_provider=COALESCE(NULLIF(?,''), meta_provider),
			meta_id=COALESCE(NULLIF(?,''), meta_id)
		WHERE id=?`,
		title, plot, poster, metaProvider, metaID, id)
	return err
}

func (d *DB) UpdateEpisodeMeta(ctx context.Context, id int64, title, plot, still, nfo, metaProvider, metaID string) error {
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE episodes SET
			title=COALESCE(NULLIF(?,''), title),
			plot=COALESCE(NULLIF(?,''), plot),
			still_path=COALESCE(NULLIF(?,''), still_path),
			nfo_path=COALESCE(NULLIF(?,''), nfo_path),
			meta_provider=COALESCE(NULLIF(?,''), meta_provider),
			meta_id=COALESCE(NULLIF(?,''), meta_id)
		WHERE id=?`,
		title, plot, still, nfo, metaProvider, metaID, id)
	return err
}
