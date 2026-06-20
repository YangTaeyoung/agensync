package adapter

import (
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestParseMCPServersJSONStdio(t *testing.T) {
	in := `{"mcpServers":{"ctx7":{"type":"stdio","command":"npx","args":["-y","@upstash/context7-mcp"],"env":{"K":"v"}}}}`
	servers, err := ParseMCPServersJSON([]byte(in), MCPJSONOptions{RootKey: "mcpServers"})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(servers))
	}
	s := servers[0]
	if s.Name != "ctx7" || s.Transport != ir.TransportStdio || s.Command != "npx" {
		t.Fatalf("bad server: %+v", s)
	}
	if len(s.Args) != 2 || s.Env["K"] != "v" || !s.Enabled {
		t.Fatalf("bad fields: %+v", s)
	}
}

func TestParseMCPServersJSONRemoteDialects(t *testing.T) {
	// gemini-style: no type; url == SSE, httpUrl == HTTP
	servers, _ := ParseMCPServersJSON([]byte(`{"mcpServers":{"a":{"url":"https://a"},"b":{"httpUrl":"https://b"}}}`), MCPJSONOptions{RootKey: "mcpServers"})
	byName := map[string]ir.McpServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if byName["a"].Transport != ir.TransportSSE || byName["a"].URL != "https://a" {
		t.Fatalf("a: %+v", byName["a"])
	}
	if byName["b"].Transport != ir.TransportHTTP || byName["b"].URL != "https://b" {
		t.Fatalf("b: %+v", byName["b"])
	}
	// antigravity-style: serverUrl == remote
	srv, _ := ParseMCPServersJSON([]byte(`{"mcpServers":{"c":{"serverUrl":"https://c"}}}`), MCPJSONOptions{RootKey: "mcpServers"})
	if srv[0].URL != "https://c" {
		t.Fatalf("serverUrl not parsed: %+v", srv[0])
	}
}

func TestParseMCPServersJSONDisabledAndComments(t *testing.T) {
	// disabled -> Enabled=false; JSON5-ish comments stripped when requested
	in := `{
		// leading comment
		"mcpServers": {"x": {"command": "go", "disabled": true, "autoApprove": ["*"], "timeout": 5}}
	}`
	servers, err := ParseMCPServersJSON([]byte(in), MCPJSONOptions{RootKey: "mcpServers", StripComments: true})
	if err != nil {
		t.Fatal(err)
	}
	if servers[0].Enabled {
		t.Fatalf("disabled server should have Enabled=false: %+v", servers[0])
	}
	if len(servers[0].AutoApprove) != 1 || servers[0].AutoApprove[0] != "*" {
		t.Fatalf("autoApprove: %+v", servers[0].AutoApprove)
	}
	if servers[0].Timeout != 5 {
		t.Fatalf("timeout: %+v", servers[0].Timeout)
	}
}

func TestRenderMCPServersJSONRoundTrip(t *testing.T) {
	servers := []ir.McpServer{
		{Name: "ctx7", Transport: ir.TransportStdio, Command: "npx", Args: []string{"-y", "pkg"}, Enabled: true},
		{Name: "remote", Transport: ir.TransportHTTP, URL: "https://r", Enabled: true},
	}
	out, err := RenderMCPServersJSON(servers, MCPJSONOptions{RootKey: "mcpServers", RemoteURLKey: "url", EmitType: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"mcpServers"`) || !strings.Contains(string(out), `"command": "npx"`) {
		t.Fatalf("render missing stdio:\n%s", out)
	}
	// round-trip back
	got, err := ParseMCPServersJSON(out, MCPJSONOptions{RootKey: "mcpServers"})
	if err != nil || len(got) != 2 {
		t.Fatalf("roundtrip failed: %v %d", err, len(got))
	}
}

func TestRenderMCPServersJSONRemoteKeyRemap(t *testing.T) {
	servers := []ir.McpServer{{Name: "r", Transport: ir.TransportHTTP, URL: "https://r", Enabled: true}}
	out, _ := RenderMCPServersJSON(servers, MCPJSONOptions{RootKey: "mcpServers", RemoteURLKey: "serverUrl"})
	if !strings.Contains(string(out), `"serverUrl": "https://r"`) {
		t.Fatalf("remote key not remapped:\n%s", out)
	}
}
