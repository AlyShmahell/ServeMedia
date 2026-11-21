package listen

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

func stopSup(t *testing.T, s *Supervisor) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
}

func testSup(t *testing.T) *Supervisor {
	t.Helper()
	s := NewSupervisor()
	s.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	stopSup(t, s)
	return s
}

func TestBindFallsBackToPreviousPort(t *testing.T) {
	s := testSup(t)
	got, note, err := s.Bind("127.0.0.1", "0")
	if err != nil {
		t.Fatal(err)
	}
	if note != "" {
		t.Fatalf("note %q", note)
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, busy, err := net.SplitHostPort(held.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	again, note, err := s.Bind("127.0.0.1", busy)
	if err != nil {
		t.Fatal(err)
	}
	if again.Port != got.Port {
		t.Fatalf("port %s want previous %s", again.Port, got.Port)
	}
	if note == "" {
		t.Fatal("expected fallback note")
	}
}

func TestBindEphemeralWhenNoPrevious(t *testing.T) {
	s := testSup(t)
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, busy, err := net.SplitHostPort(held.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	got, note, err := s.Bind("127.0.0.1", busy)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port == busy || got.Port == "" {
		t.Fatalf("port %s note %q", got.Port, note)
	}
	if note == "" {
		t.Fatal("expected fallback note")
	}
}

func TestBindEphemeralWhenPreviousAlsoBusy(t *testing.T) {
	s := testSup(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Use(ln); err != nil {
		t.Fatal(err)
	}
	prev, ok := s.Current()
	if !ok {
		t.Fatal("no listener")
	}
	s.listenFn = func(network, address string) (net.Listener, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if port == "9999" || port == prev.Port {
			return nil, &net.OpError{Op: "listen", Net: network, Err: syscall.EADDRINUSE}
		}
		return net.Listen(network, address)
	}
	// Wildcard on the held port overlaps, so the previous port is released
	// and then rejected by listenFn. The last step is an ephemeral port.
	got, note, err := s.Bind("", prev.Port)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port == prev.Port || got.Port == "9999" || got.Port == "" {
		t.Fatalf("port %s note %q", got.Port, note)
	}
	resp, err := http.Get(got.OpenURL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestBindFallsBackToPreviousIP(t *testing.T) {
	s := testSup(t)
	if _, _, err := s.Bind("127.0.0.1", "0"); err != nil {
		t.Fatal(err)
	}
	got, note, err := s.Bind("192.0.2.1", "7676")
	if err != nil {
		t.Fatal(err)
	}
	if !IsLoopback(got.Host) {
		t.Fatalf("got %s note %q", got.String(), note)
	}
	if note == "" {
		t.Fatal("expected IP fallback note")
	}
	resp, err := http.Get(got.OpenURL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestBindFailureKeepsListener(t *testing.T) {
	s := testSup(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Use(ln); err != nil {
		t.Fatal(err)
	}
	prev, _ := s.Current()
	s.listenFn = func(network, address string) (net.Listener, error) {
		return nil, &net.OpError{Op: "listen", Net: network, Err: syscall.EADDRNOTAVAIL}
	}
	got, _, err := s.Bind("192.0.2.1", "7676")
	if err == nil {
		t.Fatal("expected error")
	}
	if got.Port != prev.Port {
		t.Fatalf("port %s want %s", got.Port, prev.Port)
	}
	resp, err := http.Get(prev.OpenURL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestBindDuringRequest(t *testing.T) {
	s := NewSupervisor()
	stopSup(t, s)
	s.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur, ok := s.Current()
		if !ok {
			http.Error(w, "no listener", http.StatusInternalServerError)
			return
		}
		got, _, err := s.Bind("", cur.Port)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, got.String())
	}))
	first, _, err := s.Bind("127.0.0.1", "0")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(first.OpenURL())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}
}

func TestSuggestHostsIncludesWildcard(t *testing.T) {
	got := SuggestHosts()
	if len(got) < 2 || got[0] != "0.0.0.0" || got[1] != "::" {
		t.Fatalf("%v", got)
	}
}
