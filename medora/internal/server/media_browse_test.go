package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJailMediaPathSingleRoot(t *testing.T) {
	root := "/media"
	got, err := jailMediaPath([]string{root}, "")
	if err != nil || got != root {
		t.Fatalf("empty: %q %v", got, err)
	}
	got, err = jailMediaPath([]string{root}, "Movies")
	if err != nil || got != "/media/Movies" {
		t.Fatalf("rel: %q %v", got, err)
	}
	got, err = jailMediaPath([]string{root}, "/media/TV")
	if err != nil || got != "/media/TV" {
		t.Fatalf("abs: %q %v", got, err)
	}
	if _, err := jailMediaPath([]string{root}, "/etc"); err == nil {
		t.Fatal("expected outside")
	}
}

func TestJailMediaPathMultiRoot(t *testing.T) {
	roots := []string{"/mnt/Movies", "/mnt/TV"}
	got, err := jailMediaPath(roots, "/mnt/TV/Show")
	if err != nil || got != "/mnt/TV/Show" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := jailMediaPath(roots, "/mnt/Movies"); err != nil {
		t.Fatal(err)
	}
	if _, err := jailMediaPath(roots, "/etc/passwd"); err == nil {
		t.Fatal("expected outside")
	}
	if _, err := jailMediaPath(roots, ""); err == nil {
		t.Fatal("empty multi-root should fail jail")
	}
	if _, err := jailMediaPath(roots, "rel"); err == nil {
		t.Fatal("relative multi-root should fail jail")
	}
}

func TestListMediaDirsSingleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "TV"), 0o755); err != nil {
		t.Fatal(err)
	}
	browse, err := listMediaDirs([]string{root}, "")
	if err != nil {
		t.Fatal(err)
	}
	if browse.Path != filepath.Clean(root) {
		t.Fatalf("path %q", browse.Path)
	}
	if browse.CanGoUp {
		t.Fatal("should not go up from single root")
	}
	if len(browse.Dirs) != 2 || browse.Dirs[0].Name != "Movies" || browse.Dirs[1].Name != "TV" {
		t.Fatalf("dirs %#v", browse.Dirs)
	}
	if browse.Dirs[0].Path != filepath.Join(root, "Movies") {
		t.Fatalf("child path %q", browse.Dirs[0].Path)
	}
	inner, err := listMediaDirs([]string{root}, filepath.Join(root, "Movies"))
	if err != nil {
		t.Fatal(err)
	}
	if !inner.CanGoUp || inner.Parent != filepath.Clean(root) {
		t.Fatalf("up: can=%v parent=%q", inner.CanGoUp, inner.Parent)
	}
}

func TestListMediaDirsMultiRoot(t *testing.T) {
	base := t.TempDir()
	movies := filepath.Join(base, "Movies")
	tv := filepath.Join(base, "TV")
	if err := os.Mkdir(movies, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(movies, "Inception"), 0o755); err != nil {
		t.Fatal(err)
	}
	roots := []string{movies, tv}
	browse, err := listMediaDirs(roots, "")
	if err != nil {
		t.Fatal(err)
	}
	if browse.Path != "" || browse.CanGoUp {
		t.Fatalf("virtual: path=%q canUp=%v", browse.Path, browse.CanGoUp)
	}
	if len(browse.Dirs) != 2 || browse.Dirs[0].Name != "Movies" || browse.Dirs[1].Name != "TV" {
		t.Fatalf("dirs %#v", browse.Dirs)
	}
	if browse.Dirs[0].Path != movies || browse.Dirs[1].Path != tv {
		t.Fatalf("root paths %#v", browse.Dirs)
	}

	inner, err := listMediaDirs(roots, movies)
	if err != nil {
		t.Fatal(err)
	}
	if inner.Path != movies || !inner.CanGoUp || inner.Parent != "" {
		t.Fatalf("inside root: %#v", inner)
	}
	if len(inner.Dirs) != 1 || inner.Dirs[0].Name != "Inception" {
		t.Fatalf("children %#v", inner.Dirs)
	}
}

func TestListVirtualMediaRootsDuplicateBasename(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "disk1", "Media")
	b := filepath.Join(base, "disk2", "Media")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	browse := listVirtualMediaRoots([]string{a, b})
	if len(browse.Dirs) != 2 {
		t.Fatalf("dirs %#v", browse.Dirs)
	}
	if browse.Dirs[0].Name == "Media" || browse.Dirs[1].Name == "Media" {
		t.Fatalf("expected full-path labels, got %#v", browse.Dirs)
	}
}
