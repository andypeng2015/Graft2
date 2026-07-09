package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestVerifyIntegrityReportsUnreachableRef(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	bad := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	refPath := filepath.Join(r.GraftDir, "refs", "heads", "bad")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(refPath, []byte(string(bad)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(ref): %v", err)
	}

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !hasRepositoryDiagnostic(report, "ref_target_unreachable") {
		t.Fatalf("diagnostics missing ref_target_unreachable: %+v", report.Diagnostics)
	}
}

func TestVerifyIntegrityReportsMalformedGitMap(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, gitMapFile), []byte("not enough\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitmap): %v", err)
	}

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !hasRepositoryDiagnostic(report, "git_shadow_map_line_malformed") {
		t.Fatalf("diagnostics missing git_shadow_map_line_malformed: %+v", report.Diagnostics)
	}
}

func TestVerifyIntegrityAcceptsValidCoordFeedChain(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	first := writeCoordFeedEntry(t, r, "")
	second := writeCoordFeedEntry(t, r, string(first))
	if err := r.UpdateRefCAS(coordFeedHeadRef, second, ""); err != nil {
		t.Fatalf("UpdateRefCAS(coord feed head): %v", err)
	}

	report := r.VerifyIntegrity()
	if hasRepositoryDiagnosticPrefix(report, "coord_feed_") {
		t.Fatalf("unexpected coord feed diagnostic: %+v", report.Diagnostics)
	}
}

func TestVerifyIntegrityReportsMalformedCoordFeedEntry(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h, err := r.Store.WriteBlob(&object.Blob{Data: []byte("{not-json")})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	if err := r.UpdateRefCAS(coordFeedHeadRef, h, ""); err != nil {
		t.Fatalf("UpdateRefCAS(coord feed head): %v", err)
	}

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !hasRepositoryDiagnostic(report, "coord_feed_entry_malformed") {
		t.Fatalf("diagnostics missing coord_feed_entry_malformed: %+v", report.Diagnostics)
	}
}

func TestVerifyIntegrityReportsMissingCoordFeedParent(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	missing := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	head := writeCoordFeedEntry(t, r, string(missing))
	if err := r.UpdateRefCAS(coordFeedHeadRef, head, ""); err != nil {
		t.Fatalf("UpdateRefCAS(coord feed head): %v", err)
	}

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !hasRepositoryDiagnostic(report, "coord_feed_entry_unreachable") {
		t.Fatalf("diagnostics missing coord_feed_entry_unreachable: %+v", report.Diagnostics)
	}
}

func hasRepositoryDiagnostic(report *RepositoryIntegrityReport, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func hasRepositoryDiagnosticPrefix(report *RepositoryIntegrityReport, prefix string) bool {
	for _, d := range report.Diagnostics {
		if strings.HasPrefix(d.Code, prefix) {
			return true
		}
	}
	return false
}

func writeCoordFeedEntry(t *testing.T, r *Repo, parent string) object.Hash {
	t.Helper()

	data := []byte(fmt.Sprintf(
		`{"parent":%q,"event":{"event":"test","agent_id":"agent-a","agent_name":"agent-a"},"timestamp":"2026-07-09T09:45:00Z"}`,
		parent,
	))
	h, err := r.Store.WriteBlob(&object.Blob{Data: data})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	return h
}
