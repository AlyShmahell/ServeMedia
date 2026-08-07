package webhooks_test

import (
	"testing"

	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/webhooks"
)

func TestBasePayload(t *testing.T) {
	p := webhooks.BasePayload("server-uuid", "https://medora.test", webhooks.NotificationPlaybackStart)
	if p["ServerId"] != "server-uuid" {
		t.Fatalf("ServerId = %v", p["ServerId"])
	}
	if p["ServerUrl"] != "https://medora.test" {
		t.Fatalf("ServerUrl = %v", p["ServerUrl"])
	}
	if p["NotificationType"] != webhooks.NotificationPlaybackStart {
		t.Fatalf("NotificationType = %v", p["NotificationType"])
	}
}

func TestPlaybackPayload(t *testing.T) {
	movie := webhooks.PlaybackPayload("movie", 1, "Film", "alice", 2, 0, 0, false)
	if movie["ItemType"] != "Movie" {
		t.Fatalf("ItemType = %v", movie["ItemType"])
	}
	ep := webhooks.PlaybackPayload("episode", 3, "Ep", "alice", 2, 10, 100, false)
	if ep["ItemType"] != "Episode" {
		t.Fatalf("ItemType = %v", ep["ItemType"])
	}
}

func TestMediaItemPayload(t *testing.T) {
	item := &db.MediaItem{
		ID:    5,
		Kind:  "movie",
		Title: "Film",
	}
	p := webhooks.MediaItemPayload(item)
	if p["ItemType"] != "Movie" {
		t.Fatalf("ItemType = %v", p["ItemType"])
	}
}

func TestMaskKey(t *testing.T) {
	if got := webhooks.MaskKey("abcd"); got != "****" {
		t.Fatalf("got %q", got)
	}
	if got := webhooks.MaskKey("abcdefghij"); got != "****ghij" {
		t.Fatalf("got %q", got)
	}
}
