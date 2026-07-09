package coordd

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
)

type SandboxBackendHealthReport struct {
	OK                       bool                       `json:"ok"`
	PreferredBackend         string                     `json:"preferred_backend"`
	ContainerRuntime         string                     `json:"container_runtime"`
	ContainerImageConfigured bool                       `json:"container_image_configured"`
	RequestedProfile         RuntimeProfile             `json:"requested_profile"`
	SelectedBackend          string                     `json:"selected_backend,omitempty"`
	EffectiveProfile         RuntimeProfile             `json:"effective_profile,omitempty"`
	Degradations             []string                   `json:"degradations,omitempty"`
	Checks                   []SandboxBackendCheck      `json:"checks"`
	Diagnostics              []SandboxBackendDiagnostic `json:"diagnostics,omitempty"`
}

type SandboxBackendCheck struct {
	Backend      string   `json:"backend"`
	Status       string   `json:"status"`
	Available    bool     `json:"available"`
	Selected     bool     `json:"selected,omitempty"`
	Runtime      string   `json:"runtime,omitempty"`
	Error        string   `json:"error,omitempty"`
	Degradations []string `json:"degradations,omitempty"`
	Repair       string   `json:"repair,omitempty"`
}

type SandboxBackendDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Repair   string `json:"repair,omitempty"`
}

func SandboxBackendHealth(r *repo.Repo, cfg *GuardConfig, requested RuntimeProfile) SandboxBackendHealthReport {
	normalized := normalizeGuardConfig(cfg)
	report := SandboxBackendHealthReport{
		OK:                       true,
		PreferredBackend:         normalized.PreferredBackend,
		ContainerRuntime:         normalized.ContainerRuntime,
		ContainerImageConfigured: strings.TrimSpace(normalized.ContainerImage) != "",
		RequestedProfile:         requested,
	}

	checksByName := map[string]*SandboxBackendCheck{}
	for _, check := range []SandboxBackendCheck{
		checkContainerBackend(r, normalized, requested),
		checkBwrapBackend(r, requested),
		checkHostDirectBackend(requested),
	} {
		check := check
		checksByName[check.Backend] = &check
		report.Checks = append(report.Checks, check)
	}

	selected, effective, degradations, err := selectBackendFromHealth(normalized.PreferredBackend, checksByName, requested)
	if err != nil {
		report.OK = false
		report.Diagnostics = append(report.Diagnostics, SandboxBackendDiagnostic{
			Severity: "error",
			Code:     "coordd_backend_unavailable",
			Message:  err.Error(),
			Repair:   backendRepair(normalized.PreferredBackend),
		})
		return report
	}

	report.SelectedBackend = selected
	report.EffectiveProfile = effective
	report.Degradations = append([]string(nil), degradations...)
	for i := range report.Checks {
		if report.Checks[i].Backend == selected {
			report.Checks[i].Selected = true
			break
		}
	}
	if len(degradations) > 0 {
		report.Diagnostics = append(report.Diagnostics, SandboxBackendDiagnostic{
			Severity: "warning",
			Code:     "coordd_backend_degraded",
			Message:  fmt.Sprintf("coordd selected %s for profile %s with reduced isolation", selected, requested.Name),
			Repair:   "install/configure bubblewrap or a container runtime and image, or explicitly accept host-direct for this repository",
		})
	}
	return report
}

func normalizeGuardConfig(cfg *GuardConfig) *GuardConfig {
	normalized := &GuardConfig{
		Mode:               "advisory",
		AllowedActions:     nil,
		RequireActiveAgent: false,
		PreferredBackend:   "auto",
		ContainerRuntime:   "auto",
		ContainerImage:     "",
	}
	if cfg == nil {
		return normalized
	}
	*normalized = *cfg
	normalized.AllowedActions = append([]string(nil), cfg.AllowedActions...)
	if strings.TrimSpace(normalized.Mode) == "" {
		normalized.Mode = "advisory"
	}
	if strings.TrimSpace(normalized.PreferredBackend) == "" {
		normalized.PreferredBackend = "auto"
	}
	if strings.TrimSpace(normalized.ContainerRuntime) == "" {
		normalized.ContainerRuntime = "auto"
	}
	normalized.PreferredBackend = strings.TrimSpace(normalized.PreferredBackend)
	normalized.ContainerRuntime = strings.TrimSpace(normalized.ContainerRuntime)
	normalized.ContainerImage = strings.TrimSpace(normalized.ContainerImage)
	return normalized
}

func checkContainerBackend(r *repo.Repo, cfg *GuardConfig, requested RuntimeProfile) SandboxBackendCheck {
	check := SandboxBackendCheck{
		Backend: "container",
		Status:  "unavailable",
		Repair:  backendRepair("container"),
	}
	if cfg == nil || strings.TrimSpace(cfg.ContainerImage) == "" {
		check.Error = "container image is not configured"
		return check
	}
	runtimeName, err := resolveContainerRuntime(cfg)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	check.Runtime = runtimeName
	if requested.FilesystemScope == FilesystemScopeRepoRO || requested.FilesystemScope == FilesystemScopeRepoRW {
		if r == nil || strings.TrimSpace(r.RootDir) == "" {
			check.Error = "repo filesystem scopes require an open repo root"
			return check
		}
	}
	check.Available = true
	check.Status = "available"
	check.Repair = ""
	return check
}

func checkBwrapBackend(r *repo.Repo, requested RuntimeProfile) SandboxBackendCheck {
	check := SandboxBackendCheck{
		Backend: "host-bwrap",
		Status:  "unavailable",
		Repair:  backendRepair("host-bwrap"),
	}
	if err := bwrapAvailability(r, requested); err != nil {
		check.Error = err.Error()
		return check
	}
	check.Available = true
	check.Status = "available"
	check.Repair = ""
	return check
}

func checkHostDirectBackend(requested RuntimeProfile) SandboxBackendCheck {
	_, degradations := directEffectiveProfile(requested)
	check := SandboxBackendCheck{
		Backend:      "host-direct",
		Status:       "available",
		Available:    true,
		Degradations: append([]string(nil), degradations...),
	}
	if len(degradations) > 0 {
		check.Status = "degraded"
		check.Repair = "use host-bwrap or container when filesystem, network, or delete isolation must be enforced"
	}
	return check
}

func selectBackendFromHealth(preference string, checks map[string]*SandboxBackendCheck, requested RuntimeProfile) (string, RuntimeProfile, []string, error) {
	preference = strings.TrimSpace(preference)
	if preference == "" {
		preference = "auto"
	}
	switch preference {
	case "auto":
		for _, backend := range []string{"container", "host-bwrap"} {
			if check := checks[backend]; check != nil && check.Available {
				return backend, requested, nil, nil
			}
		}
		effective, degradations := directEffectiveProfile(requested)
		return "host-direct", effective, degradations, nil
	case "container", "host-bwrap":
		check := checks[preference]
		if check == nil || !check.Available {
			reason := "unavailable"
			if check != nil && strings.TrimSpace(check.Error) != "" {
				reason = check.Error
			}
			return "", RuntimeProfile{}, nil, fmt.Errorf("coordd %s backend unavailable for requested profile %s: %s", preference, requested.Name, reason)
		}
		return preference, requested, nil, nil
	case "host-direct":
		effective, degradations := directEffectiveProfile(requested)
		return "host-direct", effective, degradations, nil
	default:
		return "", RuntimeProfile{}, nil, fmt.Errorf("unknown coordd backend preference %q", preference)
	}
}

func backendRepair(backend string) string {
	switch backend {
	case "container":
		return "configure a container runtime and image with `graft coordd guard runtime <auto|podman|docker>` and `graft coordd guard image <image-ref>`"
	case "host-bwrap":
		return "install bubblewrap or switch to `graft coordd guard backend auto`"
	case "host-direct":
		return "host-direct is always available but does not enforce sandbox isolation"
	default:
		return "choose `graft coordd guard backend auto` or configure an available backend"
	}
}
