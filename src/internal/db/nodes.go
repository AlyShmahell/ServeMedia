package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type LibraryNode struct {
	ID           int64
	LibraryID    int64
	ParentID     sql.NullInt64
	RelPath      string
	Name         string
	NodeKind     string
	MMKind       sql.NullString
	MMTitle      sql.NullString
	MMYear       sql.NullString
	PosterPath   sql.NullString
	MatchStatus  sql.NullString
	MetaProvider sql.NullString
	MetaID       sql.NullString
	Plot         sql.NullString
	GroupNodeID  sql.NullInt64
	SeasonNum    sql.NullString
	EpisodeNum   sql.NullString
	FileTitle    sql.NullString
	FilePlot     sql.NullString
	StillPath    sql.NullString
	DateAdded    string
}

type FileWatch struct {
	ID   int64
	Rel  string
	Pct  int
	Done bool
}

type NodeGroup struct {
	Source      string
	Role        string
	Kind        string
	Title       string
	Year        string
	ParentTitle string
	Status      string
	Err         string
	Ranker      string
	Provider    string
	MetaID      string
	Score       float64
	Jaccard     float64
	CatalogFor  string
	Plot        string
	Poster      string
	Candidates  string
}

func (d *DB) EnsureLibraryRoot(ctx context.Context, libraryID int64) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM library_nodes WHERE library_id=? AND rel_path=''`, libraryID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO library_nodes(library_id, parent_id, rel_path, name, node_kind, date_added) VALUES (?, NULL, '', '', 'dir', ?)`, libraryID, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpsertNodeChain(ctx context.Context, libraryID int64, rel, leafKind string) (int64, error) {
	root, err := d.EnsureLibraryRoot(ctx, libraryID)
	if err != nil {
		return 0, err
	}
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return root, nil
	}
	parent := root
	parts := strings.Split(rel, "/")
	cur := ""
	var id int64
	for i, part := range parts {
		if part == "" {
			continue
		}
		if cur == "" {
			cur = part
		} else {
			cur += "/" + part
		}
		kind := "dir"
		if i == len(parts)-1 {
			kind = leafKind
		}
		id, err = d.upsertNode(ctx, libraryID, parent, cur, part, kind)
		if err != nil {
			return 0, err
		}
		parent = id
	}
	return id, nil
}

func (d *DB) upsertNode(ctx context.Context, libraryID, parentID int64, rel, name, kind string) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM library_nodes WHERE library_id=? AND rel_path=?`, libraryID, rel).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := d.SQL.ExecContext(ctx, `INSERT INTO library_nodes(library_id, parent_id, rel_path, name, node_kind, date_added, mtime) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			libraryID, parentID, rel, name, kind, now(), time.Now().Unix())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	_, err = d.SQL.ExecContext(ctx, `UPDATE library_nodes SET parent_id=?, name=?, node_kind=? WHERE id=?`, parentID, name, kind, id)
	return id, err
}

func (d *DB) SetNodeStill(ctx context.Context, id int64, still string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET still_path=NULLIF(?,'') WHERE id=?`, still, id)
	return err
}

func (d *DB) SetNodePoster(ctx context.Context, id int64, poster string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET poster_path=NULLIF(?,'') WHERE id=?`, poster, id)
	return err
}

// SetNodeMatchIdentity records a resolved match on a directory node.
// Title and stored candidates are left as they are.
func (d *DB) SetNodeMatchIdentity(ctx context.Context, id int64, status, provider, metaID, poster string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET
		match_status=NULLIF(?,''), match_error=NULL,
		meta_provider=NULLIF(?,''), meta_id=NULLIF(?,''),
		poster_path=COALESCE(NULLIF(?,''), poster_path)
		WHERE id=?`, status, provider, metaID, poster, id)
	return err
}

func (d *DB) SetNodeGroup(ctx context.Context, id int64, g NodeGroup) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET
		mm_source=NULLIF(?,''), mm_role=NULLIF(?,''), mm_kind=NULLIF(?,''), mm_title=NULLIF(?,''), mm_year=NULLIF(?,''),
		mm_parent_title=NULLIF(?,''), match_status=NULLIF(?,''), match_error=NULLIF(?,''), ranker=NULLIF(?,''),
		meta_provider=NULLIF(?,''), meta_id=NULLIF(?,''), match_score=?, match_jaccard=?, catalog_for=NULLIF(?,''),
		plot=NULLIF(?,''), poster_path=COALESCE(NULLIF(?,''), poster_path), match_candidates=NULLIF(?,'')
		WHERE id=?`,
		g.Source, g.Role, g.Kind, g.Title, g.Year, g.ParentTitle, g.Status, g.Err, g.Ranker,
		g.Provider, g.MetaID, g.Score, g.Jaccard, g.CatalogFor, g.Plot, g.Poster, g.Candidates, id)
	return err
}

func (d *DB) SetNodeFile(ctx context.Context, id, groupID int64, season, episode, title, plot, still string) error {
	var group any
	if groupID > 0 {
		group = groupID
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET group_node_id=?, season_num=NULLIF(?,''), episode_num=NULLIF(?,''), file_title=NULLIF(?,''), file_plot=NULLIF(?,''), still_path=NULLIF(?,'') WHERE id=?`,
		group, season, episode, title, plot, still, id)
	return err
}

func (d *DB) CountLibraryNodes(ctx context.Context, libraryID int64) (int, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_nodes WHERE library_id=? AND rel_path<>''`, libraryID).Scan(&n)
	return n, err
}

func likePrefix(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (d *DB) ListNodesUnder(ctx context.Context, libraryID int64, rel string) ([]LibraryNode, error) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	q := `SELECT id, library_id, parent_id, rel_path, name, node_kind,
		mm_kind, mm_title, mm_year, poster_path, match_status, meta_provider, meta_id, plot,
		group_node_id, season_num, episode_num, file_title, file_plot, still_path, date_added
		FROM library_nodes WHERE library_id=? AND rel_path<>''`
	args := []any{libraryID}
	if rel != "" {
		q += ` AND (rel_path=? OR rel_path LIKE ? ESCAPE '\' )`
		args = append(args, rel, likePrefix(rel)+`/%`)
	}
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryNode
	for rows.Next() {
		var n LibraryNode
		if err := rows.Scan(&n.ID, &n.LibraryID, &n.ParentID, &n.RelPath, &n.Name, &n.NodeKind,
			&n.MMKind, &n.MMTitle, &n.MMYear, &n.PosterPath, &n.MatchStatus, &n.MetaProvider, &n.MetaID, &n.Plot,
			&n.GroupNodeID, &n.SeasonNum, &n.EpisodeNum, &n.FileTitle, &n.FilePlot, &n.StillPath, &n.DateAdded); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

const nodeDisplayTitleSQL = `COALESCE(NULLIF(file_title,''), NULLIF(mm_title,''), name) COLLATE NOCASE`

func childNodesOrder(sort string) string {
	switch strings.TrimSpace(sort) {
	case "date_added", "added":
		// Full ingest timestamp (RFC3339); datetime() normalizes for ordering.
		return `node_kind, datetime(date_added) DESC, ` + nodeDisplayTitleSQL
	case "year", "released":
		// Full premiere/release date from NFO; year-only titles use YYYY-01-01.
		return `node_kind, COALESCE(NULLIF(mm_premiered,''), NULLIF(mm_year,'') || '-01-01') DESC, ` + nodeDisplayTitleSQL
	default:
		return `node_kind, ` + nodeDisplayTitleSQL
	}
}

// NodeTitleFacts are NFO-backed fields cached on library_nodes for listing/sort.
type NodeTitleFacts struct {
	Title     string
	Year      string
	Premiered string
	Plot      string
	Rating    float64
}

// ApplyNodeTitleFacts updates non-empty NFO facts onto a directory node.
func (d *DB) ApplyNodeTitleFacts(ctx context.Context, id int64, f NodeTitleFacts) error {
	if id <= 0 {
		return nil
	}
	_, err := d.SQL.ExecContext(ctx, `
		UPDATE library_nodes SET
			mm_title=CASE WHEN ?!='' THEN ? ELSE mm_title END,
			mm_year=CASE WHEN ?!='' THEN ? ELSE mm_year END,
			mm_premiered=CASE WHEN ?!='' THEN ? ELSE mm_premiered END,
			plot=CASE WHEN ?!='' THEN ? ELSE plot END,
			rating=CASE WHEN ?>0 THEN ? ELSE rating END
		WHERE id=?`,
		f.Title, f.Title,
		f.Year, f.Year,
		f.Premiered, f.Premiered,
		f.Plot, f.Plot,
		f.Rating, f.Rating,
		id)
	return err
}

func (d *DB) ListChildNodes(ctx context.Context, libraryID int64, rel, sort string) ([]LibraryNode, error) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	var parentID int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM library_nodes WHERE library_id=? AND rel_path=?`, libraryID, rel).Scan(&parentID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, library_id, parent_id, rel_path, name, node_kind,
		mm_kind, mm_title, mm_year, poster_path, match_status, meta_provider, meta_id, plot,
		group_node_id, season_num, episode_num, file_title, file_plot, still_path, date_added
		FROM library_nodes WHERE library_id=? AND parent_id=? ORDER BY `+childNodesOrder(sort), libraryID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryNode
	for rows.Next() {
		var n LibraryNode
		if err := rows.Scan(&n.ID, &n.LibraryID, &n.ParentID, &n.RelPath, &n.Name, &n.NodeKind,
			&n.MMKind, &n.MMTitle, &n.MMYear, &n.PosterPath, &n.MatchStatus, &n.MetaProvider, &n.MetaID, &n.Plot,
			&n.GroupNodeID, &n.SeasonNum, &n.EpisodeNum, &n.FileTitle, &n.FilePlot, &n.StillPath, &n.DateAdded); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *DB) ListFileWatch(ctx context.Context, userID, libraryID int64) ([]FileWatch, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT f.id, f.rel_path,
		COALESCE(wp.position_seconds, 0), wp.duration_seconds, COALESCE(wp.completed, 0)
		FROM library_nodes f
		LEFT JOIN watch_progress wp ON wp.node_id = f.id AND wp.user_id = ?
		WHERE f.library_id = ? AND f.node_kind = 'file'`, userID, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileWatch
	for rows.Next() {
		var w FileWatch
		var pos float64
		var dur sql.NullFloat64
		var completed int
		if err := rows.Scan(&w.ID, &w.Rel, &pos, &dur, &completed); err != nil {
			return nil, err
		}
		w.Rel = strings.Trim(filepath.ToSlash(w.Rel), "/")
		w.Done = completed == 1
		w.Pct = progressPctFromRow(pos, dur, completed)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) NodeIDByRel(ctx context.Context, libraryID int64, rel string) (int64, error) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM library_nodes WHERE library_id=? AND rel_path=?`, libraryID, rel).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (d *DB) SetMediaItemNode(ctx context.Context, itemID, nodeID int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET node_id=? WHERE id=?`, nodeID, itemID)
	return err
}

func (d *DB) MediaItemNodeID(ctx context.Context, itemID int64) (int64, error) {
	var id sql.NullInt64
	err := d.SQL.QueryRowContext(ctx, `SELECT node_id FROM media_items WHERE id=?`, itemID).Scan(&id)
	if err == sql.ErrNoRows || !id.Valid {
		return 0, nil
	}
	return id.Int64, err
}

func (d *DB) SeasonDirRel(ctx context.Context, groupNodeID int64, season int) (string, bool) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT parent.rel_path, f.season_num
		FROM library_nodes f
		JOIN library_nodes parent ON parent.id = f.parent_id
		WHERE f.group_node_id=? AND f.node_kind='file'`, groupNodeID)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	want := strconv.Itoa(season)
	var rel string
	found := false
	for rows.Next() {
		var parentRel, num string
		if err := rows.Scan(&parentRel, &num); err != nil {
			return "", false
		}
		if strings.TrimLeft(num, "0") != strings.TrimLeft(want, "0") && num != want {
			continue
		}
		if !found {
			rel = parentRel
			found = true
			continue
		}
		if rel != parentRel {
			return "", false
		}
	}
	return rel, found && rows.Err() == nil
}

func (d *DB) ListGoneLibraryNodeIDs(ctx context.Context, libraryID int64, root string) ([]int64, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, rel_path FROM library_nodes WHERE library_id=? AND rel_path<>'' ORDER BY length(rel_path) DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var rel string
		if err := rows.Scan(&id, &rel); err != nil {
			return nil, err
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (d *DB) DeleteLibraryNodes(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := d.SQL.ExecContext(ctx, `DELETE FROM library_nodes WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) DeleteGoneLibraryNodes(ctx context.Context, libraryID int64, root string) error {
	ids, err := d.ListGoneLibraryNodeIDs(ctx, libraryID, root)
	if err != nil {
		return err
	}
	return d.DeleteLibraryNodes(ctx, ids)
}

func (d *DB) GetLibraryNode(ctx context.Context, id int64) (*LibraryNode, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, library_id, parent_id, rel_path, name, node_kind,
		mm_kind, mm_title, mm_year, poster_path, match_status, meta_provider, meta_id, plot,
		group_node_id, season_num, episode_num, file_title, file_plot, still_path, date_added
		FROM library_nodes WHERE id=?`, id)
	var n LibraryNode
	if err := row.Scan(&n.ID, &n.LibraryID, &n.ParentID, &n.RelPath, &n.Name, &n.NodeKind,
		&n.MMKind, &n.MMTitle, &n.MMYear, &n.PosterPath, &n.MatchStatus, &n.MetaProvider, &n.MetaID, &n.Plot,
		&n.GroupNodeID, &n.SeasonNum, &n.EpisodeNum, &n.FileTitle, &n.FilePlot, &n.StillPath, &n.DateAdded); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

func (d *DB) AdjacentFileNodes(ctx context.Context, id int64) (prev, next *LibraryNode, err error) {
	n, err := d.GetLibraryNode(ctx, id)
	if err != nil || n == nil || !n.ParentID.Valid {
		return nil, nil, err
	}
	kids, err := d.SQL.QueryContext(ctx, `SELECT id FROM library_nodes WHERE library_id=? AND parent_id=? AND node_kind='file' ORDER BY rel_path COLLATE NOCASE`, n.LibraryID, n.ParentID.Int64)
	if err != nil {
		return nil, nil, err
	}
	defer kids.Close()
	var ids []int64
	for kids.Next() {
		var kid int64
		if err := kids.Scan(&kid); err != nil {
			return nil, nil, err
		}
		ids = append(ids, kid)
	}
	if err := kids.Err(); err != nil {
		return nil, nil, err
	}
	for i, kid := range ids {
		if kid != id {
			continue
		}
		if i > 0 {
			prev, _ = d.GetLibraryNode(ctx, ids[i-1])
		}
		if i+1 < len(ids) {
			next, _ = d.GetLibraryNode(ctx, ids[i+1])
		}
		break
	}
	return prev, next, nil
}

func (d *DB) BackfillLibraryNodes(ctx context.Context) error {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, path FROM libraries`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type lib struct {
		id   int64
		path string
	}
	var libs []lib
	for rows.Next() {
		var l lib
		if err := rows.Scan(&l.id, &l.path); err != nil {
			return err
		}
		libs = append(libs, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range libs {
		if _, err := d.EnsureLibraryRoot(ctx, l.id); err != nil {
			return err
		}
		paths, err := d.SQL.QueryContext(ctx, `SELECT path FROM media_items WHERE library_id=? UNION SELECT path FROM episodes WHERE show_id IN (SELECT id FROM media_items WHERE library_id=?)`, l.id, l.id)
		if err != nil {
			return err
		}
		var abs []string
		for paths.Next() {
			var p string
			if err := paths.Scan(&p); err != nil {
				paths.Close()
				return err
			}
			abs = append(abs, p)
		}
		paths.Close()
		for _, p := range abs {
			rel, err := filepath.Rel(l.path, p)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == ".." || strings.HasPrefix(rel, "../") || rel == "." {
				continue
			}
			if _, err := d.UpsertNodeChain(ctx, l.id, rel, "file"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) ItemByNode(ctx context.Context, nodeID int64) (id int64, kind, status, matchErr string, err error) {
	err = d.SQL.QueryRowContext(ctx, `
		SELECT id, kind, COALESCE(match_status,''), COALESCE(match_error,'')
		FROM media_items WHERE node_id=?`, nodeID).Scan(&id, &kind, &status, &matchErr)
	if err == sql.ErrNoRows {
		return 0, "", "", "", nil
	}
	return
}

func (d *DB) EpisodeIDByPath(ctx context.Context, path string) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT id FROM episodes WHERE path=?`, path).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (d *DB) LibraryPath(ctx context.Context, id int64) (string, error) {
	var p string
	err := d.SQL.QueryRowContext(ctx, `SELECT path FROM libraries WHERE id=?`, id).Scan(&p)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return p, err
}

func (d *DB) SeasonEdit(ctx context.Context, dirID int64) (showID int64, season int, ok bool) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT m.id, f.season_num
		FROM library_nodes f
		JOIN media_items m ON m.node_id = f.group_node_id AND m.kind='show'
		WHERE f.parent_id=? AND f.node_kind='file' AND COALESCE(f.season_num,'') != ''`, dirID)
	if err != nil {
		return 0, 0, false
	}
	defer rows.Close()
	var show int64
	num := -1
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return 0, 0, false
		}
		n, convErr := seasonNum(raw)
		if convErr != nil {
			return 0, 0, false
		}
		if show == 0 {
			show, num = id, n
			continue
		}
		if id != show || n != num {
			return 0, 0, false
		}
	}
	return show, num, show != 0 && num >= 0 && rows.Err() == nil
}

func seasonNum(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, strconv.ErrSyntax
	}
	trimmed := strings.TrimLeft(raw, "0")
	if trimmed == "" {
		return 0, nil
	}
	return strconv.Atoi(trimmed)
}

func (d *DB) SetNodeMatchStatus(ctx context.Context, id int64, status, matchErr string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET match_status=NULLIF(?,''), match_error=NULLIF(?,''), match_candidates=NULL WHERE id=?`, status, matchErr, id)
	return err
}

func (d *DB) SetNodeDisplay(ctx context.Context, id int64, file bool, title, plot *string) error {
	if file {
		if title != nil {
			if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET file_title=? WHERE id=?`, strings.TrimSpace(*title), id); err != nil {
				return err
			}
		}
		if plot != nil {
			if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET file_plot=? WHERE id=?`, *plot, id); err != nil {
				return err
			}
		}
		return nil
	}
	if title != nil {
		if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET mm_title=? WHERE id=?`, strings.TrimSpace(*title), id); err != nil {
			return err
		}
	}
	if plot != nil {
		if _, err := d.SQL.ExecContext(ctx, `UPDATE library_nodes SET plot=? WHERE id=?`, *plot, id); err != nil {
			return err
		}
	}
	return nil
}
