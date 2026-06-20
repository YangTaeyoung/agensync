// Package secret detects inline credentials in source configs and derives
// env-var names so adapters can externalize them instead of writing plaintext.
package secret

import (
	"regexp"
	"strings"
)

var (
	envRef     = regexp.MustCompile(`^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?$`)
	secretLike = regexp.MustCompile(`(?i)(sk-[a-z0-9-]{16,}|gh[pousr]_[A-Za-z0-9]{30,}|[A-Za-z0-9_\-]{32,})`)
)

// LooksLikeSecret reports whether a value is a plausible inline credential
// (and is NOT already an env reference like ${X} or $X).
func LooksLikeSecret(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || envRef.MatchString(v) {
		return false
	}
	// Strip a leading "Bearer " so "Bearer sk-ant-..." is still detected.
	if rest, ok := strings.CutPrefix(v, "Bearer "); ok {
		v = strings.TrimSpace(rest)
	}
	return secretLike.MatchString(v)
}

// EnvVarName derives a stable env var name for a server's secret.
func EnvVarName(server, hint string) string {
	if hint != "" {
		return hint
	}
	return strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(server)) + "_TOKEN"
}
