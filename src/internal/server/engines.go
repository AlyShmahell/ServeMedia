package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"gopkg.in/yaml.v3"
)

func (s *Server) enginesStatus() (ready bool, version, reason string) {
	if s.Meta == nil {
		return false, "", "MatchMedia unavailable"
	}
	st, err := s.Meta.Status()
	if err != nil || !st.Ready {
		reason = st.DisabledReason
		if reason == "" {
			reason = "MatchMedia unavailable"
		}
		return false, st.Version, reason
	}
	return true, st.Version, ""
}

func (s *Server) handleEnginesStatus(w http.ResponseWriter, r *http.Request) {
	ready, _, _ := s.enginesStatus()
	s.render(w, r, "partials/engines_status.html", map[string]any{
		"Ready": ready,
	})
}

func settingsEnginesTab(r *http.Request) string {
	if strings.TrimSpace(r.URL.Query().Get("tab")) == "ffmpeg" {
		return "ffmpeg"
	}
	return "matchmedia"
}

func (s *Server) enginesSettingsData() map[string]any {
	ready, ver, reason := s.enginesStatus()
	secrets := map[string]bool{}
	var secretKeys []string
	overlayYAML := ""
	var overlayErr string
	if s.Meta != nil {
		if st, err := s.Meta.SecretsStatus(); err == nil {
			secrets = st
			for k := range st {
				secretKeys = append(secretKeys, k)
			}
			sort.Strings(secretKeys)
		}
		if cfg, err := s.Meta.Config(); err == nil {
			b, err := yaml.Marshal(cfg)
			if err != nil {
				overlayErr = err.Error()
			} else {
				overlayYAML = string(b)
			}
		} else {
			overlayErr = err.Error()
		}
	}
	hw := ""
	active := ""
	hwOn := false
	if s.Transcode != nil {
		hw = s.Transcode.HWAccelStatus()
		active = s.Transcode.ActiveTranscodeStatus()
		hwOn = strings.TrimSpace(hw) != "" && !strings.EqualFold(hw, "software")
	}
	return map[string]any{
		"Ready":           ready,
		"MetaVersion":     ver,
		"Reason":          reason,
		"Secrets":         secrets,
		"SecretKeys":      secretKeys,
		"OverlayYAML":     overlayYAML,
		"OverlayError":    overlayErr,
		"Config":          s.Cfg,
		"TranscodeHW":     hw,
		"TranscodeHWOn":   hwOn,
		"ActiveTranscode": active,
	}
}

func (s *Server) handleSettingsEngines(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, settingsEnginesTab(r), s.enginesSettingsData())
}

func (s *Server) handleSaveEngines(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("ffmpeg_settings") == "1" {
		s.saveFFmpegSettings(w, r)
		return
	}
	if s.Meta != nil {
		updates := map[string]string{}
		for k := range r.PostForm {
			if !strings.HasPrefix(k, "secret_") {
				continue
			}
			key := strings.TrimPrefix(k, "secret_")
			if key == "" {
				continue
			}
			if v := strings.TrimSpace(r.FormValue(k)); v != "" {
				updates[key] = v
			}
		}
		if len(updates) > 0 {
			if err := s.Meta.SetSecrets(updates); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		raw := r.FormValue("overlay_yaml")
		if strings.TrimSpace(raw) != "" {
			var patch map[string]any
			if err := yaml.Unmarshal([]byte(raw), &patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if patch == nil {
				patch = map[string]any{}
			}
			if err := s.Meta.SetConfig(patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	s.finishEngines(w, r, "matchmedia")
}

func (s *Server) saveFFmpegSettings(w http.ResponseWriter, r *http.Request) {
	crf, err := strconv.Atoi(strings.TrimSpace(r.FormValue("crf")))
	if err != nil || crf <= 0 {
		http.Error(w, "crf must be a positive number", http.StatusBadRequest)
		return
	}
	height, err := strconv.Atoi(strings.TrimSpace(r.FormValue("max_height")))
	if err != nil || height <= 0 {
		http.Error(w, "max height must be a positive number", http.StatusBadRequest)
		return
	}
	segment, err := strconv.Atoi(strings.TrimSpace(r.FormValue("segment_seconds")))
	if err != nil || segment <= 0 {
		http.Error(w, "segment seconds must be a positive number", http.StatusBadRequest)
		return
	}
	cleanup, err := strconv.Atoi(strings.TrimSpace(r.FormValue("cleanup_hours")))
	if err != nil || cleanup <= 0 {
		http.Error(w, "cleanup hours must be a positive number", http.StatusBadRequest)
		return
	}
	hw := strings.ToLower(strings.TrimSpace(r.FormValue("hwaccel")))
	switch hw {
	case "auto", "vaapi", "none":
	default:
		http.Error(w, "hwaccel must be auto, vaapi, or none", http.StatusBadRequest)
		return
	}
	s.Cfg.Transcode.CRF = crf
	s.Cfg.Transcode.MaxHeight = height
	s.Cfg.Transcode.SegmentSeconds = segment
	s.Cfg.Transcode.CleanupHours = cleanup
	s.Cfg.Transcode.HWAccel = hw
	if err := s.Cfg.Save(s.Cfg.OverlaySavePath()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Transcode != nil {
		s.Transcode.UseConfig(*s.Cfg)
	}
	s.finishEngines(w, r, "ffmpeg")
}

func (s *Server) finishEngines(w http.ResponseWriter, r *http.Request, tab string) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.finishSettings(w, r, tab, s.enginesSettingsData())
}

func (s *Server) handleRestoreEngines(w http.ResponseWriter, r *http.Request) {
	patch := map[string]any{}
	if s.Meta != nil {
		if cur, err := s.Meta.Config(); err == nil {
			patch = matchmedia.APIKeyPatch(cur)
		}
		if err := s.Meta.SetConfig(patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.finishEngines(w, r, "matchmedia")
}

func (s *Server) handleRestartEngines(w http.ResponseWriter, r *http.Request) {
	if s.restartMeta == nil {
		http.Error(w, "MatchMedia restart unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.restartMeta(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		s.handleEnginesStatus(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
