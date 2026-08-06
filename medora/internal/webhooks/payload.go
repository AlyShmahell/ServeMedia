package webhooks

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/version"
)

func BasePayload(cfg *config.Config, notificationType string) map[string]any {
	wh := cfg.Integrations.Webhooks
	serverURL := strings.TrimSpace(wh.ServerURL)
	if serverURL == "" {
		serverURL = "http://localhost:7676"
	}
	now := time.Now()
	return map[string]any{
		"ServerId":         wh.ServerID,
		"ServerName":       "Medora",
		"ServerVersion":    version.Version,
		"ServerUrl":        serverURL,
		"NotificationType": notificationType,
		"ClientName":       "Medora",
		"Client":           "Medora",
		"Timestamp":        now.Format(time.RFC3339),
		"UtcTimestamp":     now.UTC().Format(time.RFC3339),
	}
}

func MergePayload(base map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func PlaybackPayload(kind string, id int64, title string, username string, userID int64, position, duration float64, completed bool) map[string]any {
	itemType := "Video"
	switch kind {
	case "movie":
		itemType = "Movie"
	case "episode":
		itemType = "Episode"
	}
	extra := map[string]any{
		"Name":       title,
		"ItemId":     strconv.FormatInt(id, 10),
		"ItemType":   itemType,
		"Username":   username,
		"UserId":     strconv.FormatInt(userID, 10),
		"DeviceName": "Web",
		"DeviceId":   "web",
	}
	if duration > 0 {
		extra["RunTime"] = formatDuration(duration)
		extra["PlaybackPosition"] = formatDuration(position)
		extra["PlayedToCompletion"] = completed
	}
	return extra
}

func MediaItemPayload(item *db.MediaItem) map[string]any {
	if item == nil {
		return map[string]any{}
	}
	itemType := webhookItemType(item.Kind)
	extra := map[string]any{
		"Name":     item.Title,
		"ItemId":   strconv.FormatInt(item.ID, 10),
		"ItemType": itemType,
	}
	if item.Year.Valid && item.Year.Int64 > 0 {
		extra["Year"] = item.Year.Int64
	}
	if item.MetaID.Valid && item.MetaID.String != "" {
		switch item.MetaProvider.String {
		case "tmdb":
			extra["Provider_tmdb"] = item.MetaID.String
		case "imdb":
			extra["Provider_imdb"] = item.MetaID.String
		case "tvdb":
			extra["Provider_tvdb"] = item.MetaID.String
		}
	}
	return extra
}

func EpisodePayload(ep *db.Episode, show *db.MediaItem) map[string]any {
	if ep == nil {
		return map[string]any{}
	}
	title := ep.Title.String
	if title == "" {
		title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
	}
	extra := map[string]any{
		"Name":           title,
		"ItemId":         strconv.FormatInt(ep.ID, 10),
		"ItemType":       "Episode",
		"EpisodeNumber":  ep.EpisodeNumber,
		"EpisodeNumber00": fmt.Sprintf("%02d", ep.EpisodeNumber),
	}
	if show != nil {
		extra["SeriesName"] = show.Title
		extra["SeriesId"] = strconv.FormatInt(show.ID, 10)
	}
	return extra
}

func UserPayload(u *db.User) map[string]any {
	if u == nil {
		return map[string]any{}
	}
	return map[string]any{
		"Username":             u.Username,
		"NotificationUsername": u.Username,
		"UserId":               strconv.FormatInt(u.ID, 10),
	}
}

func TaskPayload(message string) map[string]any {
	return map[string]any{"Message": message}
}

func webhookItemType(kind string) string {
	switch strings.ToLower(kind) {
	case "movie":
		return "Movie"
	case "show":
		return "Series"
	case "episode":
		return "Episode"
	default:
		return "Video"
	}
}

func formatDuration(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
