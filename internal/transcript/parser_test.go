package transcript

import (
	"strings"
	"testing"
)

func TestParseSRT(t *testing.T) {
	input := `1
00:00:01,500 --> 00:00:03,000
Hello <b>world</b> &amp; friends

2
00:01:00,000 --> 00:01:02,250
Second    line`

	lines, err := ParseSRT(input)
	if err != nil {
		t.Fatalf("ParseSRT returned error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0].StartSeconds != 1.5 || lines[0].EndSeconds != 3.0 || lines[0].Text != "Hello world & friends" {
		t.Fatalf("first line = %#v", lines[0])
	}
	if got := FormatLines(lines); !strings.Contains(got, "[1.5s -> 3.0s] Hello world & friends") {
		t.Fatalf("formatted = %q", got)
	}
}

func TestParseVTT(t *testing.T) {
	input := `WEBVTT

cue-1
00:00:10.000 --> 00:00:12.500 align:start
Line <i>one</i>

00:13.000 --> 00:14.000
Line&nbsp;two

00:15.000 --> 00:16.000
&lt;b&gt;escaped&lt;/b&gt; &amp; entity`

	lines, err := ParseVTT(input)
	if err != nil {
		t.Fatalf("ParseVTT returned error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0].StartSeconds != 10 || lines[0].EndSeconds != 12.5 || lines[0].Text != "Line one" {
		t.Fatalf("first line = %#v", lines[0])
	}
	if lines[1].StartSeconds != 13 || lines[1].EndSeconds != 14 || lines[1].Text != "Line two" {
		t.Fatalf("second line = %#v", lines[1])
	}
	if lines[2].Text != "escaped & entity" {
		t.Fatalf("third line = %#v", lines[2])
	}
}

func TestParseMalformedCuesAndEmptyOutput(t *testing.T) {
	lines, err := ParseSRT("not a cue\n\n00:bad --> also bad\ntext\n\n --> 00:00:01,000\nmissing start")
	if err != nil {
		t.Fatalf("ParseSRT returned error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %#v", lines)
	}

	lines, err = ParseVTT("WEBVTT\n\nNOTE skip me")
	if err != nil {
		t.Fatalf("ParseVTT returned error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %#v", lines)
	}
}
