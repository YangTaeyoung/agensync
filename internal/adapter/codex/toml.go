package codex

import (
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/secret"
)

// ---- decode (go-toml/v2) ----

type codexMCPRaw struct {
	Command           string            `toml:"command"`
	Args              []string          `toml:"args"`
	Env               map[string]string `toml:"env"`
	Cwd               string            `toml:"cwd"`
	URL               string            `toml:"url"`
	BearerTokenEnvVar string            `toml:"bearer_token_env_var"`
	Headers           map[string]string `toml:"headers"`
}

type codexProject struct {
	TrustLevel string `toml:"trust_level"`
}

type codexConfig struct {
	McpServers map[string]codexMCPRaw  `toml:"mcp_servers"`
	Projects   map[string]codexProject `toml:"projects"`
}

func decodeConfigTOML(b []byte) (codexConfig, error) {
	var c codexConfig
	err := toml.Unmarshal(b, &c)
	return c, err
}

type codexAgent struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
	Model                 string `toml:"model"`
}

func decodeAgentTOML(b []byte) (codexAgent, error) {
	var a codexAgent
	err := toml.Unmarshal(b, &a)
	return a, err
}

// ---- encode (manual, for deterministic [table] form) ----

type secretRef struct {
	Server string
	EnvVar string
	Value  string
}

// encodeMCPTOML renders IR servers as [mcp_servers.<name>] TOML, externalizing
// any inline secret into an env-var reference (returned as secretRef so the
// caller can emit a .env stub and warnings). The TOML never contains plaintext.
func encodeMCPTOML(servers []ir.McpServer, from string) ([]byte, []secretRef, error) {
	sorted := append([]ir.McpServer(nil), servers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	var refs []secretRef
	for _, s := range sorted {
		b.WriteString("[mcp_servers." + s.Name + "]\n")
		if s.Transport == ir.TransportStdio || s.Command != "" {
			b.WriteString("command = " + tomlString(s.Command) + "\n")
			if len(s.Args) > 0 {
				b.WriteString("args = " + tomlStringArray(s.Args) + "\n")
			}
			if env, envRefs := externalizeEnv(s); len(env) > 0 {
				b.WriteString("env = " + tomlInlineMap(env) + "\n")
				refs = append(refs, envRefs...)
			} else {
				refs = append(refs, envRefs...)
			}
			if s.Cwd != "" {
				b.WriteString("cwd = " + tomlString(s.Cwd) + "\n")
			}
		} else {
			b.WriteString("url = " + tomlString(s.URL) + "\n")
			envVar, value, ok := bearerSecret(s)
			switch {
			case ok:
				b.WriteString("bearer_token_env_var = " + tomlString(envVar) + "\n")
				refs = append(refs, secretRef{Server: s.Name, EnvVar: envVar, Value: value})
			case s.Auth != nil && s.Auth.BearerTokenEnvVar != "":
				b.WriteString("bearer_token_env_var = " + tomlString(s.Auth.BearerTokenEnvVar) + "\n")
			}
		}
		b.WriteString("\n")
	}
	return []byte(b.String()), refs, nil
}

// externalizeEnv replaces inline-secret env values with ${VAR} references.
func externalizeEnv(s ir.McpServer) (map[string]string, []secretRef) {
	if len(s.Env) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	var refs []secretRef
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := s.Env[k]
		if secret.LooksLikeSecret(v) {
			envVar := secret.EnvVarName(s.Name+"-"+k, "")
			out[k] = "${" + envVar + "}"
			refs = append(refs, secretRef{Server: s.Name, EnvVar: envVar, Value: v})
		} else {
			out[k] = v
		}
	}
	return out, refs
}

// bearerSecret extracts an inline "Authorization: Bearer <secret>" header.
func bearerSecret(s ir.McpServer) (envVar, value string, ok bool) {
	for k, v := range s.Headers {
		if !strings.EqualFold(k, "Authorization") {
			continue
		}
		if !secret.LooksLikeSecret(v) {
			continue
		}
		value = strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
		return secret.EnvVarName(s.Name, ""), value, true
	}
	return "", "", false
}

func encodeAgentTOML(sub ir.Subagent) []byte {
	var b strings.Builder
	b.WriteString("name = " + tomlString(sub.Name) + "\n")
	if sub.Description != "" {
		b.WriteString("description = " + tomlString(sub.Description) + "\n")
	}
	if sub.SystemPrompt != "" {
		b.WriteString("developer_instructions = " + tomlString(sub.SystemPrompt) + "\n")
	}
	if sub.Model != "" {
		b.WriteString("model = " + tomlString(sub.Model) + "\n")
	}
	return []byte(b.String())
}

func tomlString(s string) string { return strconv.Quote(s) }

func tomlStringArray(a []string) string {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = tomlString(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func tomlInlineMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " = " + tomlString(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
