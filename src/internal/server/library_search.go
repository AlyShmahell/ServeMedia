package server

import (
	"context"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func nodeMatches(n db.LibraryNode, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	for _, s := range []string{n.Name, n.MMTitle.String, n.FileTitle.String} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

func containerRel(parent, match string) string {
	parent = strings.Trim(strings.ReplaceAll(parent, "\\", "/"), "/")
	match = strings.Trim(strings.ReplaceAll(match, "\\", "/"), "/")
	if match == "" || match == parent {
		return ""
	}
	rest := match
	if parent != "" {
		prefix := parent + "/"
		if !strings.HasPrefix(match, prefix) {
			return ""
		}
		rest = match[len(prefix):]
	}
	if rest == "" {
		return ""
	}
	i := strings.Index(rest, "/")
	if i < 0 {
		if parent == "" {
			return rest
		}
		return parent + "/" + rest
	}
	child := rest[:i]
	if parent == "" {
		return child
	}
	return parent + "/" + child
}

func matchingContainers(parent string, nodes []db.LibraryNode, q string) map[string]bool {
	keep := map[string]bool{}
	for _, n := range nodes {
		if !nodeMatches(n, q) {
			continue
		}
		if c := containerRel(parent, n.RelPath); c != "" {
			keep[c] = true
		}
	}
	return keep
}

func (s *Server) nodeArt(ctx context.Context, node db.LibraryNode, mediaID int64) string {
	if node.NodeKind == "file" {
		if p := strings.TrimSpace(node.StillPath.String); p != "" {
			return p
		}
	}
	if p := strings.TrimSpace(node.PosterPath.String); p != "" {
		return p
	}
	if mediaID <= 0 || s == nil || s.DB == nil {
		return ""
	}
	it, err := s.DB.GetMediaItem(ctx, mediaID)
	if err != nil || it == nil || !it.PosterPath.Valid {
		return ""
	}
	return strings.TrimSpace(it.PosterPath.String)
}
