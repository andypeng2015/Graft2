package main

import (
	"strings"
	"testing"
)

func TestParseBenchmarkOutput(t *testing.T) {
	name, nsOp, bytesOp, allocs, ok := parseBenchmarkOutput("BenchmarkThing-24 \t       3\t       120.5 ns/op\t      64 B/op\t       2 allocs/op\n")
	if !ok {
		t.Fatal("parseBenchmarkOutput ok = false")
	}
	if name != "BenchmarkThing-24" || nsOp != 120.5 || bytesOp != 64 || allocs != 2 {
		t.Fatalf("parsed = %q %.1f %.1f %.1f", name, nsOp, bytesOp, allocs)
	}
}

func TestReadBenchmarkStreamAndCompare(t *testing.T) {
	baseJSON := `{"Action":"output","Package":"pkg/a","Output":"BenchmarkThing \t 1\t100 ns/op\t10 B/op\t1 allocs/op\n"}
{"Action":"output","Package":"pkg/a","Output":"BenchmarkThing \t 1\t110 ns/op\t12 B/op\t1 allocs/op\n"}
`
	candidateJSON := `{"Action":"output","Package":"pkg/a","Output":"BenchmarkThing \t 1\t130 ns/op\t12 B/op\t2 allocs/op\n"}
`
	base := benchmarkSet{}
	if err := readBenchmarkStream(strings.NewReader(baseJSON), base); err != nil {
		t.Fatalf("read base: %v", err)
	}
	candidate := benchmarkSet{}
	if err := readBenchmarkStream(strings.NewReader(candidateJSON), candidate); err != nil {
		t.Fatalf("read candidate: %v", err)
	}

	regressions, compared := compareBenchmarkSets(summarize(base), summarize(candidate), 0.20)
	if compared != 1 {
		t.Fatalf("compared = %d, want 1", compared)
	}
	if len(regressions) != 2 {
		t.Fatalf("regressions = %+v, want ns/op and allocs/op", regressions)
	}
	if regressions[0].Metric != "ns/op" {
		t.Fatalf("first regression = %+v, want ns/op", regressions[0])
	}
}

func TestCompareBenchmarkSetsAllowsThreshold(t *testing.T) {
	base := map[string]benchmarkSummary{
		"pkg/a/BenchmarkThing": {NsOp: 100, BytesOp: 10, Allocs: 1},
	}
	candidate := map[string]benchmarkSummary{
		"pkg/a/BenchmarkThing": {NsOp: 119, BytesOp: 11, Allocs: 1},
	}
	regressions, compared := compareBenchmarkSets(base, candidate, 0.20)
	if compared != 1 {
		t.Fatalf("compared = %d, want 1", compared)
	}
	if len(regressions) != 0 {
		t.Fatalf("regressions = %+v, want none", regressions)
	}
}
