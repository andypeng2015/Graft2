package redact

import (
	"strings"
	"testing"
)

func TestURLRedactsCredentialsAndSensitiveQueryValues(t *testing.T) {
	got := URL("https://alice:remote-secret@example.com/graft/alice/repo?token=remote-token&ok=yes&X-Amz-Signature=sig")
	for _, forbidden := range []string{"remote-secret", "remote-token", "sig"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("URL leaked %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"redacted@example.com", "token=redacted", "ok=yes", "X-Amz-Signature=redacted"} {
		if !strings.Contains(got, want) {
			t.Fatalf("URL missing %q: %s", want, got)
		}
	}
}

func TestTextRedactsAuthHeadersAssignmentsAndURLs(t *testing.T) {
	raw := `Get "https://alice:remote-secret@example.com/repo?token=remote-token": Authorization: Bearer header-secret standalone Bearer bearer-secret GRAFT_TOKEN=env-secret password: pass-secret {"signature":"sig-secret"}`
	got := Text(raw)
	for _, forbidden := range []string{"remote-secret", "remote-token", "header-secret", "bearer-secret", "env-secret", "pass-secret", "sig-secret"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Text leaked %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{`https://redacted@example.com/repo?token=redacted":`, "Authorization: redacted", "Bearer redacted", "GRAFT_TOKEN=redacted", "password: redacted", `"signature":"redacted"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("Text missing %q: %s", want, got)
		}
	}
}

func TestSensitiveKeyAndLooksSensitive(t *testing.T) {
	for _, key := range []string{"Authorization", "GRAFT_TOKEN", "signing_key", "session_cookie"} {
		if !SensitiveKey(key) {
			t.Fatalf("SensitiveKey(%q) = false", key)
		}
	}
	for _, value := range []string{"shell:echo secret-token", "Bearer abc", "credential helper"} {
		if !LooksSensitive(value) {
			t.Fatalf("LooksSensitive(%q) = false", value)
		}
	}
}
