// Package listen rebinds the process accept socket without dropping the
// request that asked for the change.
package listen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const gracePeriod = 30 * time.Second

// Addr is the address a listener actually accepted.
type Addr struct {
	Host string
	Port string
}

func (a Addr) String() string {
	if a.Port == "" {
		return ""
	}
	return net.JoinHostPort(a.Host, a.Port)
}

// OpenURL is the URL a browser on this machine can open.
// Wildcard and loopback binds use 127.0.0.1. Any other address is used as-is.
func (a Addr) OpenURL() string {
	if a.Port == "" {
		return ""
	}
	host := a.Host
	if IsWildcard(host) || IsLoopback(host) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, a.Port)
}

// IsWildcard reports an unspecified bind (every interface).
func IsWildcard(host string) bool {
	return canonicalHost(host) == "*"
}

// IsLoopback reports a loopback address.
func IsLoopback(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SuggestHosts lists wildcard addresses and this machine's unicast addresses.
func SuggestHosts() []string {
	out := []string{"0.0.0.0", "::", "127.0.0.1", "::1"}
	seen := map[string]bool{}
	for _, h := range out {
		seen[h] = true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP == nil {
			continue
		}
		ip := ipnet.IP
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() {
			continue
		}
		s := ip.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

type bindKind int

const (
	kindOK bindKind = iota
	kindPort
	kindIP
	kindOther
)

// Supervisor owns one accept socket and the HTTP server on it.
type Supervisor struct {
	mu       sync.Mutex
	handler  http.Handler
	ln       net.Listener
	srv      *http.Server
	actual   Addr
	listenFn func(network, address string) (net.Listener, error)
}

func NewSupervisor() *Supervisor {
	return &Supervisor{}
}

func (s *Supervisor) SetHandler(h http.Handler) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}

// Use serves ln. It replaces a listener this supervisor already holds.
func (s *Supervisor) Use(ln net.Listener) (Addr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handler == nil {
		return Addr{}, errors.New("listen: no handler")
	}
	if ln == nil {
		return Addr{}, errors.New("listen: no listener")
	}
	actual, err := addrOf(ln)
	if err != nil {
		return Addr{}, err
	}
	s.install(ln, actual)
	return s.actual, nil
}

// Bind applies host and port. An empty host listens on every interface.
// A busy or forbidden port falls back to the previous port, then to an
// ephemeral port. An address that cannot be assigned falls back to the
// previous host. If every attempt fails, the listener already held stays.
func (s *Supervisor) Bind(host, port string) (Addr, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handler == nil {
		return Addr{}, "", errors.New("listen: no handler")
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	port = strings.TrimSpace(port)
	if port == "" {
		port = "7676"
	}
	if _, err := listenAddr(host, port); err != nil {
		return s.actual, "", err
	}
	prev := s.actual
	var sawPort, sawIP bool
	reason := ""
	ln, actual, held, ok := s.cascade(host, port, prev.Port, &sawPort, &sawIP, &reason)
	if !ok && sawIP && prev.Port != "" && canonicalHost(prev.Host) != canonicalHost(host) {
		ln, actual, held, ok = s.cascade(prev.Host, port, prev.Port, &sawPort, &sawIP, &reason)
	}
	if !ok {
		if s.ln == nil && prev.Port != "" {
			if err := s.restore(prev); err != nil {
				return Addr{}, "", err
			}
		}
		label := host
		if label == "" {
			label = "all interfaces"
		}
		return s.actual, "", fmt.Errorf("could not bind %s:%s", label, port)
	}
	if !held {
		s.install(ln, actual)
	}
	return s.actual, describe(host, port, s.actual, sawPort, sawIP, reason), nil
}

// Stop drops the listener. Existing connections get gracePeriod to finish.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.ln = nil
	s.actual = Addr{}
	s.mu.Unlock()
	if srv != nil {
		go shutdownAfter(srv, gracePeriod)
	}
}

// Shutdown waits for connections to finish, then closes them.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.ln = nil
	s.actual = Addr{}
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	_ = srv.Close()
	return err
}

func (s *Supervisor) Current() (Addr, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil || s.actual.Port == "" {
		return Addr{}, false
	}
	return s.actual, true
}

func (s *Supervisor) cascade(host, requested, previous string, sawPort, sawIP *bool, reason *string) (net.Listener, Addr, bool, bool) {
	for _, p := range portCascade(requested, previous) {
		if s.ln != nil && sameBind(s.actual, host, p) {
			return s.ln, s.actual, true, true
		}
		ln, kind, denied := s.tryOne(host, p)
		if ln != nil {
			actual, err := addrOf(ln)
			if err != nil {
				_ = ln.Close()
				continue
			}
			return ln, actual, false, true
		}
		switch kind {
		case kindIP:
			*sawIP = true
			return nil, Addr{}, false, false
		case kindPort:
			*sawPort = true
			if denied {
				*reason = "denied"
			} else if *reason == "" {
				*reason = "busy"
			}
		}
	}
	return nil, Addr{}, false, false
}

func (s *Supervisor) tryOne(host, port string) (net.Listener, bindKind, bool) {
	addr, err := listenAddr(host, port)
	if err != nil {
		return nil, kindOther, false
	}
	ln, err := s.open(addr)
	if err == nil {
		return ln, kindOK, false
	}
	kind, denied := classify(err)
	if kind == kindPort && s.ln != nil && overlaps(s.actual, host, port) {
		_ = s.ln.Close()
		s.ln = nil
		ln, err = s.open(addr)
		if err == nil {
			return ln, kindOK, false
		}
		kind, denied = classify(err)
	}
	return nil, kind, denied
}

func (s *Supervisor) open(addr string) (net.Listener, error) {
	if s.listenFn != nil {
		return s.listenFn("tcp", addr)
	}
	return net.Listen("tcp", addr)
}

func (s *Supervisor) install(ln net.Listener, actual Addr) {
	oldSrv := s.srv
	oldLn := s.ln
	if oldLn != nil && oldLn != ln {
		_ = oldLn.Close()
	}
	s.ln = ln
	s.actual = actual
	s.srv = s.start(ln)
	if oldSrv != nil {
		go shutdownAfter(oldSrv, gracePeriod)
	}
}

func (s *Supervisor) start(ln net.Listener) *http.Server {
	srv := &http.Server{Handler: s.handler}
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("listen: %v", err)
		}
	}()
	return srv
}

// restore puts prev back after a failed attempt released its listen socket.
// It uses the real network stack so a test double cannot drop the last socket.
func (s *Supervisor) restore(prev Addr) error {
	addr, err := listenAddr(prev.Host, prev.Port)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		addr0, aerr := listenAddr(prev.Host, "0")
		if aerr != nil {
			return err
		}
		ln, err = net.Listen("tcp", addr0)
		if err != nil {
			return err
		}
	}
	actual, err := addrOf(ln)
	if err != nil {
		_ = ln.Close()
		return err
	}
	s.install(ln, actual)
	return nil
}

func shutdownAfter(srv *http.Server, d time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = srv.Close()
}

func portCascade(requested, previous string) []string {
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		for _, e := range out {
			if e == p {
				return
			}
		}
		out = append(out, p)
	}
	add(requested)
	add(previous)
	add("0")
	return out
}

func listenAddr(host, port string) (string, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	port = strings.TrimSpace(port)
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

func addrOf(ln net.Listener) (Addr, error) {
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || port == "" {
		return Addr{}, fmt.Errorf("listen address %v", ln.Addr())
	}
	return Addr{Host: host, Port: port}, nil
}

func canonicalHost(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return "*"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return strings.ToLower(host)
	}
	if ip.IsUnspecified() {
		return "*"
	}
	return ip.String()
}

func sameBind(cur Addr, host, port string) bool {
	if cur.Port == "" || port == "" || port == "0" {
		return false
	}
	return cur.Port == port && canonicalHost(cur.Host) == canonicalHost(host)
}

func overlaps(cur Addr, host, port string) bool {
	if cur.Port == "" || port == "" || port == "0" || cur.Port != port {
		return false
	}
	ch := canonicalHost(cur.Host)
	h := canonicalHost(host)
	return ch == h || ch == "*" || h == "*"
}

func classify(err error) (bindKind, bool) {
	if err == nil {
		return kindOK, false
	}
	denied := errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
	switch {
	case errors.Is(err, syscall.EADDRINUSE), denied:
		return kindPort, denied
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		return kindIP, false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "address already in use"):
		return kindPort, false
	case strings.Contains(msg, "permission denied"):
		return kindPort, true
	case strings.Contains(msg, "cannot assign"), strings.Contains(msg, "not available"):
		return kindIP, false
	default:
		return kindOther, false
	}
}

func describe(requestedHost, requestedPort string, actual Addr, sawPort, sawIP bool, reason string) string {
	hostSame := canonicalHost(requestedHost) == canonicalHost(actual.Host)
	portSame := requestedPort == actual.Port
	if hostSame && portSame {
		return ""
	}
	var parts []string
	if sawPort && !portSame {
		if reason == "denied" {
			parts = append(parts, fmt.Sprintf("Port %s is not permitted", requestedPort))
		} else {
			parts = append(parts, fmt.Sprintf("Port %s is in use", requestedPort))
		}
	}
	if sawIP && !hostSame {
		label := requestedHost
		if label == "" {
			label = "the address"
		}
		parts = append(parts, fmt.Sprintf("Could not bind %s", label))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + fmt.Sprintf("; listening on %s.", actual.String())
}
