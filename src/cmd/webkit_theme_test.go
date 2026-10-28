package main

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
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

func TestFlatpakProgramName(t *testing.T) {
	if got := flatpakProgramName("  eu.alyshmahell.ServeMedia  "); got != "eu.alyshmahell.ServeMedia" {
		t.Fatalf("flatpak id %q", got)
	}
	for _, id := range []string{"", "servemedia", "not an id", ":1.5", ".eu.App", "eu..App", "1.App"} {
		if got := flatpakProgramName(id); got != "" {
			t.Fatalf("accepted %q as %q", id, got)
		}
	}
}

func TestApplicationIDValidMatchesGLib(t *testing.T) {
	handle, err := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_NOW)
	if err != nil {
		t.Skip(err)
	}
	var isValid func(string) int32
	purego.RegisterLibFunc(&isValid, handle, "g_application_id_is_valid")
	samples := []string{
		"eu.alyshmahell.ServeMedia",
		"servemedia",
		"org.webkit.app-abc",
		"",
		".eu.Foo",
		"eu.",
		"eu..Foo",
		"1eu.Foo",
		"eu.1Foo",
		"eu.Foo-bar",
		"eu.Foo_bar",
		"-eu.Foo",
		"eu.-Foo",
		"A.B",
		":1.50",
		strings.Repeat("a", 256),
		"a." + strings.Repeat("b", 253),
	}
	for _, sample := range samples {
		got := applicationIDValid(sample)
		want := isValid(sample) != 0
		if got != want {
			t.Fatalf("%q: got %v, glib %v", sample, got, want)
		}
	}
}

func TestSetFlatpakProgramName(t *testing.T) {
	glib, err := openShared("libglib-2.0.so.0")
	if err != nil {
		t.Skip(err)
	}
	var getPrgname func() uintptr
	var setPrgname func(string)
	if err := registerLibFunc(&getPrgname, glib, "g_get_prgname"); err != nil {
		t.Fatal(err)
	}
	if err := registerLibFunc(&setPrgname, glib, "g_set_prgname"); err != nil {
		t.Fatal(err)
	}
	prev := goString(getPrgname())
	t.Cleanup(func() {
		if prev != "" {
			setPrgname(prev)
		}
	})
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	if err := setFlatpakProgramName(glib); err != nil {
		t.Fatal(err)
	}
	if got := goString(getPrgname()); got != "eu.alyshmahell.ServeMedia" {
		t.Fatalf("prgname %q", got)
	}
	t.Setenv("FLATPAK_ID", "servemedia")
	if err := setFlatpakProgramName(glib); err != nil {
		t.Fatal(err)
	}
	if got := goString(getPrgname()); got != "eu.alyshmahell.ServeMedia" {
		t.Fatalf("invalid id replaced prgname with %q", got)
	}
}

func goString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var buf []byte
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			return string(buf)
		}
		buf = append(buf, b)
		ptr++
	}
}
