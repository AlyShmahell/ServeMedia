package server

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/listen"
)

// NetworkBind is the address accepting connections after a save or reset.
type NetworkBind struct {
	Host  string
	Port  string
	URL   string
	Note  string
	LanUp bool
}

// NetworkApply applies a saved address. disable clears it: desktop stops the
// LAN listener, and server mode returns to every interface on port 7676.
type NetworkApply func(host string, port int, disable bool) (NetworkBind, error)

type networkState struct {
	mu           sync.Mutex
	desktop      bool
	loopbackURL  string
	flagOverride bool
	host         string
	port         string
	url          string
	lanUp        bool
	apply        NetworkApply
}

// SetNetwork records the listener currently in use and the hot-rebind callback.
func (s *Server) SetNetwork(desktop bool, loopbackURL string, flagOverride bool, actual NetworkBind, apply NetworkApply) {
	s.netState.mu.Lock()
	defer s.netState.mu.Unlock()
	s.netState.desktop = desktop
	s.netState.loopbackURL = loopbackURL
	s.netState.flagOverride = flagOverride
	s.netState.host = actual.Host
	s.netState.port = actual.Port
	s.netState.url = actual.URL
	s.netState.lanUp = actual.LanUp
	s.netState.apply = apply
}

func (s *Server) handleSettingsNetwork(w http.ResponseWriter, r *http.Request) {
	if !adminUser(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.renderSettings(w, r, "network", s.networkData(r))
}

func (s *Server) handleSaveNetwork(w http.ResponseWriter, r *http.Request) {
	if !adminUser(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Cfg == nil {
		http.Error(w, "no config", http.StatusInternalServerError)
		return
	}
	_ = r.ParseForm()
	if r.FormValue("reset") == "1" {
		s.Cfg.Network = nil
		if err := s.Cfg.Save(s.Cfg.OverlaySavePath()); err != nil {
			s.failNetwork(w, r, err)
			return
		}
		bind, err := s.applyNetwork("", 0, true)
		s.finishNetwork(w, r, bind, err)
		return
	}
	host, port, err := parseNetworkForm(r.FormValue("host"), r.FormValue("port"))
	if err != nil {
		data := s.networkData(r)
		data["NetworkHost"] = strings.TrimSpace(r.FormValue("host"))
		data["NetworkPort"] = strings.TrimSpace(r.FormValue("port"))
		data["Notice"] = err.Error()
		data["NoticeLevel"] = "error"
		r.Header.Set("HX-Target", "settings-panel")
		s.renderSettings(w, r, "network", data)
		return
	}
	s.Cfg.Network = &config.NetworkConfig{Host: host, Port: port}
	if err := s.Cfg.Save(s.Cfg.OverlaySavePath()); err != nil {
		s.failNetwork(w, r, err)
		return
	}
	bind, err := s.applyNetwork(host, port, false)
	s.finishNetwork(w, r, bind, err)
}

func (s *Server) failNetwork(w http.ResponseWriter, r *http.Request, err error) {
	data := s.networkData(r)
	data["Notice"] = err.Error()
	data["NoticeLevel"] = "error"
	r.Header.Set("HX-Target", "settings-panel")
	s.renderSettings(w, r, "network", data)
}

func (s *Server) finishNetwork(w http.ResponseWriter, r *http.Request, bind NetworkBind, err error) {
	data := s.networkData(r)
	switch {
	case err != nil:
		msg := err.Error()
		if bind.Note != "" {
			msg = err.Error() + " " + bind.Note
		}
		data["Notice"] = msg
		data["NoticeLevel"] = "error"
	case bind.Note != "":
		data["Notice"] = bind.Note
		data["NoticeLevel"] = "warn"
	default:
		data["Notice"] = "Saved"
		data["NoticeLevel"] = "ok"
	}
	r.Header.Set("HX-Target", "settings-panel")
	s.renderSettings(w, r, "network", data)
}

func (s *Server) applyNetwork(host string, port int, disable bool) (NetworkBind, error) {
	s.netState.mu.Lock()
	apply := s.netState.apply
	s.netState.mu.Unlock()
	if apply == nil {
		return NetworkBind{}, fmt.Errorf("network changes are unavailable")
	}
	bind, err := apply(host, port, disable)
	s.netState.mu.Lock()
	if bind.Port != "" {
		s.netState.host = bind.Host
		s.netState.port = bind.Port
		s.netState.url = bind.URL
		s.netState.lanUp = bind.LanUp
	}
	s.netState.mu.Unlock()
	return bind, err
}

func (s *Server) networkData(r *http.Request) map[string]any {
	s.netState.mu.Lock()
	desktop := s.netState.desktop
	loopbackURL := s.netState.loopbackURL
	flagOverride := s.netState.flagOverride
	host := s.netState.host
	port := s.netState.port
	rawURL := s.netState.url
	lanUp := s.netState.lanUp
	s.netState.mu.Unlock()

	formHost, formPort := "", ""
	if s.Cfg != nil && s.Cfg.Network != nil {
		formHost = s.Cfg.Network.Host
		if s.Cfg.Network.Port > 0 {
			formPort = strconv.Itoa(s.Cfg.Network.Port)
		}
	}
	showLoopback := desktop && lanUp && loopbackURL != "" && loopbackURL != rawURL
	loopback := ""
	if showLoopback {
		loopback = loopbackURL
	}
	return map[string]any{
		"NetworkHost":  formHost,
		"NetworkPort":  formPort,
		"ActualHost":   host,
		"ActualPort":   port,
		"ActualURL":    rawURL,
		"ActualAll":    port != "" && listen.IsWildcard(host),
		"LoopbackURL":  loopback,
		"FlagOverride": flagOverride,
		"LanHolds":     desktop && lanUp,
		"NeedsAccount": s.networkNeedsAccount(r, host, desktop, lanUp),
		"LocalAddrs":   listen.SuggestHosts(),
	}
}

func (s *Server) networkNeedsAccount(r *http.Request, host string, desktop, lanUp bool) bool {
	expose := false
	switch {
	case desktop && lanUp:
		expose = true
	case !desktop && host != "" && (listen.IsWildcard(host) || !listen.IsLoopback(host)):
		expose = true
	}
	if !expose || s.DB == nil {
		return false
	}
	users, err := s.accountUsers(r.Context())
	return err == nil && len(users) == 0
}

func adminUser(r *http.Request) bool {
	u := userFrom(r)
	return u != nil && u.Role == db.RoleAdmin
}

func parseNetworkForm(host, port string) (string, int, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			return "", 0, fmt.Errorf("IP address must be a literal address")
		}
		host = ip.String()
	}
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("port must be from 1 to 65535")
	}
	return host, n, nil
}
