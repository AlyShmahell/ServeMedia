package cascade

import (
	"testing"

	"github.com/alyshmahell/medora-plugin-sdk/rpcapi"
)

func TestHasPosterAndStill(t *testing.T) {
	if hasPoster(nil) || hasStill(nil) {
		t.Fatal("nil should be empty")
	}
	if hasPoster(&rpcapi.Result{PosterURL: "  "}) {
		t.Fatal("whitespace poster")
	}
	if !hasPoster(&rpcapi.Result{PosterURL: "http://x/p.jpg"}) {
		t.Fatal("expected poster")
	}
	if !hasStill(&rpcapi.Result{StillURL: "http://x/s.jpg"}) {
		t.Fatal("expected still")
	}
}
