package server

import (
	"fmt"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func applyNav(name string, data map[string]any) {
	title, back := navFor(name, data)
	if _, ok := data["NavTitle"]; !ok && title != "" {
		data["NavTitle"] = title
	}
	if _, ok := data["NavBack"]; !ok && back != "" {
		data["NavBack"] = back
	}
}

func navFor(name string, data map[string]any) (title, back string) {
	switch name {
	case "home.html":
		return "Home", ""
	case "library.html":
		return libraryName(data["Library"]), libraryBack(data)
	case "player.html":
		title, _ = data["Title"].(string)
		kind, _ := data["Kind"].(string)
		if kind == "file" {
			back = libraryFolderBack(int64FromData(data["LibraryID"]), stringFromData(data["Folder"]))
		}
		return title, back
	}
	return "", ""
}

func libraryBack(data map[string]any) string {
	id := libraryID(data["Library"])
	path, _ := data["Path"].(string)
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	if id == 0 || path == "" {
		return "/"
	}
	return libraryFolderBack(id, parentRel(path))
}

func libraryFolderBack(id int64, folder string) string {
	if id <= 0 {
		return ""
	}
	folder = strings.Trim(strings.ReplaceAll(folder, "\\", "/"), "/")
	if folder == "" {
		return fmt.Sprintf("/libraries/%d", id)
	}
	return fmt.Sprintf("/libraries/%d/tree?path=%s", id, urlQueryEscape(folder))
}

func libraryID(v any) int64 {
	switch lib := v.(type) {
	case *db.Library:
		if lib != nil {
			return lib.ID
		}
	case db.Library:
		return lib.ID
	}
	return 0
}

func stringFromData(v any) string {
	s, _ := v.(string)
	return s
}

func libraryName(v any) string {
	switch lib := v.(type) {
	case *db.Library:
		if lib != nil {
			return lib.Name
		}
	case db.Library:
		return lib.Name
	}
	return ""
}

func int64FromData(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
