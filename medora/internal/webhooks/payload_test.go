package webhooks_test

import (
	"database/sql"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/webhooks"
)

func testConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Integrations.Webhooks.Enabled = true
	cfg.Integrations.Webhooks.ServerURL = "https://medora.test"
	cfg.Integrations.Webhooks.ServerID = "server-uuid"
	cfg.Integrations.Webhooks.APIKey = "secret-key-abc123"
	return &cfg
}

func TestBasePayloadIdentifiesAsMedora(t *testing.T) {
	cfg := testConfig()
	p := webhooks.BasePayload(cfg, webhooks.NotificationPlaybackStart)
	if p["ServerName"] != "Medora" {
		t.Fatalf("ServerName = %v", p["ServerName"])
	}
	if p["ClientName"] != "Medora" {
		t.Fatalf("ClientName = %v", p["ClientName"])
	}
	if p["NotificationType"] != webhooks.NotificationPlaybackStart {
		t.Fatalf("NotificationType = %v", p["NotificationType"])
	}
	if p["ServerUrl"] != "https://medora.test" {
		t.Fatalf("ServerUrl = %v", p["ServerUrl"])
	}
}

func TestPlaybackPayloadItemTypes(t *testing.T) {
	movie := webhooks.PlaybackPayload("movie", 1, "Film", "alice", 2, 0, 0, false)
	if movie["ItemType"] != "Movie" {
		t.Fatalf("movie ItemType = %v", movie["ItemType"])
	}
	ep := webhooks.PlaybackPayload("episode", 3, "Ep", "alice", 2, 10, 100, false)
	if ep["ItemType"] != "Episode" {
		t.Fatalf("episode ItemType = %v", ep["ItemType"])
	}
}

func TestMediaItemPayloadSeries(t *testing.T) {
	item := &db.MediaItem{
		ID:    5,
		Kind:  "show",
		Title: "Sample Show",
		MetaProvider: sql.NullString{String: "tmdb", Valid: true},
		MetaID:       sql.NullString{String: "123", Valid: true},
	}
	p := webhooks.MediaItemPayload(item)
	if p["ItemType"] != "Series" {
		t.Fatalf("ItemType = %v", p["ItemType"])
	}
	if p["Provider_tmdb"] != "123" {
		t.Fatalf("Provider_tmdb = %v", p["Provider_tmdb"])
	}
}

func TestMaskKey(t *testing.T) {
	if got := webhooks.MaskKey("abcd"); got != "****" {
		t.Fatalf("short key mask = %q", got)
	}
	if got := webhooks.MaskKey("abcdefghij"); got != "****ghij" {
		t.Fatalf("long key mask = %q", got)
	}
}
