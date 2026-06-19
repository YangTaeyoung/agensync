package copilot

import (
	"sort"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/secret"
)

// secretRef records an inline secret that was lifted out of an MCP server config
// into an env-var reference, so the caller can render a .env stub and warn.
type secretRef struct {
	Server string
	EnvVar string
	Value  string
}

// externalizeSecrets returns a copy of servers in which every inline-secret env
// value or Authorization header has been replaced by a ${VAR} reference, plus
// the list of extracted secrets. The rendered Copilot JSON therefore never
// contains plaintext credentials (§8 / capability matrix row 157). Servers and
// their maps are deep-copied so the caller's IR is not mutated.
func externalizeSecrets(servers []ir.McpServer) ([]ir.McpServer, []secretRef) {
	out := make([]ir.McpServer, len(servers))
	var refs []secretRef
	for i, s := range servers {
		cp := s
		if len(s.Env) > 0 {
			env := make(map[string]string, len(s.Env))
			keys := make([]string, 0, len(s.Env))
			for k := range s.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := s.Env[k]
				if secret.LooksLikeSecret(v) {
					envVar := secret.EnvVarName(s.Name+"-"+k, "")
					env[k] = "${" + envVar + "}"
					refs = append(refs, secretRef{Server: s.Name, EnvVar: envVar, Value: v})
				} else {
					env[k] = v
				}
			}
			cp.Env = env
		}
		if len(s.Headers) > 0 {
			hdr := make(map[string]string, len(s.Headers))
			keys := make([]string, 0, len(s.Headers))
			for k := range s.Headers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := s.Headers[k]
				if secret.LooksLikeSecret(v) {
					hint := ""
					if strings.EqualFold(k, "Authorization") {
						hint = secret.EnvVarName(s.Name, "")
					}
					envVar := secret.EnvVarName(s.Name+"-"+k, hint)
					// Preserve a leading "Bearer " scheme so the reference still
					// renders as a valid Authorization header.
					prefix := ""
					if rest, ok := strings.CutPrefix(strings.TrimSpace(v), "Bearer "); ok {
						prefix = "Bearer "
						v = strings.TrimSpace(rest)
					}
					hdr[k] = prefix + "${" + envVar + "}"
					refs = append(refs, secretRef{Server: s.Name, EnvVar: envVar, Value: v})
				} else {
					hdr[k] = v
				}
			}
			cp.Headers = hdr
		}
		out[i] = cp
	}
	return out, refs
}

// renderEnvStub builds a .env file listing every extracted secret. Values are
// the original inline secrets; the user is told to set them before running.
func renderEnvStub(refs []secretRef) []byte {
	var b strings.Builder
	b.WriteString("# agensync: set these before running; values came from inline secrets in the source config\n")
	seen := map[string]bool{}
	for _, r := range refs {
		if seen[r.EnvVar] {
			continue
		}
		seen[r.EnvVar] = true
		b.WriteString(r.EnvVar + "=" + r.Value + "\n")
	}
	return []byte(b.String())
}
