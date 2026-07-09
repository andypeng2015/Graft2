package main

import (
	"strings"
	"testing"
)

func TestParseTiming(t *testing.T) {
	raw := `Command being timed: "graft add ."
User time (seconds): 1.25
System time (seconds): 0.50
Elapsed (wall clock) time (h:mm:ss or m:ss): 1:02.30
Maximum resident set size (kbytes): 123456
`
	metric, err := parseTiming("/tmp/add.time.txt", strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parseTiming: %v", err)
	}
	if metric.Name != "add" {
		t.Fatalf("Name = %q, want add", metric.Name)
	}
	if metric.UserSeconds != 1.25 || metric.SystemSeconds != 0.50 || metric.ElapsedSeconds != 62.30 || metric.MaxRSSKB != 123456 {
		t.Fatalf("metric = %+v", metric)
	}
}

func TestParseElapsed(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"0:01.50", 1.5},
		{"1:02.50", 62.5},
		{"2:01:02", 7262},
	} {
		if got := parseElapsed(tc.in); got != tc.want {
			t.Fatalf("parseElapsed(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
