package fetch

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

// resolveManualFromChildren marks a manual directory matched when one of its
// candidates is already the identity of a subdirectory. The directory poster
// is the poster of the last such subdirectory. Episodes are not ingested.
func (w *Worker) resolveManualFromChildren(ctx context.Context, lib *db.Library) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	items, err := w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return err
	}
	order := make([]int, len(items))
	for i := range items {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return pathDepth(items[order[a]].Path) > pathDepth(items[order[b]].Path)
	})
	for _, idx := range order {
		if err := w.resolveOneManual(ctx, lib, items, idx); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) resolveOneManual(ctx context.Context, lib *db.Library, items []db.MediaItem, idx int) error {
	it := items[idx]
	if strings.TrimSpace(it.MatchStatus.String) != "manual" {
		return nil
	}
	full, err := w.DB.GetMediaItem(ctx, it.ID)
	if err != nil || full == nil {
		return err
	}
	snap := DecodePickerSnapshot(full.MatchCandidates.String)
	cands := snap.Candidates
	if len(cands) == 0 && snap.Match != nil {
		cands = []matchmedia.Candidate{*snap.Match}
	}
	var chosen *matchmedia.Candidate
	poster := ""
	for i := range cands {
		hits := matchesUnder(items, it.ID, it.Path, cands[i].Provider, cands[i].ID)
		if len(hits) == 0 {
			continue
		}
		sort.Slice(hits, func(a, b int) bool { return hits[a].Path < hits[b].Path })
		chosen = &cands[i]
		poster = ""
		last := hits[len(hits)-1]
		if last.PosterPath.Valid {
			poster = strings.TrimSpace(last.PosterPath.String)
		}
	}
	if chosen == nil {
		return nil
	}
	session, job := "", ""
	if full.MatchMediaSessionID.Valid {
		session = full.MatchMediaSessionID.String
	}
	if full.MatchMediaJobID.Valid {
		job = full.MatchMediaJobID.String
	}
	if err := w.DB.SetMatchMediaMatch(ctx, it.ID, session, job, "matched", ""); err != nil {
		return err
	}
	if err := w.DB.UpdateMediaItemMeta(ctx, it.ID, "", 0, "", poster, "", "", 0, chosen.Provider, chosen.ID); err != nil {
		return err
	}
	items[idx].MatchStatus = sql.NullString{String: "matched", Valid: true}
	items[idx].MetaProvider = sql.NullString{String: chosen.Provider, Valid: true}
	items[idx].MetaID = sql.NullString{String: chosen.ID, Valid: true}
	if poster != "" {
		items[idx].PosterPath = sql.NullString{String: poster, Valid: true}
	}
	rel, err := relWithin(lib.Path, it.Path)
	if err != nil || rel == "" {
		return nil
	}
	nodeID, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || nodeID == 0 {
		return err
	}
	return w.DB.SetNodeMatchIdentity(ctx, nodeID, "matched", chosen.Provider, chosen.ID, poster)
}

func matchesUnder(items []db.MediaItem, self int64, root, provider, id string) []db.MediaItem {
	root = filepath.Clean(root)
	var hits []db.MediaItem
	for _, it := range items {
		if it.ID == self {
			continue
		}
		path := filepath.Clean(it.Path)
		if path == root || !pathUnder(path, root) {
			continue
		}
		if !it.MetaProvider.Valid || !it.MetaID.Valid {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(it.MetaProvider.String), strings.TrimSpace(provider)) {
			continue
		}
		if strings.TrimSpace(it.MetaID.String) != strings.TrimSpace(id) {
			continue
		}
		hits = append(hits, it)
	}
	return hits
}

func pathDepth(p string) int {
	p = filepath.Clean(p)
	if p == "" || p == "." || p == string(filepath.Separator) {
		return 0
	}
	return strings.Count(p, string(filepath.Separator))
}
