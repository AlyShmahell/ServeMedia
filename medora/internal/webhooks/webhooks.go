package webhooks

import (
	"net/http"
	"time"

	"github.com/alyshmahell/medora/internal/config"
)

const (
	NotificationGeneric           = "Generic"
	NotificationPlaybackStart     = "PlaybackStart"
	NotificationPlaybackProgress  = "PlaybackProgress"
	NotificationPlaybackStop      = "PlaybackStop"
	NotificationItemAdded         = "ItemAdded"
	NotificationTaskCompleted     = "TaskCompleted"
	NotificationUserCreated       = "UserCreated"
	NotificationUserDeleted       = "UserDeleted"
)

var AllNotificationTypes = []string{
	NotificationGeneric,
	NotificationPlaybackStart,
	NotificationPlaybackProgress,
	NotificationPlaybackStop,
	NotificationItemAdded,
	NotificationTaskCompleted,
	NotificationUserCreated,
	NotificationUserDeleted,
}

var AllItemTypes = []string{
	"Movie", "Episode", "Season", "Series", "Album", "Song", "Video",
}

type Service struct {
	Cfg *config.Config
	hc  *http.Client
}

func New(cfg *config.Config) *Service {
	return &Service{
		Cfg: cfg,
		hc:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.Cfg != nil && s.Cfg.Integrations.Webhooks.Enabled
}

func (s *Service) Refresh(cfg *config.Config) {
	if s == nil {
		return
	}
	s.Cfg = cfg
}

func MaskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
