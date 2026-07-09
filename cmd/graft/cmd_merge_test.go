package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/odvcencio/graft/pkg/merge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func TestMergeReportToJSONIncludesConfidence(t *testing.T) {
	report := &repo.MergeReport{
		Files: []repo.FileMergeReport{{
			Path:       "main.go",
			Status:     "clean",
			Confidence: merge.MergeConfidenceStructuralClean,
		}},
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := mergeReportToJSON(cmd, report, "preview", "feature", "main"); err != nil {
		t.Fatalf("mergeReportToJSON: %v", err)
	}

	var result JSONMergeOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].Confidence != merge.MergeConfidenceStructuralClean {
		t.Fatalf("confidence = %q, want %q", result.Files[0].Confidence, merge.MergeConfidenceStructuralClean)
	}
}
