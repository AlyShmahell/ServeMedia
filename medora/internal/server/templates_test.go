package server

import (
	"io/fs"
	"testing"

	"github.com/alyshmahell/medora/web"
)

func TestMustParseTemplates(t *testing.T) {
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	for _, name := range []string{
		"home.html",
		"library.html",
		"movie.html",
		"show.html",
		"partials/match_dialog.html",
		"partials/scan_modal.html",
		"partials/card_actions.html",
		"partials/items.html",
	} {
		if tpl.Lookup(name) == nil {
			t.Fatalf("missing template %s", name)
		}
	}
}
