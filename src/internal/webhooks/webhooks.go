package webhooks

import (
	"net/http"
	"strings"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/config"
)

const (
	NotificationGeneric          = "Generic"
	NotificationPlaybackStart    = "PlaybackStart"
	NotificationPlaybackProgress = "PlaybackProgress"
	NotificationPlaybackStop     = "PlaybackStop"
	NotificationItemAdded        = "ItemAdded"
	NotificationTaskCompleted    = "TaskCompleted"
	NotificationUserCreated      = "UserCreated"
	NotificationUserDeleted      = "UserDeleted"
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
	Cfg         *config.Config
	LoopbackURL string
	hc          *http.Client
}

func New(cfg *config.Config) *Service {
	return &Service{
		Cfg: cfg,
		hc:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) Refresh(cfg *config.Config) {
	if s == nil {
		return
	}
	s.Cfg = cfg
}

func (s *Service) SetLoopbackURL(u string) {
	if s == nil {
		return
	}
	s.LoopbackURL = u
}

func (s *Service) serverURL(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.LoopbackURL)
}

func (s *Service) serverVersion() string {
	if s == nil || s.Cfg == nil {
		return ""
	}
	return s.Cfg.Version
}

func MaskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
