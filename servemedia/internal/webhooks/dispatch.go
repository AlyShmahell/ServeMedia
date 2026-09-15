package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alyshmahell/medora/internal/config"
)

func (s *Service) Dispatch(ctx context.Context, wh *config.WebhooksConfig, serverID, notificationType string, extra map[string]any) {
	if s == nil || wh == nil || !wh.Enabled {
		return
	}
	base := BasePayload(serverID, wh.ServerURL, notificationType)
	payload := MergePayload(base, extra)
	itemType, _ := payload["ItemType"].(string)
	go func() {
		_, _ = s.DispatchSync(*wh, notificationType, itemType, payload)
	}()
}

func (s *Service) DispatchTest(ctx context.Context, wh *config.WebhooksConfig, serverID, username string) (int, []string) {
	if s == nil {
		return 0, []string{"webhooks unavailable"}
	}
	if wh == nil || !wh.Enabled {
		return 0, []string{"webhooks disabled"}
	}
	payload := MergePayload(BasePayload(serverID, wh.ServerURL, NotificationGeneric), map[string]any{
		"Message":  "Medora webhook test",
		"Username": username,
	})
	return s.DispatchSync(*wh, NotificationGeneric, "", payload)
}

// DispatchSync sends to matching destinations synchronously (used by tests and DispatchTest).
func (s *Service) DispatchSync(wh config.WebhooksConfig, notificationType, itemType string, payload map[string]any) (int, []string) {
	if !wh.Enabled {
		return 0, nil
	}
	var errs []string
	sent := 0
	for _, dest := range wh.Destinations {
		if !dest.Enabled || strings.TrimSpace(dest.URL) == "" {
			continue
		}
		if !matchesNotificationType(dest, notificationType) {
			continue
		}
		if itemType != "" && len(dest.ItemTypes) > 0 && !containsCI(dest.ItemTypes, itemType) {
			continue
		}
		body, err := renderBody(dest, payload)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dest.Name, err))
			continue
		}
		req, err := http.NewRequest(http.MethodPost, dest.URL, bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dest.Name, err))
			continue
		}
		hasContentType := false
		for _, h := range dest.Headers {
			if strings.EqualFold(strings.TrimSpace(h.Key), "Content-Type") {
				hasContentType = true
			}
			req.Header.Set(h.Key, h.Value)
		}
		if !hasContentType {
			req.Header.Set("Content-Type", "application/json")
		}
		if wh.APIKey != "" {
			req.Header.Set("X-Medora-Webhook-Key", wh.APIKey)
		}
		resp, err := s.hc.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dest.Name, err))
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			errs = append(errs, fmt.Sprintf("%s: HTTP %d", dest.Name, resp.StatusCode))
			continue
		}
		sent++
	}
	return sent, errs
}

func matchesNotificationType(dest config.WebhookDestination, notificationType string) bool {
	if len(dest.NotificationTypes) == 0 {
		return true
	}
	return containsCI(dest.NotificationTypes, notificationType)
}

func containsCI(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func renderBody(dest config.WebhookDestination, payload map[string]any) ([]byte, error) {
	if strings.TrimSpace(dest.Template) == "" {
		return json.Marshal(payload)
	}
	out := dest.Template
	for k, v := range payload {
		placeholder := "{{" + k + "}}"
		out = strings.ReplaceAll(out, placeholder, fmt.Sprint(v))
	}
	return []byte(out), nil
}
