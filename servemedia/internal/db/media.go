package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Library struct {
	ID        int64
	UserID    int64
	Name      string
	Path      string
	CreatedAt string
	UpdatedAt string
}

type MediaItem struct {
	ID             int64
	LibraryID      int64
	Kind           string
	Title          string
	SortTitle      string
	Year           sql.NullInt64
	Path           string
	RuntimeSeconds sql.NullInt64
	Plot           sql.NullString
	PosterPath     sql.NullString
	BackdropPath   sql.NullString
	NFOPath        sql.NullString
	Rating         sql.NullFloat64
	MetaProvider      sql.NullString
	MetaID            sql.NullString
	MatchMediaSessionID sql.NullString
	MatchMediaJobID     sql.NullString
	MatchStatus       sql.NullString
	MatchError        sql.NullString
	Mtime             int64
	DateAdded         string
	ParentID          sql.NullInt64
}

const MatchStatusSkipped = "skipped"

func (it *MediaItem) MatchSkipped() bool {
	return it != nil && it.MatchStatus.Valid && strings.TrimSpace(it.MatchStatus.String) == MatchStatusSkipped
}

type Season struct {
	ID           int64
	ShowID       int64
	SeasonNumber int
	Title        sql.NullString
	PosterPath   sql.NullString
	Plot         sql.NullString
}

type Episode struct {
	ID             int64
	SeasonID       int64
	ShowID         int64
	EpisodeNumber  int
	Title          sql.NullString
	Path           string
	RuntimeSeconds sql.NullInt64
	Plot           sql.NullString
	StillPath      sql.NullString
	NFOPath        sql.NullString
	MetaProvider   sql.NullString
	MetaID         sql.NullString
	Mtime          int64
	DateAdded      string
}

type WatchProgress struct {
	ID              int64
	UserID          int64
	MediaItemID     sql.NullInt64
	EpisodeID       sql.NullInt64
	PositionSeconds float64
	DurationSeconds sql.NullFloat64
	Completed       bool
	UpdatedAt       string
	Title           string
	PosterPath      string
	Kind            string
	PlayURL         string
	ProgressPct     int
}

type ScanJob struct {
	ID          int64
	LibraryID   sql.NullInt64
	Status      string
	ProgressPct int
	Message     sql.NullString
	StartedAt   sql.NullString
	FinishedAt  sql.NullString
}

func (d *DB) CreateLibrary(ctx context.Context, userID int64, name, path string) (*Library, error) {
	t := now()
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO libraries(user_id, name, path, created_at, updated_at) VALUES (?,?,?,?,?)`,
		userID, name, path, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Library{ID: id, UserID: userID, Name: name, Path: path, CreatedAt: t, UpdatedAt: t}, nil
}

func (d *DB) ListLibraries(ctx context.Context, userID int64) ([]Library, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, user_id, name, path, created_at, updated_at FROM libraries WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Library
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Path, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) ListAllLibraries(ctx context.Context) ([]Library, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, user_id, name, path, created_at, updated_at FROM libraries ORDER BY user_id, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Library
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Path, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) GetLibrary(ctx context.Context, userID, id int64) (*Library, error) {
	l := &Library{}
	err := d.SQL.QueryRowContext(ctx,
		`SELECT id, user_id, name, path, created_at, updated_at FROM libraries WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&l.ID, &l.UserID, &l.Name, &l.Path, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (d *DB) RenameLibrary(ctx context.Context, userID, id int64, name string) error {
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE libraries SET name = ?, updated_at = ? WHERE id = ? AND user_id = ?`, name, now(), id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteLibrary removes store metadata files under storePath for the library,
// then deletes the library row (SQLite CASCADE removes related rows).
// Never touches files under the media library path — only storePath.
func (d *DB) DeleteLibrary(ctx context.Context, userID, id int64, storePath string) error {
	var n int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM libraries WHERE id=? AND user_id=?`, id, userID).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if storePath != "" {
		_ = d.purgeLibraryStoreFiles(ctx, id, storePath)
	}
	res, err := d.SQL.ExecContext(ctx, `DELETE FROM libraries WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) purgeLibraryStoreFiles(ctx context.Context, libraryID int64, storePath string) error {
	storePath = filepath.Clean(storePath)
	var rels []string
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT poster_path, backdrop_path, nfo_path FROM media_items WHERE library_id=?`, libraryID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var poster, backdrop, nfo sql.NullString
		if err := rows.Scan(&poster, &backdrop, &nfo); err != nil {
			rows.Close()
			return err
		}
		for _, p := range []sql.NullString{poster, backdrop, nfo} {
			if p.Valid && strings.TrimSpace(p.String) != "" {
				rels = append(rels, p.String)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	erows, err := d.SQL.QueryContext(ctx, `
		SELECT e.still_path, e.nfo_path FROM episodes e
		JOIN media_items m ON m.id = e.show_id
		WHERE m.library_id=?`, libraryID)
	if err != nil {
		return err
	}
	for erows.Next() {
		var still, nfo sql.NullString
		if err := erows.Scan(&still, &nfo); err != nil {
			erows.Close()
			return err
		}
		for _, p := range []sql.NullString{still, nfo} {
			if p.Valid && strings.TrimSpace(p.String) != "" {
				rels = append(rels, p.String)
			}
		}
	}
	err = erows.Err()
	erows.Close()
	if err != nil {
		return err
	}
	// Season posters also live under store.
	srows, err := d.SQL.QueryContext(ctx, `
		SELECT s.poster_path FROM seasons s
		JOIN media_items m ON m.id = s.show_id
		WHERE m.library_id=?`, libraryID)
	if err != nil {
		return err
	}
	for srows.Next() {
		var poster sql.NullString
		if err := srows.Scan(&poster); err != nil {
			srows.Close()
			return err
		}
		if poster.Valid && strings.TrimSpace(poster.String) != "" {
			rels = append(rels, poster.String)
		}
	}
	err = srows.Err()
	srows.Close()
	if err != nil {
		return err
	}
	return removeStoreRels(storePath, rels)
}

func (d *DB) purgeMediaItemStoreFiles(ctx context.Context, id int64, storePath string) error {
	var rels []string
	var poster, backdrop, nfo sql.NullString
	err := d.SQL.QueryRowContext(ctx, `
		SELECT poster_path, backdrop_path, nfo_path FROM media_items WHERE id=?`, id).Scan(&poster, &backdrop, &nfo)
	if err != nil {
		return err
	}
	for _, p := range []sql.NullString{poster, backdrop, nfo} {
		if p.Valid && strings.TrimSpace(p.String) != "" {
			rels = append(rels, p.String)
		}
	}
	erows, err := d.SQL.QueryContext(ctx, `SELECT still_path, nfo_path FROM episodes WHERE show_id=?`, id)
	if err != nil {
		return err
	}
	for erows.Next() {
		var still, epNFO sql.NullString
		if err := erows.Scan(&still, &epNFO); err != nil {
			erows.Close()
			return err
		}
		for _, p := range []sql.NullString{still, epNFO} {
			if p.Valid && strings.TrimSpace(p.String) != "" {
				rels = append(rels, p.String)
			}
		}
	}
	err = erows.Err()
	erows.Close()
	if err != nil {
		return err
	}
	srows, err := d.SQL.QueryContext(ctx, `SELECT poster_path FROM seasons WHERE show_id=?`, id)
	if err != nil {
		return err
	}
	for srows.Next() {
		var sp sql.NullString
		if err := srows.Scan(&sp); err != nil {
			srows.Close()
			return err
		}
		if sp.Valid && strings.TrimSpace(sp.String) != "" {
			rels = append(rels, sp.String)
		}
	}
	err = srows.Err()
	srows.Close()
	if err != nil {
		return err
	}
	return removeStoreRels(storePath, rels)
}

func removeStoreRels(storePath string, rels []string) error {
	storePath = filepath.Clean(storePath)
	if storePath == "" || storePath == "." {
		return nil
	}
	parents := map[string]struct{}{}
	for _, rel := range rels {
		rel = strings.TrimPrefix(filepath.Clean("/"+rel), "/")
		if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		full := filepath.Join(storePath, rel)
		if !strings.HasPrefix(full, storePath+string(os.PathSeparator)) && full != storePath {
			continue
		}
		_ = os.Remove(full)
		parents[filepath.Dir(full)] = struct{}{}
	}
	metaMovies := filepath.Join(storePath, "metadata", "movies")
	metaTV := filepath.Join(storePath, "metadata", "tv")
	for p := range parents {
		removeEmptyParents(p, metaMovies, metaTV)
	}
	return nil
}

// DissociateMediaItem deletes this item's store NFO/art, clears metadata columns,
// reverts title/year, and sets match_status to skipped.
func (d *DB) DissociateMediaItem(ctx context.Context, id int64, title string, year int, storePath string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("missing title")
	}
	if storePath != "" {
		_ = d.purgeMediaItemStoreFiles(ctx, id, storePath)
	}
	var yearArg any
	if year > 0 {
		yearArg = year
	}
	if _, err := d.SQL.ExecContext(ctx, `
		UPDATE media_items SET
			title=?, sort_title=?, year=?,
			plot=NULL, poster_path=NULL, backdrop_path=NULL, nfo_path=NULL, rating=NULL,
			meta_provider=NULL, meta_id=NULL,
			matchmedia_session_id=NULL, matchmedia_job_id=NULL,
			match_status=?, match_error=NULL
		WHERE id=?`, title, title, yearArg, MatchStatusSkipped, id); err != nil {
		return err
	}
	if _, err := d.SQL.ExecContext(ctx, `
		UPDATE seasons SET
			poster_path=NULL, plot=NULL, meta_provider=NULL, meta_id=NULL,
			title=CASE WHEN season_number=0 THEN 'Specials' ELSE 'Season ' || season_number END
		WHERE show_id=?`, id); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE episodes SET
			plot=NULL, still_path=NULL, nfo_path=NULL, meta_provider=NULL, meta_id=NULL
		WHERE show_id=?`, id)
	return err
}

func (d *DB) SetEpisodeTitle(ctx context.Context, id int64, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE episodes SET title=? WHERE id=?`, title, id)
	return err
}

// removeEmptyParents removes empty directories up to (but not including) stop roots.
func removeEmptyParents(dir, stopA, stopB string) {
	stopA = filepath.Clean(stopA)
	stopB = filepath.Clean(stopB)
	for {
		dir = filepath.Clean(dir)
		if dir == stopA || dir == stopB || dir == filepath.Dir(stopA) || dir == filepath.Dir(stopB) {
			return
		}
		if !strings.HasPrefix(dir, stopA+string(os.PathSeparator)) && !strings.HasPrefix(dir, stopB+string(os.PathSeparator)) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func (d *DB) UserOwnsMediaItem(ctx context.Context, userID, mediaItemID int64) (bool, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_items m
		JOIN libraries l ON l.id = m.library_id
		WHERE m.id = ? AND l.user_id = ?`, mediaItemID, userID).Scan(&n)
	return n > 0, err
}

func (d *DB) UserOwnsEpisode(ctx context.Context, userID, episodeID int64) (bool, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM episodes e
		JOIN media_items m ON m.id = e.show_id
		JOIN libraries l ON l.id = m.library_id
		WHERE e.id = ? AND l.user_id = ?`, episodeID, userID).Scan(&n)
	return n > 0, err
}

// GetMediaItemByMeta returns the canonical show in a library with this catalog id, or nil.
func (d *DB) GetMediaItemByMeta(ctx context.Context, libraryID int64, provider, id string) (*MediaItem, error) {
	provider = strings.TrimSpace(provider)
	id = strings.TrimSpace(id)
	if provider == "" || id == "" {
		return nil, nil
	}
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id FROM media_items
		WHERE library_id=? AND kind='show' AND meta_provider=? AND meta_id=?
		ORDER BY id`, libraryID, provider, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var itemID int64
		if err := rows.Scan(&itemID); err != nil {
			return nil, err
		}
		ids = append(ids, itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var best *MediaItem
	bestEps := -1
	bestSpecial := true
	for _, itemID := range ids {
		it, err := d.GetMediaItem(ctx, itemID)
		if err != nil || it == nil {
			continue
		}
		n, _ := d.CountEpisodesByShow(ctx, it.ID)
		special := extrasFolderPath(it.Path)
		better := false
		if best == nil {
			better = true
		} else if bestSpecial && !special {
			better = true
		} else if bestSpecial == special && n > bestEps {
			better = true
		}
		if better {
			best = it
			bestEps = n
			bestSpecial = special
		}
	}
	return best, nil
}

func extrasFolderPath(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	switch base {
	case "specials", "special", "ova", "ovas", "movies", "movie", "films", "film":
		return true
	}
	return strings.Contains(base, "special") || strings.Contains(base, "ova") ||
		strings.Contains(base, "movies") || strings.Contains(base, "films")
}

// ListShows returns show rows in a library, oldest first.
func (d *DB) ListShows(ctx context.Context, libraryID int64) ([]MediaItem, error) {
	items, err := d.ListMediaItems(ctx, libraryID, "", "")
	if err != nil {
		return nil, err
	}
	var out []MediaItem
	for _, it := range items {
		if it.Kind == "show" {
			out = append(out, it)
		}
	}
	return out, nil
}

func (d *DB) CountEpisodesByShow(ctx context.Context, showID int64) (int, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes WHERE show_id=?`, showID).Scan(&n)
	return n, err
}

func (d *DB) MaxEpisodeNumber(ctx context.Context, showID int64, season int) (int, error) {
	var n sql.NullInt64
	err := d.SQL.QueryRowContext(ctx, `
		SELECT MAX(e.episode_number) FROM episodes e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.show_id=? AND s.season_number=?`, showID, season).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

func (d *DB) DeleteMediaItem(ctx context.Context, id int64) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM media_items WHERE id=?`, id)
	return err
}

func (d *DB) UpdateMediaItemPath(ctx context.Context, id int64, path string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET path=? WHERE id=?`, path, id)
	return err
}

// GetMediaItemByPath returns the media item for a library path, or nil.
func (d *DB) GetMediaItemByPath(ctx context.Context, libraryID int64, path string) (*MediaItem, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM media_items WHERE library_id = ? AND path = ?`, libraryID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetMediaItem(ctx, id)
}

// TouchMediaItemMtime updates only mtime (used when scan must not re-poison meta).
func (d *DB) TouchMediaItemMtime(ctx context.Context, id int64, mtime int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET mtime=? WHERE id=?`, mtime, id)
	return err
}

func (d *DB) MediaItemExistsAtPath(ctx context.Context, libraryID int64, path string) (bool, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM media_items WHERE library_id = ? AND path = ?`, libraryID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (d *DB) EpisodeExistsAtPath(ctx context.Context, showID int64, path string) (bool, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM episodes WHERE show_id = ? AND path = ?`, showID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (d *DB) DeleteEpisodesUnderPath(ctx context.Context, showID int64, root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if showID <= 0 || root == "" || root == "." {
		return nil
	}
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM episodes WHERE show_id=? AND (path=? OR path LIKE ?)`,
		showID, root, root+string(os.PathSeparator)+"%")
	return err
}

func (d *DB) DeleteEpisode(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM episodes WHERE id=?`, id)
	return err
}

func (d *DB) UpsertMediaItem(ctx context.Context, it MediaItem) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM media_items WHERE library_id = ? AND path = ?`, it.LibraryID, it.Path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := d.SQL.ExecContext(ctx, `
			INSERT INTO media_items(library_id, kind, title, sort_title, year, path, runtime_seconds, plot, poster_path, backdrop_path, nfo_path, rating, mtime, date_added)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.LibraryID, it.Kind, it.Title, it.SortTitle, it.Year, it.Path, it.RuntimeSeconds, it.Plot, it.PosterPath, it.BackdropPath, it.NFOPath, it.Rating, it.Mtime, now())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	_, err = d.SQL.ExecContext(ctx, `
		UPDATE media_items SET kind=?, title=?, sort_title=?, year=?, runtime_seconds=?, plot=?, poster_path=?, backdrop_path=?, nfo_path=?, rating=?, mtime=?
		WHERE id=?`, it.Kind, it.Title, it.SortTitle, it.Year, it.RuntimeSeconds, it.Plot, it.PosterPath, it.BackdropPath, it.NFOPath, it.Rating, it.Mtime, id)
	return id, err
}

// showPosterSQL coalesces a media row's poster with the first season poster
// (S1, then other numbered seasons, then S0). Movies have no seasons, so this
// is the item poster.
func showPosterSQL(alias string) string {
	idCol, posterCol := "media_items.id", "media_items.poster_path"
	if alias != "" {
		idCol = alias + ".id"
		posterCol = alias + ".poster_path"
	}
	return `COALESCE(NULLIF(` + posterCol + `,''),(SELECT s.poster_path FROM seasons s WHERE s.show_id=` + idCol + ` AND s.poster_path IS NOT NULL AND s.poster_path!='' ORDER BY CASE WHEN s.season_number=1 THEN 0 WHEN s.season_number=0 THEN 2 ELSE 1 END, s.season_number LIMIT 1))`
}

func (d *DB) GetMediaItem(ctx context.Context, id int64) (*MediaItem, error) {
	it := &MediaItem{}
	err := d.SQL.QueryRowContext(ctx, `
		SELECT id, library_id, kind, title, COALESCE(sort_title,''), year, path, runtime_seconds, plot, `+showPosterSQL("")+`, backdrop_path, nfo_path,
			meta_provider, meta_id, matchmedia_session_id, matchmedia_job_id, match_status, match_error, mtime, date_added, parent_id
		FROM media_items WHERE id = ?`, id).
		Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.SortTitle, &it.Year, &it.Path, &it.RuntimeSeconds, &it.Plot, &it.PosterPath, &it.BackdropPath, &it.NFOPath,
			&it.MetaProvider, &it.MetaID, &it.MatchMediaSessionID, &it.MatchMediaJobID, &it.MatchStatus, &it.MatchError, &it.Mtime, &it.DateAdded, &it.ParentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) SetMatchMediaMatch(ctx context.Context, id int64, session, jobID, status, matchErr string) error {
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE media_items SET matchmedia_session_id=NULLIF(?,''), matchmedia_job_id=NULLIF(?,''), match_status=NULLIF(?, ''), match_error=NULLIF(?, '') WHERE id=?`,
		session, jobID, status, matchErr, id)
	return err
}

func (d *DB) ClearMatchMediaMatch(ctx context.Context, id int64) error {
	return d.SetMatchMediaMatch(ctx, id, "", "", "", "")
}

func (d *DB) SkipMatchMedia(ctx context.Context, id int64) error {
	return d.SetMatchMediaMatch(ctx, id, "", "", MatchStatusSkipped, "")
}

func (d *DB) ListLibraryPosters(ctx context.Context, libraryID int64) ([]string, error) {
	const maxStrips = 3
	var total int
	if err := d.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_items
		WHERE library_id = ? AND poster_path IS NOT NULL AND poster_path != ''`, libraryID).Scan(&total); err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}

	rows, err := d.SQL.QueryContext(ctx, `
		SELECT poster_path FROM media_items
		WHERE library_id = ? AND poster_path IS NOT NULL AND poster_path != ''
		  AND rating IS NOT NULL AND rating > 0
		ORDER BY rating DESC, date_added DESC, id DESC LIMIT ?`, libraryID, maxStrips)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	chosen := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
		chosen[p] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) >= maxStrips || len(out) >= total {
		return out, nil
	}

	fillRows, err := d.SQL.QueryContext(ctx, `
		SELECT poster_path FROM media_items
		WHERE library_id = ? AND poster_path IS NOT NULL AND poster_path != ''
		ORDER BY date_added DESC, id DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer fillRows.Close()
	for fillRows.Next() {
		if len(out) >= maxStrips {
			break
		}
		var p string
		if err := fillRows.Scan(&p); err != nil {
			return nil, err
		}
		if chosen[p] {
			continue
		}
		out = append(out, p)
		chosen[p] = true
	}
	return out, fillRows.Err()
}

func (d *DB) ListMediaItems(ctx context.Context, libraryID int64, sort, q string) ([]MediaItem, error) {
	order := `title COLLATE NOCASE`
	switch sort {
	case "date_added":
		order = `date_added DESC`
	case "year":
		order = `year DESC, title COLLATE NOCASE`
	}
	query := `SELECT id, library_id, kind, title, COALESCE(sort_title,''), year, path, runtime_seconds, plot, ` + showPosterSQL("") + `, backdrop_path, nfo_path,
			meta_provider, meta_id, matchmedia_session_id, matchmedia_job_id, match_status, match_error, mtime, date_added, parent_id
		FROM media_items WHERE library_id = ?`
	args := []any{libraryID}
	if q != "" {
		query += ` AND title LIKE ?`
		args = append(args, "%"+q+"%")
	} else {
		query += ` AND parent_id IS NULL`
	}
	query += ` ORDER BY ` + order
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		var it MediaItem
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.SortTitle, &it.Year, &it.Path, &it.RuntimeSeconds, &it.Plot, &it.PosterPath, &it.BackdropPath, &it.NFOPath,
			&it.MetaProvider, &it.MetaID, &it.MatchMediaSessionID, &it.MatchMediaJobID, &it.MatchStatus, &it.MatchError, &it.Mtime, &it.DateAdded, &it.ParentID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) ListAllMediaItems(ctx context.Context, libraryID int64) ([]MediaItem, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, library_id, kind, title, COALESCE(sort_title,''), year, path, runtime_seconds, plot, `+showPosterSQL("")+`, backdrop_path, nfo_path,
			meta_provider, meta_id, matchmedia_session_id, matchmedia_job_id, match_status, match_error, mtime, date_added, parent_id
		FROM media_items WHERE library_id=? ORDER BY id`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		var it MediaItem
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.SortTitle, &it.Year, &it.Path, &it.RuntimeSeconds, &it.Plot, &it.PosterPath, &it.BackdropPath, &it.NFOPath,
			&it.MetaProvider, &it.MetaID, &it.MatchMediaSessionID, &it.MatchMediaJobID, &it.MatchStatus, &it.MatchError, &it.Mtime, &it.DateAdded, &it.ParentID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) ListChildMediaItems(ctx context.Context, parentID int64) ([]MediaItem, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, library_id, kind, title, COALESCE(sort_title,''), year, path, runtime_seconds, plot, `+showPosterSQL("")+`, backdrop_path, nfo_path,
			meta_provider, meta_id, matchmedia_session_id, matchmedia_job_id, match_status, match_error, mtime, date_added, parent_id
		FROM media_items WHERE parent_id=? ORDER BY title COLLATE NOCASE`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		var it MediaItem
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.SortTitle, &it.Year, &it.Path, &it.RuntimeSeconds, &it.Plot, &it.PosterPath, &it.BackdropPath, &it.NFOPath,
			&it.MetaProvider, &it.MetaID, &it.MatchMediaSessionID, &it.MatchMediaJobID, &it.MatchStatus, &it.MatchError, &it.Mtime, &it.DateAdded, &it.ParentID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) SetMediaItemParent(ctx context.Context, id, parentID int64) error {
	if id <= 0 || id == parentID {
		return nil
	}
	if parentID <= 0 {
		_, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET parent_id=NULL WHERE id=?`, id)
		return err
	}
	parent, err := d.GetMediaItem(ctx, parentID)
	if err != nil || parent == nil {
		return err
	}
	if parent.ParentID.Valid && parent.ParentID.Int64 == id {
		return nil
	}
	_, err = d.SQL.ExecContext(ctx, `UPDATE media_items SET parent_id=? WHERE id=?`, parentID, id)
	return err
}

func (d *DB) RecentlyAdded(ctx context.Context, userID int64, limit int) ([]MediaItem, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT m.id, m.library_id, m.kind, m.title, COALESCE(m.sort_title,''), m.year, m.path, m.runtime_seconds, m.plot, `+showPosterSQL("m")+`, m.backdrop_path, m.nfo_path,
			m.meta_provider, m.meta_id, m.matchmedia_session_id, m.matchmedia_job_id, m.match_status, m.match_error, m.mtime, m.date_added, m.parent_id
		FROM media_items m
		JOIN libraries l ON l.id = m.library_id
		WHERE l.user_id = ? AND m.parent_id IS NULL
		ORDER BY m.date_added DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		var it MediaItem
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.SortTitle, &it.Year, &it.Path, &it.RuntimeSeconds, &it.Plot, &it.PosterPath, &it.BackdropPath, &it.NFOPath,
			&it.MetaProvider, &it.MetaID, &it.MatchMediaSessionID, &it.MatchMediaJobID, &it.MatchStatus, &it.MatchError, &it.Mtime, &it.DateAdded, &it.ParentID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) UpsertSeason(ctx context.Context, showID int64, num int, title, poster, plot string) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM seasons WHERE show_id=? AND season_number=?`, showID, num).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := d.SQL.ExecContext(ctx, `INSERT INTO seasons(show_id, season_number, title, poster_path, plot) VALUES (?,?,?,?,?)`,
			showID, num, nullStr(title), nullStr(poster), nullStr(plot))
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	_, err = d.SQL.ExecContext(ctx, `
		UPDATE seasons SET
			title=COALESCE(?, title),
			poster_path=COALESCE(?, poster_path),
			plot=COALESCE(?, plot)
		WHERE id=?`, nullStr(title), nullStr(poster), nullStr(plot), id)
	return id, err
}

func (d *DB) ListSeasons(ctx context.Context, showID int64) ([]Season, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, show_id, season_number, title, poster_path, plot FROM seasons WHERE show_id=? ORDER BY season_number`, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Season
	for rows.Next() {
		var s Season
		if err := rows.Scan(&s.ID, &s.ShowID, &s.SeasonNumber, &s.Title, &s.PosterPath, &s.Plot); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) UpsertEpisode(ctx context.Context, ep Episode) (int64, error) {
	var pathID, slotID int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM episodes WHERE show_id=? AND path=?`, ep.ShowID, ep.Path).Scan(&pathID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	hasPath := err == nil
	err = d.SQL.QueryRowContext(ctx, `SELECT id FROM episodes WHERE season_id=? AND episode_number=?`, ep.SeasonID, ep.EpisodeNumber).Scan(&slotID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	hasSlot := err == nil

	updateRow := func(id int64) (int64, error) {
		_, err := d.SQL.ExecContext(ctx, `
			UPDATE episodes SET season_id=?, episode_number=?, title=?, path=?, runtime_seconds=?, plot=?, still_path=?, nfo_path=?, mtime=?
			WHERE id=?`,
			ep.SeasonID, ep.EpisodeNumber, ep.Title, ep.Path, ep.RuntimeSeconds, ep.Plot, ep.StillPath, ep.NFOPath, ep.Mtime, id)
		return id, err
	}

	// Path is durable identity: reassign season/episode when folder layout changes.
	if hasPath {
		if hasSlot && slotID != pathID {
			// Slot taken by a different file; drop it so this path can move in.
			// The other path is re-ingested later in the same scan if still on disk.
			if _, err := d.SQL.ExecContext(ctx, `DELETE FROM episodes WHERE id=?`, slotID); err != nil {
				return 0, err
			}
		}
		return updateRow(pathID)
	}
	if hasSlot {
		return updateRow(slotID)
	}
	res, err := d.SQL.ExecContext(ctx, `
		INSERT INTO episodes(season_id, show_id, episode_number, title, path, runtime_seconds, plot, still_path, nfo_path, mtime, date_added)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		ep.SeasonID, ep.ShowID, ep.EpisodeNumber, ep.Title, ep.Path, ep.RuntimeSeconds, ep.Plot, ep.StillPath, ep.NFOPath, ep.Mtime, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListEpisodes(ctx context.Context, seasonID int64) ([]Episode, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, season_id, show_id, episode_number, title, path, runtime_seconds, plot, still_path, nfo_path, mtime, date_added
		FROM episodes WHERE season_id=? ORDER BY episode_number`, seasonID)
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

func (d *DB) GetEpisode(ctx context.Context, id int64) (*Episode, error) {
	e := &Episode{}
	err := d.SQL.QueryRowContext(ctx, `
		SELECT id, season_id, show_id, episode_number, title, path, runtime_seconds, plot, still_path, nfo_path,
			meta_provider, meta_id, mtime, date_added
		FROM episodes WHERE id=?`, id).
		Scan(&e.ID, &e.SeasonID, &e.ShowID, &e.EpisodeNumber, &e.Title, &e.Path, &e.RuntimeSeconds, &e.Plot, &e.StillPath, &e.NFOPath,
			&e.MetaProvider, &e.MetaID, &e.Mtime, &e.DateAdded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// AdjacentEpisodes returns the previous and next episodes in show order
// (season_number, episode_number). Either may be nil at the ends of the show.
func (d *DB) AdjacentEpisodes(ctx context.Context, episodeID int64) (prev, next *Episode, err error) {
	ep, err := d.GetEpisode(ctx, episodeID)
	if err != nil || ep == nil {
		return nil, nil, err
	}
	var seasonNum int
	if err := d.SQL.QueryRowContext(ctx, `SELECT season_number FROM seasons WHERE id=?`, ep.SeasonID).Scan(&seasonNum); err != nil {
		return nil, nil, err
	}
	scanEp := func(row *sql.Row) (*Episode, error) {
		e := &Episode{}
		err := row.Scan(&e.ID, &e.SeasonID, &e.ShowID, &e.EpisodeNumber, &e.Title, &e.Path, &e.RuntimeSeconds, &e.Plot, &e.StillPath, &e.NFOPath, &e.Mtime, &e.DateAdded)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return e, nil
	}
	const cols = `e.id, e.season_id, e.show_id, e.episode_number, e.title, e.path, e.runtime_seconds, e.plot, e.still_path, e.nfo_path, e.mtime, e.date_added`
	prev, err = scanEp(d.SQL.QueryRowContext(ctx, `
		SELECT `+cols+`
		FROM episodes e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.show_id = ?
		  AND (s.season_number < ? OR (s.season_number = ? AND e.episode_number < ?))
		ORDER BY s.season_number DESC, e.episode_number DESC
		LIMIT 1`, ep.ShowID, seasonNum, seasonNum, ep.EpisodeNumber))
	if err != nil {
		return nil, nil, err
	}
	next, err = scanEp(d.SQL.QueryRowContext(ctx, `
		SELECT `+cols+`
		FROM episodes e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.show_id = ?
		  AND (s.season_number > ? OR (s.season_number = ? AND e.episode_number > ?))
		ORDER BY s.season_number ASC, e.episode_number ASC
		LIMIT 1`, ep.ShowID, seasonNum, seasonNum, ep.EpisodeNumber))
	if err != nil {
		return nil, nil, err
	}
	return prev, next, nil
}

func (d *DB) GetSeasonByShowNum(ctx context.Context, showID int64, num int) (*Season, error) {
	s := &Season{}
	err := d.SQL.QueryRowContext(ctx, `SELECT id, show_id, season_number, title, poster_path, plot FROM seasons WHERE show_id=? AND season_number=?`, showID, num).
		Scan(&s.ID, &s.ShowID, &s.SeasonNumber, &s.Title, &s.PosterPath, &s.Plot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

func (d *DB) CountEpisodes(ctx context.Context, seasonID int64) (int, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes WHERE season_id=?`, seasonID).Scan(&n)
	return n, err
}

func (d *DB) UpsertWatchMovie(ctx context.Context, userID, movieID int64, pos, dur float64, completed bool) error {
	comp := 0
	if completed {
		comp = 1
	}
	if _, err := d.SQL.ExecContext(ctx, `DELETE FROM watch_progress WHERE user_id=? AND media_item_id=?`, userID, movieID); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO watch_progress(user_id, media_item_id, episode_id, position_seconds, duration_seconds, completed, updated_at)
		VALUES (?,?,NULL,?,?,?,?)`, userID, movieID, pos, dur, comp, now())
	return err
}

func (d *DB) UpsertWatchEpisode(ctx context.Context, userID, episodeID int64, pos, dur float64, completed bool) error {
	comp := 0
	if completed {
		comp = 1
	}
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM watch_progress WHERE user_id=? AND episode_id=?`, userID, episodeID)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `
		INSERT INTO watch_progress(user_id, media_item_id, episode_id, position_seconds, duration_seconds, completed, updated_at)
		VALUES (?,NULL,?,?,?,?,?)`, userID, episodeID, pos, dur, comp, now())
	return err
}

func progressPctFromRow(pos float64, dur sql.NullFloat64, completed int) int {
	if completed == 1 {
		return 100
	}
	if !dur.Valid || dur.Float64 <= 0 {
		return 0
	}
	p := int((pos / dur.Float64) * 100)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// WatchProgressPctByMovieIDs returns watch % for each movie id (missing → 0).
func (d *DB) WatchProgressPctByMovieIDs(ctx context.Context, userID int64, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = 0
	}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT media_item_id, position_seconds, duration_seconds, completed
		FROM watch_progress WHERE user_id=? AND media_item_id IN (` + placeholders(len(ids)) + `)`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var pos float64
		var dur sql.NullFloat64
		var completed int
		if err := rows.Scan(&mid, &pos, &dur, &completed); err != nil {
			return out, err
		}
		out[mid] = progressPctFromRow(pos, dur, completed)
	}
	return out, rows.Err()
}

// WatchProgressPctByEpisodeIDs returns watch % for each episode id (missing → 0).
func (d *DB) WatchProgressPctByEpisodeIDs(ctx context.Context, userID int64, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = 0
	}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT episode_id, position_seconds, duration_seconds, completed
		FROM watch_progress WHERE user_id=? AND episode_id IN (` + placeholders(len(ids)) + `)`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var eid int64
		var pos float64
		var dur sql.NullFloat64
		var completed int
		if err := rows.Scan(&eid, &pos, &dur, &completed); err != nil {
			return out, err
		}
		out[eid] = progressPctFromRow(pos, dur, completed)
	}
	return out, rows.Err()
}

func (d *DB) episodeProgressAverage(ctx context.Context, userID int64, whereSQL string, arg any) (int, error) {
	var total int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes WHERE `+whereSQL, arg).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT e.id, COALESCE(wp.position_seconds, 0), wp.duration_seconds, COALESCE(wp.completed, 0)
		FROM episodes e
		LEFT JOIN watch_progress wp ON wp.episode_id = e.id AND wp.user_id = ?
		WHERE e.`+whereSQL, userID, arg)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	sum := 0
	for rows.Next() {
		var eid int64
		var pos float64
		var dur sql.NullFloat64
		var completed int
		if err := rows.Scan(&eid, &pos, &dur, &completed); err != nil {
			return 0, err
		}
		sum += progressPctFromRow(pos, dur, completed)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return sum / total, nil
}

// ShowWatchProgressPct averages episode watch % across the show (no progress → 0).
func (d *DB) ShowWatchProgressPct(ctx context.Context, userID, showID int64) (int, error) {
	return d.episodeProgressAverage(ctx, userID, `show_id=?`, showID)
}

// SeasonWatchProgressPct averages episode watch % across the season.
func (d *DB) SeasonWatchProgressPct(ctx context.Context, userID, seasonID int64) (int, error) {
	return d.episodeProgressAverage(ctx, userID, `season_id=?`, seasonID)
}

// MediaItemsProgressPct returns progress for movie/show media items.
func (d *DB) MediaItemsProgressPct(ctx context.Context, userID int64, items []MediaItem) (map[int64]int, error) {
	out := make(map[int64]int, len(items))
	var movieIDs, showIDs []int64
	for _, it := range items {
		out[it.ID] = 0
		switch it.Kind {
		case "movie":
			movieIDs = append(movieIDs, it.ID)
		case "show":
			showIDs = append(showIDs, it.ID)
		}
	}
	movies, err := d.WatchProgressPctByMovieIDs(ctx, userID, movieIDs)
	if err != nil {
		return out, err
	}
	for id, p := range movies {
		out[id] = p
	}
	for _, id := range showIDs {
		p, err := d.ShowWatchProgressPct(ctx, userID, id)
		if err != nil {
			return out, err
		}
		out[id] = p
	}
	return out, nil
}

func (d *DB) ContinueWatching(ctx context.Context, userID int64, limit int) ([]WatchProgress, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT wp.id, wp.user_id, wp.media_item_id, wp.episode_id, wp.position_seconds, wp.duration_seconds, wp.completed, wp.updated_at,
			COALESCE(m.title, e.title, 'Item'), COALESCE(m.poster_path, sm.poster_path, ''), COALESCE(m.kind, 'episode')
		FROM watch_progress wp
		LEFT JOIN media_items m ON m.id = wp.media_item_id
		LEFT JOIN libraries lm ON lm.id = m.library_id AND lm.user_id = wp.user_id
		LEFT JOIN episodes e ON e.id = wp.episode_id
		LEFT JOIN media_items sm ON sm.id = e.show_id
		LEFT JOIN libraries le ON le.id = sm.library_id AND le.user_id = wp.user_id
		WHERE wp.user_id = ? AND wp.completed = 0 AND wp.position_seconds > 5
			AND (
				(wp.media_item_id IS NOT NULL AND lm.id IS NOT NULL) OR
				(wp.episode_id IS NOT NULL AND le.id IS NOT NULL)
			)
		ORDER BY wp.updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchProgress
	for rows.Next() {
		var w WatchProgress
		var completed int
		if err := rows.Scan(&w.ID, &w.UserID, &w.MediaItemID, &w.EpisodeID, &w.PositionSeconds, &w.DurationSeconds, &completed, &w.UpdatedAt, &w.Title, &w.PosterPath, &w.Kind); err != nil {
			return nil, err
		}
		w.Completed = completed == 1
		if w.DurationSeconds.Valid && w.DurationSeconds.Float64 > 0 {
			p := int((w.PositionSeconds / w.DurationSeconds.Float64) * 100)
			if p < 0 {
				p = 0
			}
			if p > 100 {
				p = 100
			}
			w.ProgressPct = p
		}
		t := int(w.PositionSeconds)
		if w.MediaItemID.Valid {
			w.PlayURL = "/play/movie/" + itoa(w.MediaItemID.Int64) + "?t=" + itoa(int64(t))
		} else if w.EpisodeID.Valid {
			w.PlayURL = "/play/episode/" + itoa(w.EpisodeID.Int64) + "?t=" + itoa(int64(t))
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) CreateScanJob(ctx context.Context, libraryID int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `
		INSERT INTO scan_jobs(library_id, status, progress_pct, message, started_at) VALUES (?,'running',0,'Starting',?)`,
		libraryID, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateScanJob(ctx context.Context, id int64, status string, pct int, msg string) error {
	if status == "done" || status == "error" {
		_, err := d.SQL.ExecContext(ctx, `UPDATE scan_jobs SET status=?, progress_pct=?, message=?, finished_at=? WHERE id=?`, status, pct, msg, now(), id)
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE scan_jobs SET status=?, progress_pct=?, message=? WHERE id=?`, status, pct, msg, id)
	return err
}

func (d *DB) GetScanJob(ctx context.Context, id int64) (*ScanJob, error) {
	j := &ScanJob{}
	err := d.SQL.QueryRowContext(ctx, `SELECT id, library_id, status, progress_pct, message, started_at, finished_at FROM scan_jobs WHERE id=?`, id).
		Scan(&j.ID, &j.LibraryID, &j.Status, &j.ProgressPct, &j.Message, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (d *DB) LatestScanForLibrary(ctx context.Context, libraryID int64) (*ScanJob, error) {
	j := &ScanJob{}
	err := d.SQL.QueryRowContext(ctx, `SELECT id, library_id, status, progress_pct, message, started_at, finished_at FROM scan_jobs WHERE library_id=? ORDER BY id DESC LIMIT 1`, libraryID).
		Scan(&j.ID, &j.LibraryID, &j.Status, &j.ProgressPct, &j.Message, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// RunningScanForLibrary returns the newest running scan job for a library, if any.
func (d *DB) RunningScanForLibrary(ctx context.Context, libraryID int64) (*ScanJob, error) {
	j := &ScanJob{}
	err := d.SQL.QueryRowContext(ctx, `
		SELECT id, library_id, status, progress_pct, message, started_at, finished_at
		FROM scan_jobs WHERE library_id=? AND status='running' ORDER BY id DESC LIMIT 1`, libraryID).
		Scan(&j.ID, &j.LibraryID, &j.Status, &j.ProgressPct, &j.Message, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func itoa(n int64) string {
	return fmtInt(n)
}

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
