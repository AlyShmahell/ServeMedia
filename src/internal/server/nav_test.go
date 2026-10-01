package server

import (
	"database/sql"
	"testing"

	"github.com/alyshmahell/servemedia/internal/db"
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

	show := map[string]any{"Item": &db.MediaItem{Title: "KonoSuba", LibraryID: 4}}
	applyNav("show.html", show)
	if show["NavTitle"] != "KonoSuba" || show["NavBack"] != "/libraries/4" {
		t.Fatalf("show %#v", show)
	}

	season := map[string]any{
		"Item":   db.MediaItem{ID: 9},
		"Season": &db.Season{SeasonNumber: 2, Title: sql.NullString{String: "S2", Valid: true}},
	}
	applyNav("season.html", season)
	if season["NavTitle"] != "S2" || season["NavBack"] != "/shows/9" {
		t.Fatalf("season %#v", season)
	}

	play := map[string]any{"Kind": "episode", "Title": "Pilot", "ShowID": int64(3)}
	applyNav("player.html", play)
	if play["NavTitle"] != "Pilot" || play["NavBack"] != "/shows/3" {
		t.Fatalf("player %#v", play)
	}

	preset := map[string]any{"NavTitle": "Keep", "NavBack": "/custom"}
	applyNav("about.html", preset)
	if preset["NavTitle"] != "Keep" || preset["NavBack"] != "/custom" {
		t.Fatalf("preset %#v", preset)
	}
}
