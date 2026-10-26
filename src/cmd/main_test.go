package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWebKitBindOverridesHostAndPort(t *testing.T) {
	host, port := webkitBind(true, "0.0.0.0", "7676")
	if host != "127.0.0.1" || port != "0" {
		t.Fatalf("webkit bind %s %s", host, port)
	}
	ln, err := listenTCP(host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotHost, gotPort, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if gotHost != "127.0.0.1" || gotPort == "7676" || gotPort == "0" {
		t.Fatalf("listener %s", ln.Addr())
	}
	host, port = webkitBind(false, "192.168.1.5", "7676")
	if host != "192.168.1.5" || port != "7676" {
		t.Fatalf("plain bind %s %s", host, port)
	}
}

func TestWebKitFailureFallsBackToBrowser(t *testing.T) {
	const page = "http://127.0.0.1:9/"
	var opened string
	wait := openUI(true, page, func(string) error {
		return context.DeadlineExceeded
	}, func(rawURL string) { opened = rawURL })
	if !wait || opened != page {
		t.Fatalf("wait %v opened %q", wait, opened)
	}
	wait = openUI(true, page, func(string) error { return nil }, func(string) {
		t.Fatal("browser opened after the window")
	})
	if wait {
		t.Fatal("successful window should not wait again")
	}
	opened = ""
	wait = openUI(false, page, func(string) error {
		t.Fatal("window opened without the flag")
		return nil
	}, func(rawURL string) { opened = rawURL })
	if !wait || opened != page {
		t.Fatalf("plain wait %v opened %q", wait, opened)
	}
}

func TestPresentSkipsWebKitWithoutFlag(t *testing.T) {
	if uiUsesWebKit(false) {
		t.Fatal("webkit path without flag")
	}
	if !uiUsesWebKit(true) {
		t.Fatal("webkit path missing")
	}
}

func TestListenAddr(t *testing.T) {
	got, err := listenAddr("", "")
	if err != nil || got != ":7676" {
		t.Fatalf("default %q %v", got, err)
	}
	got, err = listenAddr("", "7676")
	if err != nil || got != ":7676" {
		t.Fatalf("port %q %v", got, err)
	}
	got, err = listenAddr("127.0.0.1", "7676")
	if err != nil || got != "127.0.0.1:7676" {
		t.Fatalf("loopback %q %v", got, err)
	}
	got, err = listenAddr("::1", "80")
	if err != nil || got != "[::1]:80" {
		t.Fatalf("ipv6 %q %v", got, err)
	}
	got, err = listenAddr("192.168.1.5", "0")
	if err != nil || got != "192.168.1.5:0" {
		t.Fatalf("lan %q %v", got, err)
	}
	if _, err := listenAddr("", "70000"); err == nil {
		t.Fatal("port 70000 accepted")
	}
	if _, err := listenAddr("", "abc"); err == nil {
		t.Fatal("port abc accepted")
	}
}

func TestListenFallsBackWhen7676Busy(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:7676")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ln, err := listenTCP("127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if port == "7676" {
		t.Fatal("busy 7676 was used")
	}
	if _, err := listenTCP("127.0.0.1", port); err == nil {
		t.Fatal("explicit port fell back instead of failing")
	}
}

func TestListenPublishesURLAndSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rt, err := runtimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeRunning(rt); ok {
		t.Fatal("missing url file counted as running")
	}
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	uiURL, port, err := loopbackURL(tcpLn.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uiURL, "http://127.0.0.1:") || strings.TrimPrefix(uiURL, "http://127.0.0.1:") != port {
		t.Fatalf("url %s port %s", uiURL, port)
	}
	if err := rt.ensure(); err != nil {
		t.Fatal(err)
	}
	if err := rt.write(uiURL, port); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	tcpSrv := &http.Server{Handler: mux}
	go tcpSrv.Serve(tcpLn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tcpSrv.Shutdown(ctx)
		rt.cleanup()
	})

	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err = http.Get(uiURL + "/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tcp status %d", resp.StatusCode)
	}
	var got string
	var ok bool
	for {
		got, ok = probeRunning(rt)
		if ok && got == uiURL {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe %q ok=%v", got, ok)
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, err := os.ReadFile(rt.PortFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != port {
		t.Fatalf("port file %q", body)
	}
}

func TestProbeIgnoresDeadSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rt, err := runtimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeRunning(rt); ok {
		t.Fatal("missing url file counted as running")
	}
	if err := rt.ensure(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	uiURL, port, err := loopbackURL(ln.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if err := rt.write(uiURL, port); err != nil {
		t.Fatal(err)
	}
	if _, ok := probeRunning(rt); ok {
		t.Fatal("dead url still counted as running")
	}
	if _, err := os.Stat(rt.URLFile); err != nil {
		t.Fatalf("url file: %v", err)
	}
	if _, err := os.Stat(rt.PortFile); err != nil {
		t.Fatalf("port file: %v", err)
	}
}
