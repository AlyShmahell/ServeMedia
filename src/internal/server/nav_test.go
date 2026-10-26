package server

import (
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestApplyNav(t *testing.T) {
	home := map[string]any{}
	applyNav("home.html", home)
	if home["NavTitle"] != "Home" {
		t.Fatalf("home title %#v", home["NavTitle"])
	}
	if _, ok := home["NavBack"]; ok {
		t.Fatal("home should not have back")
	}

	lib := map[string]any{"Library": &db.Library{Name: "TV"}}
	applyNav("library.html", lib)
	if lib["NavTitle"] != "TV" || lib["NavBack"] != "/" {
		t.Fatalf("library %#v", lib)
	}
	nested := map[string]any{"Library": &db.Library{ID: 4, Name: "TV"}, "Path": "Cool Show/Season 1"}
	applyNav("library.html", nested)
	if nested["NavBack"] != "/libraries/4/tree?path=Cool%20Show" {
		t.Fatalf("nested %#v", nested)
	}
	rootChild := map[string]any{"Library": &db.Library{ID: 4, Name: "TV"}, "Path": "Cool Show"}
	applyNav("library.html", rootChild)
	if rootChild["NavBack"] != "/libraries/4" {
		t.Fatalf("root child %#v", rootChild)
	}
	playFile := map[string]any{"Kind": "file", "Title": "Pilot", "LibraryID": int64(4), "Folder": "Cool Show"}
	applyNav("player.html", playFile)
	if playFile["NavBack"] != "/libraries/4/tree?path=Cool%20Show" {
		t.Fatalf("file player %#v", playFile)
	}

	play := map[string]any{"Kind": "episode", "Title": "Pilot", "NavBack": "/libraries/4/tree?path=Show"}
	applyNav("player.html", play)
	if play["NavTitle"] != "Pilot" || play["NavBack"] != "/libraries/4/tree?path=Show" {
		t.Fatalf("player %#v", play)
	}

	preset := map[string]any{"NavTitle": "Keep", "NavBack": "/custom"}
	applyNav("about.html", preset)
	if preset["NavTitle"] != "Keep" || preset["NavBack"] != "/custom" {
		t.Fatalf("preset %#v", preset)
	}
}
