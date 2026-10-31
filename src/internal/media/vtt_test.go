package media

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteShiftedVTTMovesCues(t *testing.T) {
	in := "WEBVTT\n\n00:12.000 --> 00:15.000\nhello\n\n01:00:00.500 --> 01:00:02.000 align:start position:10%\nbye\n"
	var out bytes.Buffer
	if err := WriteShiftedVTT(&out, strings.NewReader(in), 10); err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\n00:00:02.000 --> 00:00:05.000\nhello\n\n00:59:50.500 --> 00:59:52.000 align:start position:10%\nbye\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

func TestWriteShiftedVTTDropsConsumedCues(t *testing.T) {
	in := "WEBVTT\n\n00:01.000 --> 00:02.000\nearly\n\n00:12.000 --> 00:14.000\nlate\n"
	var out bytes.Buffer
	if err := WriteShiftedVTT(&out, strings.NewReader(in), 10); err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\n00:00:02.000 --> 00:00:04.000\nlate\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

func TestWriteShiftedVTTClampsStart(t *testing.T) {
	in := "WEBVTT\n\n00:08.000 --> 00:12.000\nstraddle\n"
	var out bytes.Buffer
	if err := WriteShiftedVTT(&out, strings.NewReader(in), 10); err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nstraddle\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

func TestWriteShiftedVTTKeepsNotesAndIdentifiers(t *testing.T) {
	in := "WEBVTT\n\nNOTE do not retime\n\ncue-1\n00:12.000 --> 00:14.000\ntext\n"
	var out bytes.Buffer
	if err := WriteShiftedVTT(&out, strings.NewReader(in), 1); err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\nNOTE do not retime\n\ncue-1\n00:00:11.000 --> 00:00:13.000\ntext\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

func TestParseVTTTimestamp(t *testing.T) {
	cases := map[string]float64{
		"00:12.500":    12.5,
		"01:02:03.250": 3723.25,
		"1:02:03,250":  3723.25,
	}
	for in, want := range cases {
		got, err := parseVTTTimestamp(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v want %v", in, got, want)
		}
	}
	if _, err := parseVTTTimestamp("nonsense"); err == nil {
		t.Fatal("expected error for nonsense timestamp")
	}
}

func TestFormatVTTTimestamp(t *testing.T) {
	cases := map[float64]string{
		0:       "00:00:00.000",
		3723.25: "01:02:03.250",
		12.5:    "00:00:12.500",
		-1:      "00:00:00.000",
	}
	for in, want := range cases {
		if got := formatVTTTimestamp(in); got != want {
			t.Fatalf("%v: got %q want %q", in, got, want)
		}
	}
}
