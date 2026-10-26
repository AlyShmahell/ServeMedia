package main

import (
	"strings"
	"testing"
)

func TestDarkHeaderCSS(t *testing.T) {
	for _, css := range []string{darkHeaderCSS(), darkHeaderCSSGTK4()} {
		if !strings.Contains(css, "headerbar") {
			t.Fatalf("css missing headerbar: %s", css)
		}
		if !strings.Contains(css, "background-color: #0f1419") {
			t.Fatalf("css missing dark background: %s", css)
		}
		if !strings.Contains(css, "color: #ffffff") {
			t.Fatalf("css missing white text: %s", css)
		}
	}
	if !strings.Contains(darkHeaderCSSGTK4(), "windowcontrols") {
		t.Fatalf("gtk4 css missing windowcontrols: %s", darkHeaderCSSGTK4())
	}
}

func TestWebKitSONAMEsPreferHighest(t *testing.T) {
	listing := `
	libwebkit2gtk-4.0.so.0 (libc6,x86-64) => /lib64/libwebkit2gtk-4.0.so.0
	libwebkit2gtk-4.2.so.0 (libc6,x86-64) => /lib64/libwebkit2gtk-4.2.so.0
	libwebkit2gtk-4.1.so.0 (libc6,x86-64) => /lib64/libwebkit2gtk-4.1.so.0
	libwebkit2gtk-4.1.so.0 (libc6,x86-64) => /usr/lib64/libwebkit2gtk-4.1.so.0
	libwebkitgtk-6.0.so.0 (libc6,x86-64) => /lib64/libwebkitgtk-6.0.so.0
	libwebkitgtk-6.0.so.4 (libc6,x86-64) => /lib64/libwebkitgtk-6.0.so.4
	libwebkit2gtk-4.1.so.0 (libc6) => /lib/libwebkit2gtk-4.1.so.0
	libwebkit2gtk-4.0.so.0 (libc6,AArch64) => /usr/lib/libwebkit2gtk-4.0.so.0
	libgtk-3.so.0 (libc6,x86-64) => /lib64/libgtk-3.so.0
`
	got := webkitSONAMEs(listing, "x86-64")
	want := []string{
		"libwebkitgtk-6.0.so.4",
		"libwebkitgtk-6.0.so.0",
		"libwebkit2gtk-4.2.so.0",
		"libwebkit2gtk-4.1.so.0",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sonames %v", got)
	}
	arm := webkitSONAMEs(listing, "AArch64")
	if len(arm) != 0 {
		t.Fatalf("arm sonames %v", arm)
	}
	if got := webkitSONAMEs("libwebkitgtk-6.0.so.0 (libc6,x86-64) => /lib64/libwebkitgtk-6.0.so.0\n", "x86-64"); len(got) != 1 || got[0] != "libwebkitgtk-6.0.so.0" {
		t.Fatalf("webkit 6 sonames %v", got)
	}
}
