package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
)

func TestUserWebhooksDefaultsAndSave(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "alice", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	wh, err := d.GetUserWebhooks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wh.APIKey == "" {
		t.Fatal("expected auto-generated api key")
	}

	saved, err := d.SaveUserWebhooks(ctx, u.ID, config.WebhooksConfig{
		Enabled:   true,
		ServerURL: "http://192.168.1.10:7676",
		Destinations: []config.WebhookDestination{{
			Name:    "Home",
			URL:     "http://192.168.1.10:9090/hook",
			Enabled: true,
			NotificationTypes: []string{"PlaybackStart"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || len(saved.Destinations) != 1 {
		t.Fatalf("got %+v", saved)
	}

	regen, err := d.RegenerateUserWebhookKey(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if regen.APIKey == wh.APIKey {
		t.Fatal("expected new key after regenerate")
	}
}

func TestImportLegacyWebhooks(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	legacy := config.WebhooksConfig{
		Enabled:   true,
		ServerURL: "https://medora.test",
		APIKey:    "legacy-key",
		Destinations: []config.WebhookDestination{{
			Name: "Legacy", URL: "http://example/hook", Enabled: true,
		}},
	}
	if err := d.ImportLegacyWebhooks(ctx, u.ID, legacy); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetUserWebhooks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.ServerURL != legacy.ServerURL || len(got.Destinations) != 1 {
		t.Fatalf("got %+v", got)
	}
}
