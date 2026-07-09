package coordd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSelectExecBackend_HostDirectDegradesProfile(t *testing.T) {
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true})
	cfg := &GuardConfig{PreferredBackend: "host-direct"}

	backend, effective, degradations, err := selectExecBackend(nil, cfg, requested)
	if err != nil {
		t.Fatalf("selectExecBackend: %v", err)
	}
	if backend != "host-direct" {
		t.Fatalf("backend = %q, want host-direct", backend)
	}
	if effective.Name != "host_direct" {
		t.Fatalf("effective.Name = %q, want host_direct", effective.Name)
	}
	if effective.Network != NetworkAmbient {
		t.Fatalf("effective.Network = %q, want %q", effective.Network, NetworkAmbient)
	}
	if len(degradations) == 0 {
		t.Fatal("expected degradations for host-direct backend")
	}
}

func TestBuildContainerInvocation_PodmanRepoWrite(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Selector: "shell:touch note.txt",
			Argv:     []string{"touch", "note.txt"},
		},
	}
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true})

	invocation, err := BuildContainerInvocation("podman", "docker.io/library/alpine:3.20", "/tmp/repo", "/workspace/subdir", input, "Allow", requested, requested, []string{"FOO=bar"})
	if err != nil {
		t.Fatalf("BuildContainerInvocation: %v", err)
	}
	if invocation.Runtime != "podman" {
		t.Fatalf("Runtime = %q, want podman", invocation.Runtime)
	}
	joined := strings.Join(invocation.Args, " ")
	if !strings.Contains(joined, "--network none") {
		t.Fatalf("expected network none in %q", joined)
	}
	if !strings.Contains(joined, "-v /tmp/repo:/workspace:rw") {
		t.Fatalf("expected rw workspace mount in %q", joined)
	}
	if !strings.Contains(joined, "--workdir /workspace/subdir") {
		t.Fatalf("expected workdir in %q", joined)
	}
	if !strings.Contains(joined, "--env FOO=bar") {
		t.Fatalf("expected extra env in %q", joined)
	}
}

func TestBuildContainerInvocation_RewritesRepoAbsoluteProgramPath(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Selector: "shell:/tmp/repo/.graft/hooks/pre-commit",
			Argv:     []string{"/tmp/repo/.graft/hooks/pre-commit", "arg1"},
		},
	}
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true})

	invocation, err := BuildContainerInvocation("podman", "docker.io/library/alpine:3.20", "/tmp/repo", "/workspace", input, "Allow", requested, requested, nil)
	if err != nil {
		t.Fatalf("BuildContainerInvocation: %v", err)
	}
	joined := strings.Join(invocation.Args, " ")
	if !strings.Contains(joined, "/workspace/.graft/hooks/pre-commit arg1") {
		t.Fatalf("expected program path rewrite in %q", joined)
	}
}

func TestProbeExecutableWithTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable requires POSIX shell")
	}

	dir := t.TempDir()
	writeFakeProbeExecutable(t, dir, "slow-probe", "exec /bin/sleep 2\n")
	t.Setenv("PATH", dir)
	restoreSandboxProbeTimeout(t, 50*time.Millisecond)

	start := time.Now()
	err := probeExecutableWithTimeout("slow-probe", "--version")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("probeExecutableWithTimeout succeeded, want timeout error")
	}
	if !strings.Contains(err.Error(), "slow-probe probe timed out") {
		t.Fatalf("error = %q, want timeout message", err.Error())
	}
	if elapsed > time.Second {
		t.Fatalf("probe took %s, want bounded timeout", elapsed)
	}
}

func TestSelectExecBackendAutoReportsSandboxProbeDegradations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable requires POSIX shell")
	}

	dir := t.TempDir()
	writeFakeProbeExecutable(t, dir, "podman", "exec /bin/sleep 2\n")
	t.Setenv("PATH", dir)
	restoreSandboxProbeTimeout(t, 50*time.Millisecond)

	cfg := &GuardConfig{
		PreferredBackend: "auto",
		ContainerRuntime: "auto",
		ContainerImage:   "example.invalid/graft-runtime:latest",
	}
	requested := RuntimeProfile{
		Name:    "probe_test",
		Network: NetworkAllow,
	}

	backend, _, degradations, err := selectExecBackendForPreference(nil, cfg, requested, "auto")
	if err != nil {
		t.Fatalf("selectExecBackendForPreference: %v", err)
	}
	if backend != "host-direct" {
		t.Fatalf("backend = %q, want host-direct", backend)
	}
	joined := strings.Join(degradations, "\n")
	for _, want := range []string{
		"container backend unavailable:",
		"podman probe timed out",
		"docker not found",
		"host-bwrap backend unavailable:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("degradations = %q, want to contain %q", joined, want)
		}
	}
}

func restoreSandboxProbeTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	old := sandboxProbeTimeout
	sandboxProbeTimeout = timeout
	t.Cleanup(func() {
		sandboxProbeTimeout = old
	})
}

func writeFakeProbeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}
