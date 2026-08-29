package config

import "testing"

func TestResolveMediaPaths(t *testing.T) {
	c := Defaults()
	c.ExeDir = "/opt/medora"
	c.Media.Path = "rel, /abs/tv, "
	c.resolvePaths()
	if c.Media.Path != "/opt/medora/rel,/abs/tv" {
		t.Fatalf("got %q", c.Media.Path)
	}
}
