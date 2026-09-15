package main

import (
	"syscall"
	"testing"
)

func TestPublicURL(t *testing.T) {
	if got := publicURL(":7676"); got != "http://127.0.0.1:7676" {
		t.Fatalf("bare host: %q", got)
	}
	if got := publicURL("0.0.0.0:7676"); got != "http://127.0.0.1:7676" {
		t.Fatalf("wildcard: %q", got)
	}
	if got := publicURL("127.0.0.1:9000"); got != "http://127.0.0.1:9000" {
		t.Fatalf("explicit: %q", got)
	}
}

func TestIsAddrInUse(t *testing.T) {
	if !isAddrInUse(syscall.EADDRINUSE) {
		t.Fatal("EADDRINUSE")
	}
	if isAddrInUse(syscall.ECONNREFUSED) {
		t.Fatal("ECONNREFUSED should not match")
	}
}