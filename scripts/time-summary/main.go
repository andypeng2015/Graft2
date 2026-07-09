package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type timingReport struct {
	SchemaVersion int            `json:"schemaVersion"`
	Timings       []timingMetric `json:"timings"`
}

type timingMetric struct {
	Name           string  `json:"name"`
	Path           string  `json:"path"`
	UserSeconds    float64 `json:"userSeconds,omitempty"`
	SystemSeconds  float64 `json:"systemSeconds,omitempty"`
	ElapsedSeconds float64 `json:"elapsedSeconds,omitempty"`
	MaxRSSKB       int64   `json:"maxRssKb,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: time-summary <time-output>...")
		os.Exit(2)
	}
	report := timingReport{SchemaVersion: 1}
	for _, path := range os.Args[1:] {
		metric, err := readTimingFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			os.Exit(1)
		}
		report.Timings = append(report.Timings, metric)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "write summary: %v\n", err)
		os.Exit(1)
	}
}

func readTimingFile(path string) (timingMetric, error) {
	f, err := os.Open(path)
	if err != nil {
		return timingMetric{}, err
	}
	defer f.Close()
	metric, err := parseTiming(path, f)
	if err != nil {
		return timingMetric{}, err
	}
	return metric, nil
}

func parseTiming(path string, r io.Reader) (timingMetric, error) {
	metric := timingMetric{
		Name: timingName(path),
		Path: path,
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "User time (seconds):"):
			metric.UserSeconds = parseFloat(strings.TrimPrefix(line, "User time (seconds):"))
		case strings.HasPrefix(line, "System time (seconds):"):
			metric.SystemSeconds = parseFloat(strings.TrimPrefix(line, "System time (seconds):"))
		case strings.HasPrefix(line, "Elapsed (wall clock) time (h:mm:ss or m:ss):"):
			metric.ElapsedSeconds = parseElapsed(strings.TrimPrefix(line, "Elapsed (wall clock) time (h:mm:ss or m:ss):"))
		case strings.HasPrefix(line, "Maximum resident set size (kbytes):"):
			metric.MaxRSSKB = parseInt(strings.TrimPrefix(line, "Maximum resident set size (kbytes):"))
		}
	}
	if err := scanner.Err(); err != nil {
		return timingMetric{}, err
	}
	return metric, nil
}

func timingName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".txt")
	base = strings.TrimSuffix(base, ".time")
	return base
}

func parseFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

func parseInt(raw string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return value
}

func parseElapsed(raw string) float64 {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	total := 0.0
	for _, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0
		}
		total = total*60 + value
	}
	return total
}
