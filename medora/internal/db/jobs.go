package db

import (
	"context"
	"database/sql"
	"errors"
)

type AsyncJob struct {
	ID          int64
	Kind        string
	Scope       string
	TargetID    int64
	PayloadJSON sql.NullString
	Status      string
	ProgressPct int
	Message     sql.NullString
	StartedAt   sql.NullString
	FinishedAt  sql.NullString
}

func (d *DB) CreateAsyncJob(ctx context.Context, kind, scope string, targetID int64, payloadJSON string) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `
		INSERT INTO async_jobs(kind, scope, target_id, payload_json, status, progress_pct, message, started_at)
		VALUES (?,?,?,?,'running',0,'Starting',?)`,
		kind, scope, targetID, nullStr(payloadJSON), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateAsyncJob(ctx context.Context, id int64, status string, pct int, msg string) error {
	if status == "done" || status == "error" {
		_, err := d.SQL.ExecContext(ctx,
			`UPDATE async_jobs SET status=?, progress_pct=?, message=?, finished_at=? WHERE id=?`,
			status, pct, msg, now(), id)
		return err
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE async_jobs SET status=?, progress_pct=?, message=? WHERE id=?`,
		status, pct, msg, id)
	return err
}

func (d *DB) GetAsyncJob(ctx context.Context, id int64) (*AsyncJob, error) {
	j := &AsyncJob{}
	err := d.SQL.QueryRowContext(ctx, `
		SELECT id, kind, scope, target_id, payload_json, status, progress_pct, message, started_at, finished_at
		FROM async_jobs WHERE id=?`, id).
		Scan(&j.ID, &j.Kind, &j.Scope, &j.TargetID, &j.PayloadJSON, &j.Status, &j.ProgressPct, &j.Message, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

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

func (d *DB) GetSeason(ctx context.Context, id int64) (*Season, error) {
	s := &Season{}
	err := d.SQL.QueryRowContext(ctx, `SELECT id, show_id, season_number, title, poster_path, plot FROM seasons WHERE id=?`, id).
		Scan(&s.ID, &s.ShowID, &s.SeasonNumber, &s.Title, &s.PosterPath, &s.Plot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
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

func (d *DB) UserOwnsSeason(ctx context.Context, userID, seasonID int64) (bool, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM seasons s
		JOIN media_items m ON m.id = s.show_id
		JOIN libraries l ON l.id = m.library_id
		WHERE s.id = ? AND l.user_id = ?`, seasonID, userID).Scan(&n)
	return n > 0, err
}
