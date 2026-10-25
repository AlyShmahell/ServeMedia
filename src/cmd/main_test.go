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
		t.Fatal("socket answered before listen")
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
	unixLn, err := net.Listen("unix", rt.Sock)
	if err != nil {
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
	unixSrv := &http.Server{Handler: mux}
	go tcpSrv.Serve(tcpLn)
	go unixSrv.Serve(unixLn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tcpSrv.Shutdown(ctx)
		_ = unixSrv.Shutdown(ctx)
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
	if _, err := net.Listen("unix", rt.Sock); err == nil {
		t.Fatal("second process could listen on the live socket")
	}
}

func TestProbeIgnoresDeadSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rt, err := runtimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.ensure(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", rt.Sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if _, ok := probeRunning(rt); ok {
		t.Fatal("closed socket still counted as running")
	}
	discardStaleSocket(rt)
	if _, err := os.Stat(rt.Sock); !os.IsNotExist(err) {
		t.Fatalf("stale socket: %v", err)
	}
}
