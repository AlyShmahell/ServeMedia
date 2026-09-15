package webhooks_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/webhooks"
)

func testWebhooksConfig() config.WebhooksConfig {
	return config.WebhooksConfig{
		Enabled: true,
		APIKey:  "secret-key-abc123",
	}
}

func TestDispatchSyncSendsMedoraPayload(t *testing.T) {
	var gotBody map[string]any
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Medora-Webhook-Key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := testWebhooksConfig()
	wh.Destinations = []config.WebhookDestination{{
		Name:    "test",
		URL:     srv.URL,
		Enabled: true,
	}}
	svc := webhooks.New(nil)
	payload := webhooks.MergePayload(
		webhooks.BasePayload("server-uuid", "https://medora.test", webhooks.NotificationGeneric),
		map[string]any{"Message": "hello"},
	)
	sent, errs := svc.DispatchSync(wh, webhooks.NotificationGeneric, "", payload)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if sent != 1 {
		t.Fatalf("sent = %d", sent)
	}
	if gotBody["ServerName"] != "Medora" {
		t.Fatalf("ServerName = %v", gotBody["ServerName"])
	}
	if gotKey != wh.APIKey {
		t.Fatalf("key = %q", gotKey)
	}
}

func TestDispatchSyncFiltersNotificationType(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := testWebhooksConfig()
	wh.Destinations = []config.WebhookDestination{{
		Name:              "test",
		URL:               srv.URL,
		Enabled:           true,
		NotificationTypes: []string{webhooks.NotificationPlaybackStart},
	}}
	svc := webhooks.New(nil)
	payload := webhooks.BasePayload("id", "", webhooks.NotificationGeneric)
	sent, _ := svc.DispatchSync(wh, webhooks.NotificationGeneric, "", payload)
	if sent != 0 || called {
		t.Fatal("expected no dispatch for filtered type")
	}
}

func TestDispatchSyncFiltersItemType(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := testWebhooksConfig()
	wh.Destinations = []config.WebhookDestination{{
		Name:      "test",
		URL:       srv.URL,
		Enabled:   true,
		ItemTypes: []string{"Movie"},
	}}
	svc := webhooks.New(nil)
	payload := webhooks.BasePayload("id", "", webhooks.NotificationGeneric)
	sent, _ := svc.DispatchSync(wh, webhooks.NotificationGeneric, "Episode", payload)
	if sent != 0 || called {
		t.Fatal("expected no dispatch for filtered item type")
	}
}

func TestDispatchSyncCustomTemplate(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := testWebhooksConfig()
	wh.Destinations = []config.WebhookDestination{{
		Name:     "test",
		URL:      srv.URL,
		Enabled:  true,
		Template: `{"server":"{{ServerName}}","type":"{{NotificationType}}"}`,
	}}
	svc := webhooks.New(nil)
	payload := webhooks.BasePayload("id", "", webhooks.NotificationGeneric)
	sent, errs := svc.DispatchSync(wh, webhooks.NotificationGeneric, "", payload)
	if len(errs) != 0 || sent != 1 {
		t.Fatalf("sent=%d errs=%v", sent, errs)
	}
	if raw != `{"server":"Medora","type":"Generic"}` {
		t.Fatalf("body = %s", raw)
	}
}

func TestDispatchDisabled(t *testing.T) {
	wh := testWebhooksConfig()
	wh.Enabled = false
	svc := webhooks.New(nil)
	sent, errs := svc.DispatchTest(context.Background(), &wh, "server-id", "alice")
	if sent != 0 || len(errs) == 0 {
		t.Fatalf("sent=%d errs=%v", sent, errs)
	}
}
