package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
)

func (s *Service) Dispatch(ctx context.Context, notificationType string, extra map[string]any) {
	if s == nil || !s.Enabled() {
		return
	}
	base := BasePayload(s.Cfg, notificationType)
	payload := MergePayload(base, extra)
	itemType, _ := payload["ItemType"].(string)
	go s.dispatchAll(notificationType, itemType, payload)
}

func (s *Service) DispatchTest(ctx context.Context) (int, []string) {
	if s == nil || !s.Enabled() {
		return 0, []string{"webhooks disabled"}
	}
	payload := MergePayload(BasePayload(s.Cfg, NotificationGeneric), map[string]any{
		"Message": "Medora webhook test",
	})
	return s.DispatchSync(NotificationGeneric, "", payload)
}

func (s *Service) DispatchPlaybackStart(ctx context.Context, kind string, id int64, title string, u *db.User) {
	if u == nil {
		return
	}
	s.Dispatch(ctx, NotificationPlaybackStart, PlaybackPayload(kind, id, title, u.Username, u.ID, 0, 0, false))
}

func (s *Service) DispatchPlaybackProgress(ctx context.Context, kind string, id int64, title string, u *db.User, pos, dur float64) {
	if u == nil {
		return
	}
	s.Dispatch(ctx, NotificationPlaybackProgress, PlaybackPayload(kind, id, title, u.Username, u.ID, pos, dur, false))
}

func (s *Service) DispatchPlaybackStop(ctx context.Context, kind string, id int64, title string, u *db.User, pos, dur float64) {
	if u == nil {
		return
	}
	s.Dispatch(ctx, NotificationPlaybackStop, PlaybackPayload(kind, id, title, u.Username, u.ID, pos, dur, true))
}

func (s *Service) DispatchItemAdded(ctx context.Context, item *db.MediaItem) {
	s.Dispatch(ctx, NotificationItemAdded, MediaItemPayload(item))
}

func (s *Service) DispatchEpisodeAdded(ctx context.Context, ep *db.Episode, show *db.MediaItem) {
	s.Dispatch(ctx, NotificationItemAdded, EpisodePayload(ep, show))
}

func (s *Service) DispatchTaskCompleted(ctx context.Context, message string) {
	s.Dispatch(ctx, NotificationTaskCompleted, TaskPayload(message))
}

func (s *Service) DispatchUserCreated(ctx context.Context, u *db.User) {
	s.Dispatch(ctx, NotificationUserCreated, UserPayload(u))
}

func (s *Service) DispatchUserDeleted(ctx context.Context, u *db.User) {
	s.Dispatch(ctx, NotificationUserDeleted, UserPayload(u))
}

func (s *Service) dispatchAll(notificationType, itemType string, payload map[string]any) {
	s.DispatchSync(notificationType, itemType, payload)
}

// DispatchSync sends to matching destinations synchronously (used by tests and DispatchTest).
func (s *Service) DispatchSync(notificationType, itemType string, payload map[string]any) (int, []string) {
	wh := s.Cfg.Integrations.Webhooks
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
