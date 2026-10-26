package server

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/web"
	"github.com/go-chi/chi/v5"
)

func TestContainerRel(t *testing.T) {
	cases := []struct {
		parent, match, want string
	}{
		{"", "Sword Art Online", "Sword Art Online"},
		{"", "Sword Art Online/Ordinal Scale.mkv", "Sword Art Online"},
		{"", "Sword Art Online/Season 1/E01.mkv", "Sword Art Online"},
		{"Sword Art Online", "Sword Art Online/Ordinal Scale.mkv", "Sword Art Online/Ordinal Scale.mkv"},
		{"Sword Art Online", "Sword Art Online/Season 1/E01.mkv", "Sword Art Online/Season 1"},
		{"Sword Art Online", "Sword Art Online", ""},
		{"", "", ""},
		{"Show", "Other/File.mkv", ""},
	}
	for _, tc := range cases {
		if got := containerRel(tc.parent, tc.match); got != tc.want {
			t.Fatalf("containerRel(%q,%q)=%q want %q", tc.parent, tc.match, got, tc.want)
		}
	}
}

func TestLibrarySearchShowsContainers(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "/media")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureLibraryRoot(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	sao, err := d.UpsertNodeChain(ctx, lib.ID, "Sword Art Online", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, sao, db.NodeGroup{Kind: "show", Title: "Sword Art Online"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Sword Art Online/Season 1", "dir"); err != nil {
		t.Fatal(err)
	}
	ep, err := d.UpsertNodeChain(ctx, lib.ID, "Sword Art Online/Season 1/E01.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, ep, sao, "1", "1", "Pilot", "", ""); err != nil {
		t.Fatal(err)
	}
	movie, err := d.UpsertNodeChain(ctx, lib.ID, "Sword Art Online/Ordinal Scale.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, movie, 0, "", "", "Sword Art Online The Movie - Ordinal Scale", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertNodeChain(ctx, lib.ID, "Other Show", "dir"); err != nil {
		t.Fatal(err)
	}

	s := &Server{Cfg: &config.Config{}, DB: d}
	root := s.libraryPageData(ctx, u, lib, "name", "Ordinal", "", false)
	if root["Tree"] != true {
		t.Fatalf("tree %#v", root["Tree"])
	}
	if _, ok := root["Items"]; ok {
		t.Fatal("media item grid")
	}
	dirs, _ := root["Nodes"].([]nodeCard)
	files, _ := root["Files"].([]nodeCard)
	if len(files) != 0 {
		t.Fatalf("nested files at root %#v", files)
	}
	if len(dirs) != 1 || dirs[0].Rel != "Sword Art Online" {
		t.Fatalf("root containers %#v", dirs)
	}
	if root["ItemCount"] != 1 {
		t.Fatalf("count %#v", root["ItemCount"])
	}

	swo := s.libraryPageData(ctx, u, lib, "name", "SWO", "", false)
	swoDirs, _ := swo["Nodes"].([]nodeCard)
	swoFiles, _ := swo["Files"].([]nodeCard)
	if len(swoFiles) != 0 {
		t.Fatalf("swo files %#v", swoFiles)
	}
	if len(swoDirs) != 1 || swoDirs[0].Rel != "Sword Art Online" {
		t.Fatalf("swo dirs %#v", swoDirs)
	}

	inner := s.libraryPageData(ctx, u, lib, "name", "Ordinal", "Sword Art Online", false)
	if inner["Tree"] != true || inner["Path"] != "Sword Art Online" {
		t.Fatalf("inner %#v %#v", inner["Tree"], inner["Path"])
	}
	innerDirs, _ := inner["Nodes"].([]nodeCard)
	innerFiles, _ := inner["Files"].([]nodeCard)
	if len(innerDirs) != 0 {
		t.Fatalf("inner dirs %#v", innerDirs)
	}
	if len(innerFiles) != 1 || !strings.Contains(innerFiles[0].Title, "Ordinal") {
		t.Fatalf("inner files %#v", innerFiles)
	}

	seasonHit := s.libraryPageData(ctx, u, lib, "name", "Pilot", "Sword Art Online", false)
	seasonDirs, _ := seasonHit["Nodes"].([]nodeCard)
	if len(seasonDirs) != 1 || seasonDirs[0].Rel != "Sword Art Online/Season 1" {
		t.Fatalf("season container %#v", seasonDirs)
	}
}

func TestLibraryTreeUsesItemPoster(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/media")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureLibraryRoot(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	dirID, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Film.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/media/Show", Mtime: 1,
		PosterPath: sql.NullString{String: "metadata/tv/Show/poster.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	filmID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/media/Show/Film.mkv", Mtime: 1,
		PosterPath: sql.NullString{String: "metadata/movies/Film/poster.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, showID, dirID); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, filmID, fileID); err != nil {
		t.Fatal(err)
	}

	s := &Server{Cfg: &config.Config{}, DB: d}
	root := s.libraryPageData(ctx, u, lib, "name", "", "", false)
	dirs, _ := root["Nodes"].([]nodeCard)
	if len(dirs) != 1 || dirs[0].Node.PosterPath.String != "metadata/tv/Show/poster.jpg" {
		t.Fatalf("dir poster %#v", dirs)
	}

	nested := s.libraryPageData(ctx, u, lib, "name", "", "Show", false)
	files, _ := nested["Files"].([]nodeCard)
	if len(files) != 1 || files[0].Still != "metadata/movies/Film/poster.jpg" {
		t.Fatalf("file poster %#v", files)
	}
	hero, ok := nested["FolderHero"].(nodeCard)
	if !ok || hero.Title != "Show" || hero.Node.PosterPath.String != "metadata/tv/Show/poster.jpg" {
		t.Fatalf("folder hero %#v", nested["FolderHero"])
	}
	if _, ok := root["FolderHero"]; ok {
		t.Fatalf("root hero %#v", root["FolderHero"])
	}

	search := s.libraryPageData(ctx, u, lib, "name", "Film", "Show", false)
	searchHero, ok := search["FolderHero"].(nodeCard)
	if !ok || searchHero.Title != "Show" {
		t.Fatalf("search hero %#v", search["FolderHero"])
	}

	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tpl := MustParseTemplates(tplFS)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "partials/items.html", nested); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, "metadata/movies/Film/poster.jpg") {
		t.Fatalf("nested html %s", html)
	}
	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/items_live.html", nested); err != nil {
		t.Fatal(err)
	}
	live := buf.String()
	heroAt := strings.Index(live, "show-hero-card")
	fileAt := strings.Index(live, "file-card")
	if heroAt < 0 || fileAt < 0 || heroAt > fileAt {
		t.Fatalf("nested folder hero %s", live)
	}
	if !strings.Contains(live, "metadata/tv/Show/poster.jpg") {
		t.Fatalf("hero poster %s", live)
	}
	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/items.html", root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "metadata/tv/Show/poster.jpg") {
		t.Fatalf("root html %s", buf.String())
	}
	buf.Reset()
	if err := tpl.ExecuteTemplate(&buf, "partials/items_live.html", root); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "show-hero-card") {
		t.Fatalf("root must not have folder hero %s", buf.String())
	}

	s.Templates = tpl
	rtr := chi.NewRouter()
	rtr.Get("/libraries/{id}/tree", s.handleLibrary)
	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(context.WithValue(req.Context(), userKey, u))
		w := httptest.NewRecorder()
		rtr.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status %d body %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	nestedPage := get(fmt.Sprintf("/libraries/%d/tree?path=Show", lib.ID))
	if strings.Index(nestedPage, "show-hero-card") < 0 || strings.Index(nestedPage, "file-card") < 0 {
		t.Fatalf("tree page hero %s", nestedPage)
	}
	if strings.Index(nestedPage, "show-hero-card") > strings.Index(nestedPage, "file-card") {
		t.Fatalf("hero should sit above files %s", nestedPage)
	}
	rootPage := get(fmt.Sprintf("/libraries/%d/tree", lib.ID))
	if strings.Contains(rootPage, "show-hero-card") {
		t.Fatalf("root page hero %s", rootPage)
	}
}
