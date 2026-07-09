package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestHandleCoordAddClaim_ForceTransfersClaim(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	c.Config.ConflictMode = "soft_block"

	ownerID, err := c.RegisterAgent(coord.AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent owner: %v", err)
	}
	forcerID, err := c.RegisterAgent(coord.AgentInfo{Name: "forcer", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent forcer: %v", err)
	}

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::Takeover:func Takeover():0",
		File:      "takeover.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(ownerID, req); err != nil {
		t.Fatalf("AcquireClaim owner: %v", err)
	}

	var out bytes.Buffer
	if err := handleCoordAddClaim(c, &out, forcerID, req, true); err != nil {
		t.Fatalf("handleCoordAddClaim: %v", err)
	}

	claim, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim: %v", err)
	}
	if claim == nil {
		t.Fatal("expected active claim after force transfer")
	}
	if claim.Agent != forcerID {
		t.Fatalf("claim.Agent = %q, want %q", claim.Agent, forcerID)
	}

	decisions, err := coord.ListDecisions(r.GraftDir, 10)
	if err != nil {
		t.Fatalf("coord.ListDecisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("expected recorded force decision")
	}
	if decisions[0].Outcome.Status != "force_transferred" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want force_transferred", decisions[0].Outcome.Status)
	}
	if !decisions[0].Outcome.ClaimTransferred {
		t.Fatal("expected force decision to mark claim transfer")
	}
	if decisions[0].Outcome.TransferredFromID != ownerID {
		t.Fatalf("TransferredFromID = %q, want %q", decisions[0].Outcome.TransferredFromID, ownerID)
	}
	if got := out.String(); got == "" {
		t.Fatal("expected force transfer output")
	}
}

func TestAddCmdAutoInitNoticeUsesCommandErrorWriter(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var errOut bytes.Buffer
	cmd := newAddCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"main.go"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), ".graft not found") {
		t.Fatalf("stderr = %q, want auto-init notice", errOut.String())
	}
	if !strings.Contains(errOut.String(), "Updated staging index") {
		t.Fatalf("stderr = %q, want add progress on command error writer", errOut.String())
	}
}

func TestAddCmdReadsPathsFromCommandInput(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile one.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile two.txt: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newAddCmd()
	cmd.SilenceUsage = true
	cmd.SetIn(strings.NewReader("one.txt\ntwo.txt\n\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--quiet", "--stdin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertStagedPaths(t, r, "one.txt", "two.txt")
}

func TestAddCmdReadsNullSeparatedPathsFromCommandInput(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile one.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "two.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile nested/two.txt: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newAddCmd()
	cmd.SilenceUsage = true
	cmd.SetIn(strings.NewReader("one.txt\x00nested/two.txt\x00"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--quiet", "--stdin0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertStagedPaths(t, r, "one.txt", "nested/two.txt")
}

func assertStagedPaths(t *testing.T, r *repo.Repo, paths ...string) {
	t.Helper()

	staging, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	for _, path := range paths {
		if _, ok := staging.Entries[path]; !ok {
			t.Fatalf("staging missing %s; entries: %+v", path, staging.Entries)
		}
	}
}
