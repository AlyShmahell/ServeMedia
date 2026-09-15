package server

import (
	"fmt"

	"github.com/alyshmahell/servemedia/internal/db"
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
	case "libraries.html":
		return "Libraries", "/"
	case "library.html":
		return libraryName(data["Library"]), "/"
	case "movie.html", "show.html":
		if it := mediaItemFromData(data["Item"]); it != nil {
			return it.Title, fmt.Sprintf("/libraries/%d", it.LibraryID)
		}
	case "season.html":
		back = ""
		if it := mediaItemFromData(data["Item"]); it != nil {
			back = fmt.Sprintf("/shows/%d", it.ID)
		}
		return seasonTitle(data["Season"]), back
	case "player.html":
		title, _ = data["Title"].(string)
		kind, _ := data["Kind"].(string)
		id := int64FromData(data["ID"])
		switch kind {
		case "movie":
			if id > 0 {
				back = fmt.Sprintf("/movies/%d", id)
			}
		case "episode":
			if showID := int64FromData(data["ShowID"]); showID > 0 {
				back = fmt.Sprintf("/shows/%d", showID)
			}
		}
		return title, back
	case "about.html":
		return "About", "/"
	case "settings.html":
		return "Settings", "/"
	case "settings_integrations.html":
		return "Integrations", "/settings"
	case "settings_server.html":
		return "Server", "/settings"
	case "settings_backup.html":
		return "Backup", "/settings"
	case "settings_users.html":
		return "Users", "/settings"
	}
	return "", ""
}

func mediaItemFromData(v any) *db.MediaItem {
	switch it := v.(type) {
	case *db.MediaItem:
		return it
	case db.MediaItem:
		return &it
	default:
		return nil
	}
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

func seasonTitle(v any) string {
	var se *db.Season
	switch s := v.(type) {
	case *db.Season:
		se = s
	case db.Season:
		se = &s
	}
	if se == nil {
		return ""
	}
	if se.Title.Valid && se.Title.String != "" {
		return se.Title.String
	}
	return fmt.Sprintf("Season %d", se.SeasonNumber)
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
