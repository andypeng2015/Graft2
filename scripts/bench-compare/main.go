package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type benchmarkSet map[string]*benchmarkSamples

type benchmarkSamples struct {
	Package string
	Name    string
	NsOp    []float64
	BytesOp []float64
	Allocs  []float64
}

type benchmarkSummary struct {
	Package string
	Name    string
	NsOp    float64
	BytesOp float64
	Allocs  float64
}

type regression struct {
	Key       string
	Metric    string
	Base      float64
	Candidate float64
	Delta     float64
}

func main() {
	basePath := flag.String("base", "", "baseline benchmark JSON file or directory")
	candidatePath := flag.String("candidate", "", "candidate benchmark JSON file or directory")
	maxRegression := flag.Float64("max-regression", 0.20, "allowed fractional regression before failure")
	flag.Parse()

	if strings.TrimSpace(*basePath) == "" || strings.TrimSpace(*candidatePath) == "" {
		fmt.Fprintln(os.Stderr, "usage: bench-compare -base <file-or-dir> -candidate <file-or-dir> [-max-regression 0.20]")
		os.Exit(2)
	}
	if *maxRegression < 0 || math.IsNaN(*maxRegression) || math.IsInf(*maxRegression, 0) {
		fmt.Fprintln(os.Stderr, "-max-regression must be a finite value >= 0")
		os.Exit(2)
	}

	base, err := readBenchmarkPath(*basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read baseline: %v\n", err)
		os.Exit(1)
	}
	candidate, err := readBenchmarkPath(*candidatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read candidate: %v\n", err)
		os.Exit(1)
	}

	regressions, compared := compareBenchmarkSets(summarize(base), summarize(candidate), *maxRegression)
	if len(regressions) == 0 {
		fmt.Printf("ok: compared %d benchmark(s); max regression %.2f%%\n", compared, *maxRegression*100)
		return
	}
	for _, r := range regressions {
		fmt.Printf("regression: %s %s base=%.2f candidate=%.2f delta=%.2f%%\n",
			r.Key, r.Metric, r.Base, r.Candidate, r.Delta*100)
	}
	os.Exit(1)
}

func readBenchmarkPath(path string) (benchmarkSet, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	out := benchmarkSet{}
	if !info.IsDir() {
		if err := readBenchmarkFile(path, out); err != nil {
			return nil, err
		}
		return out, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	for _, match := range matches {
		if filepath.Base(match) == "metadata.json" {
			continue
		}
		if err := readBenchmarkFile(match, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func readBenchmarkFile(path string, out benchmarkSet) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return readBenchmarkStream(f, out)
}

func readBenchmarkStream(r io.Reader, out benchmarkSet) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Output  string `json:"Output"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("decode benchmark event: %w", err)
		}
		if event.Action != "output" {
			continue
		}
		name, nsOp, bytesOp, allocs, ok := parseBenchmarkOutput(event.Output)
		if !ok {
			continue
		}
		key := event.Package + "/" + name
		s := out[key]
		if s == nil {
			s = &benchmarkSamples{Package: event.Package, Name: name}
			out[key] = s
		}
		s.NsOp = append(s.NsOp, nsOp)
		if bytesOp >= 0 {
			s.BytesOp = append(s.BytesOp, bytesOp)
		}
		if allocs >= 0 {
			s.Allocs = append(s.Allocs, allocs)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func parseBenchmarkOutput(line string) (name string, nsOp, bytesOp, allocs float64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
		return "", 0, -1, -1, false
	}
	bytesOp = -1
	allocs = -1
	for i, field := range fields {
		if i == 0 {
			continue
		}
		switch field {
		case "ns/op":
			value, err := strconv.ParseFloat(fields[i-1], 64)
			if err != nil {
				return "", 0, -1, -1, false
			}
			nsOp = value
		case "B/op":
			value, err := strconv.ParseFloat(fields[i-1], 64)
			if err != nil {
				return "", 0, -1, -1, false
			}
			bytesOp = value
		case "allocs/op":
			value, err := strconv.ParseFloat(fields[i-1], 64)
			if err != nil {
				return "", 0, -1, -1, false
			}
			allocs = value
		}
	}
	if nsOp <= 0 {
		return "", 0, -1, -1, false
	}
	return fields[0], nsOp, bytesOp, allocs, true
}

func summarize(samples benchmarkSet) map[string]benchmarkSummary {
	out := make(map[string]benchmarkSummary, len(samples))
	for key, s := range samples {
		out[key] = benchmarkSummary{
			Package: s.Package,
			Name:    s.Name,
			NsOp:    median(s.NsOp),
			BytesOp: median(s.BytesOp),
			Allocs:  median(s.Allocs),
		}
	}
	return out
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return -1
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func compareBenchmarkSets(base, candidate map[string]benchmarkSummary, maxRegression float64) ([]regression, int) {
	var out []regression
	compared := 0
	keys := make([]string, 0, len(candidate))
	for key := range candidate {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		baseSummary, ok := base[key]
		if !ok {
			continue
		}
		candidateSummary := candidate[key]
		compared++
		out = append(out, compareMetric(key, "ns/op", baseSummary.NsOp, candidateSummary.NsOp, maxRegression)...)
		out = append(out, compareMetric(key, "B/op", baseSummary.BytesOp, candidateSummary.BytesOp, maxRegression)...)
		out = append(out, compareMetric(key, "allocs/op", baseSummary.Allocs, candidateSummary.Allocs, maxRegression)...)
	}
	return out, compared
}

func compareMetric(key, metric string, base, candidate, maxRegression float64) []regression {
	if base <= 0 || candidate < 0 {
		return nil
	}
	delta := (candidate - base) / base
	if delta <= maxRegression {
		return nil
	}
	return []regression{{
		Key:       key,
		Metric:    metric,
		Base:      base,
		Candidate: candidate,
		Delta:     delta,
	}}
}
