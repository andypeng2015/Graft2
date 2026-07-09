package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestVersionCmdText(t *testing.T) {
	var out bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := strings.TrimSpace(out.String()), "graft "+version; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestVersionCmdJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVersionOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.Version != version {
		t.Fatalf("version = %q, want %q", result.Version, version)
	}
	if result.Commit == "" {
		t.Fatal("commit is empty")
	}
	if result.BuildTime == "" {
		t.Fatal("buildTime is empty")
	}
	if result.GoVersion == "" {
		t.Fatal("goVersion is empty")
	}
	if result.SupportedRepositoryFormat != repo.RepositoryFormatVersion {
		t.Fatalf("supportedRepositoryFormat = %d, want %d", result.SupportedRepositoryFormat, repo.RepositoryFormatVersion)
	}
	if result.SupportedRemoteProtocolVersion != remote.ProtocolVersion {
		t.Fatalf("supportedRemoteProtocolVersion = %q, want %q", result.SupportedRemoteProtocolVersion, remote.ProtocolVersion)
	}
}
