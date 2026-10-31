package fetch

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func syncNodeFromTitleNFO(ctx context.Context, d *db.DB, nodeID int64, dir string) error {
	if d == nil || nodeID <= 0 || strings.TrimSpace(dir) == "" {
		return nil
	}
	facts, ok := metadata.TitleNFOFactsFromDir(dir)
	if !ok {
		return nil
	}
	return d.ApplyNodeTitleFacts(ctx, nodeID, db.NodeTitleFacts{
		Title: facts.Title, Year: facts.Year, Premiered: facts.Premiered,
		Plot: facts.Plot, Rating: facts.Rating,
	})
}

func (w *Worker) syncLinkedNodeFromMovieNFO(ctx context.Context, it *db.MediaItem, nfo metadata.MovieNFO) error {
	return w.syncLinkedNodeFacts(ctx, it, metadata.TitleNFOFactsFromMovie(nfo))
}

func (w *Worker) syncLinkedNodeFromShowNFO(ctx context.Context, it *db.MediaItem, nfo metadata.TVShowNFO) error {
	return w.syncLinkedNodeFacts(ctx, it, metadata.TitleNFOFactsFromShow(nfo))
}

func (w *Worker) syncLinkedNodeFacts(ctx context.Context, it *db.MediaItem, facts metadata.TitleNFOFacts) error {
	if w == nil || w.DB == nil || it == nil {
		return nil
	}
	nodeID, err := w.DB.MediaItemNodeID(ctx, it.ID)
	if err != nil {
		return err
	}
	if nodeID <= 0 && strings.TrimSpace(it.Path) != "" {
		libPath, pathErr := w.DB.LibraryPath(ctx, it.LibraryID)
		if pathErr == nil && libPath != "" {
			rel, relErr := relWithin(libPath, it.Path)
			if relErr != nil {
				rel, relErr = relWithin(libPath, filepath.Dir(it.Path))
			}
			if relErr == nil {
				nodeID, _ = w.DB.NodeIDByRel(ctx, it.LibraryID, rel)
			}
		}
	}
	if nodeID <= 0 {
		return nil
	}
	return w.DB.ApplyNodeTitleFacts(ctx, nodeID, db.NodeTitleFacts{
		Title: facts.Title, Year: facts.Year, Premiered: facts.Premiered,
		Plot: facts.Plot, Rating: facts.Rating,
	})
}
