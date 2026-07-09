package redact

import (
	"net/url"
	"regexp"
	"strings"
)

const Replacement = "redacted"

var (
	urlPattern       = regexp.MustCompile(`(?i)\b(?:https?|ssh|git|file)://[^\s"'<>]+`)
	authValuePattern = regexp.MustCompile(
		`(?i)\b((?:Bearer|Basic)\s+)[A-Za-z0-9._~+/=-]+`,
	)
	sensitiveAssignmentPattern = regexp.MustCompile(
		`(?i)(^|[[:space:],{\[])(["']?\b[a-z0-9_.-]*(?:token|password|secret|credential|authorization|cookie|signature|private[_-]?key|signing[_-]?key)[a-z0-9_.-]*\b["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}\]]+)`,
	)
)

// URL removes credentials and sensitive query values from a URL-like string.
func URL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return redactAssignments(raw)
	}
	if u.User != nil {
		u.User = url.User(Replacement)
	}
	q := u.Query()
	for key := range q {
		if SensitiveKey(key) {
			q.Set(key, Replacement)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Text redacts common credential shapes in human-facing diagnostics.
func Text(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	out := urlPattern.ReplaceAllStringFunc(raw, URL)
	out = authValuePattern.ReplaceAllString(out, "${1}"+Replacement)
	out = redactAssignments(out)
	return out
}

func redactAssignments(raw string) string {
	return sensitiveAssignmentPattern.ReplaceAllStringFunc(raw, func(match string) string {
		parts := sensitiveAssignmentPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return Replacement
		}
		prefix := parts[1]
		key := parts[2]
		value := parts[3]
		switch {
		case strings.HasPrefix(value, `"`):
			return prefix + key + `"` + Replacement + `"`
		case strings.HasPrefix(value, `'`):
			return prefix + key + `'` + Replacement + `'`
		default:
			return prefix + key + Replacement
		}
	})
}

// SensitiveKey reports whether a header, query, environment, or parameter key
// should have its value omitted from support output.
func SensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "token") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "credential") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "cookie") ||
		strings.Contains(key, "signature") ||
		strings.Contains(key, "key")
}

// LooksSensitive is intentionally conservative for fields where the value is a
// command, selector, or local path and keeping partial context is less useful.
func LooksSensitive(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "token") ||
		strings.Contains(value, "password") ||
		strings.Contains(value, "secret") ||
		strings.Contains(value, "credential") ||
		strings.Contains(value, "authorization") ||
		strings.Contains(value, "cookie") ||
		strings.Contains(value, "signature") ||
		strings.HasPrefix(value, "bearer ") ||
		strings.HasPrefix(value, "basic ")
}
