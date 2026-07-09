package coordd

import (
	"runtime"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestSandboxBackendHealthAutoFallsBackToHostDirectWithWarning(t *testing.T) {
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesRepo: true})
	report := SandboxBackendHealth(nil, &GuardConfig{PreferredBackend: "auto"}, requested)

	if !report.OK {
		t.Fatalf("OK = false, want true: %+v", report.Diagnostics)
	}
	if report.SelectedBackend != "host-direct" {
		t.Fatalf("SelectedBackend = %q, want host-direct", report.SelectedBackend)
	}
	if report.EffectiveProfile.Name != "host_direct" {
		t.Fatalf("EffectiveProfile.Name = %q, want host_direct", report.EffectiveProfile.Name)
	}
	if !sandboxReportHasDiagnostic(report, "warning", "coordd_backend_degraded") {
		t.Fatalf("diagnostics = %+v, want coordd_backend_degraded warning", report.Diagnostics)
	}
	if !sandboxReportHasCheck(report, "container", false) {
		t.Fatalf("checks = %+v, want unavailable container check", report.Checks)
	}
}

func TestSandboxBackendHealthExplicitContainerUnavailableFails(t *testing.T) {
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesRepo: true})
	report := SandboxBackendHealth(nil, &GuardConfig{PreferredBackend: "container"}, requested)

	if report.OK {
		t.Fatal("OK = true, want false")
	}
	if report.SelectedBackend != "" {
		t.Fatalf("SelectedBackend = %q, want empty", report.SelectedBackend)
	}
	if !sandboxReportHasDiagnostic(report, "error", "coordd_backend_unavailable") {
		t.Fatalf("diagnostics = %+v, want coordd_backend_unavailable error", report.Diagnostics)
	}
}

func TestSandboxBackendHealthAutoUsesConfiguredContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable requires POSIX shell")
	}

	dir := t.TempDir()
	writeFakeProbeExecutable(t, dir, "podman", "exit 0\n")
	t.Setenv("PATH", dir)

	requested := ResolveRuntimeProfile("read_only", ActionPolicyAction{})
	report := SandboxBackendHealth(&repo.Repo{RootDir: t.TempDir()}, &GuardConfig{
		PreferredBackend: "auto",
		ContainerRuntime: "auto",
		ContainerImage:   "example.invalid/graft-runtime:latest",
	}, requested)

	if !report.OK {
		t.Fatalf("OK = false, want true: %+v", report.Diagnostics)
	}
	if report.SelectedBackend != "container" {
		t.Fatalf("SelectedBackend = %q, want container", report.SelectedBackend)
	}
	check := sandboxReportCheck(report, "container")
	if check == nil {
		t.Fatalf("checks = %+v, want container check", report.Checks)
	}
	if check.Runtime != "podman" {
		t.Fatalf("container runtime = %q, want podman", check.Runtime)
	}
	if joined := strings.Join(report.Degradations, "\n"); joined != "" {
		t.Fatalf("degradations = %q, want none", joined)
	}
}

func sandboxReportHasDiagnostic(report SandboxBackendHealthReport, severity, code string) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Severity == severity && diagnostic.Code == code {
			return true
		}
	}
	return false
}

func sandboxReportHasCheck(report SandboxBackendHealthReport, backend string, available bool) bool {
	check := sandboxReportCheck(report, backend)
	return check != nil && check.Available == available
}

func sandboxReportCheck(report SandboxBackendHealthReport, backend string) *SandboxBackendCheck {
	for i := range report.Checks {
		if report.Checks[i].Backend == backend {
			return &report.Checks[i]
		}
	}
	return nil
}
