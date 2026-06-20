package adapter

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

// MCPJSONOptions configures the dialect of an mcpServers-style JSON config.
type MCPJSONOptions struct {
	RootKey       string // "mcpServers" | "servers" | "mcp_servers"; default "mcpServers"
	RemoteURLKey  string // key used for the remote URL on render: "url" | "httpUrl" | "serverUrl"
	EmitType      bool   // write a "type" field on render (claude/copilot do; gemini does not)
	StripComments bool   // strip // and /* */ comments before parse (antigravity)
	DropTimeout   bool   // omit timeout on render (antigravity)
}

func (o MCPJSONOptions) rootKey() string {
	if o.RootKey == "" {
		return "mcpServers"
	}
	return o.RootKey
}

// mcpJSONServer is the union on-disk shape across dialects.
type mcpJSONServer struct {
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	URL         string            `json:"url,omitempty"`
	HTTPURL     string            `json:"httpUrl,omitempty"`
	ServerURL   string            `json:"serverUrl,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	AutoApprove []string          `json:"autoApprove,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
}

var (
	lineComment   = regexp.MustCompile(`(?m)^\s*//.*$`)
	blockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	trailingComma = regexp.MustCompile(`,(\s*[}\]])`)
)

// StripJSONComments removes // line and /* */ block comments and trailing
// commas so a JSON5-ish config parses with encoding/json.
func StripJSONComments(data []byte) []byte {
	data = blockComment.ReplaceAll(data, nil)
	data = lineComment.ReplaceAll(data, nil)
	data = trailingComma.ReplaceAll(data, []byte("$1"))
	return data
}

// ParseMCPServersJSON parses an mcpServers-style JSON config into IR servers.
// Transport is taken from an explicit "type" when present, otherwise inferred
// from which remote-URL key is set (httpUrl/serverUrl=HTTP, url=SSE, command=stdio).
func ParseMCPServersJSON(data []byte, o MCPJSONOptions) ([]ir.McpServer, error) {
	if o.StripComments {
		data = StripJSONComments(data)
	}
	var root map[string]map[string]mcpJSONServer
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	raw := root[o.rootKey()]
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ir.McpServer, 0, len(names))
	for _, name := range names {
		js := raw[name]
		s := ir.McpServer{
			Name:         name,
			Command:      js.Command,
			Args:         js.Args,
			Env:          js.Env,
			Cwd:          js.Cwd,
			Headers:      js.Headers,
			Enabled:      !js.Disabled,
			AutoApprove:  js.AutoApprove,
			Timeout:      js.Timeout,
			SecretsStyle: ir.SecretInline,
		}
		switch {
		case js.Command != "":
			s.Transport = ir.TransportStdio
		case js.Type != "":
			s.Transport = mapTypeTransport(js.Type)
			s.URL = firstNonEmpty(js.URL, js.HTTPURL, js.ServerURL)
		case js.HTTPURL != "":
			s.Transport = ir.TransportHTTP
			s.URL = js.HTTPURL
		case js.ServerURL != "":
			s.Transport = ir.TransportHTTP
			s.URL = js.ServerURL
		case js.URL != "":
			s.Transport = ir.TransportSSE
			s.URL = js.URL
		}
		out = append(out, s)
	}
	return out, nil
}

func mapTypeTransport(t string) ir.Transport {
	switch t {
	case "http":
		return ir.TransportHTTP
	case "sse":
		return ir.TransportSSE
	default: // "local", "stdio", ""
		return ir.TransportStdio
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RenderMCPServersJSON serializes IR servers into an mcpServers-style JSON
// config, honoring the dialect's root key, remote-URL key and type emission.
// Output is deterministic (sorted keys, 2-space indent).
func RenderMCPServersJSON(servers []ir.McpServer, o MCPJSONOptions) ([]byte, error) {
	remoteKey := o.RemoteURLKey
	if remoteKey == "" {
		remoteKey = "url"
	}
	body := map[string]any{}
	for _, s := range servers {
		js := map[string]any{}
		if s.Transport == ir.TransportStdio || s.Command != "" {
			if o.EmitType {
				js["type"] = "stdio"
			}
			js["command"] = s.Command
			if len(s.Args) > 0 {
				js["args"] = s.Args
			}
			if len(s.Env) > 0 {
				js["env"] = s.Env
			}
			if s.Cwd != "" {
				js["cwd"] = s.Cwd
			}
		} else {
			if o.EmitType {
				js["type"] = string(s.Transport)
			}
			js[remoteKey] = s.URL
			if len(s.Headers) > 0 {
				js["headers"] = s.Headers
			}
		}
		if !s.Enabled {
			js["disabled"] = true
		}
		if len(s.AutoApprove) > 0 {
			js["autoApprove"] = s.AutoApprove
		}
		if s.Timeout > 0 && !o.DropTimeout {
			js["timeout"] = s.Timeout
		}
		body[s.Name] = js
	}
	doc := map[string]any{o.rootKey(): body}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
