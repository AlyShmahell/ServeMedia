package version_test

import (
	"testing"

	"github.com/alyshmahell/medora/internal/version"
)

func TestInit(t *testing.T) {
	version.Init("0.0.2\n")
	if version.Version != "0.0.2" {
		t.Fatalf("Version = %q, want 0.0.2", version.Version)
	}
	version.Init("  1.2.3  ")
	if version.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", version.Version)
	}
	version.Init("")
	if version.Version != "" {
		t.Fatalf("Version = %q, want empty", version.Version)
	}
}
