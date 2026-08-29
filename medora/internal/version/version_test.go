package version_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/version"
)

func TestInitReadsVersionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("0.0.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	version.Init(dir)
	if version.Version != "0.0.2" {
		t.Fatalf("Version = %q, want 0.0.2", version.Version)
	}
}

func TestInitMissingFile(t *testing.T) {
	version.Init(t.TempDir())
	if version.Version != "0.0.1" {
		t.Fatalf("Version = %q, want 0.0.1 fallback", version.Version)
	}
}
