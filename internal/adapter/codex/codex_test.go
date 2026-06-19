package codex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func fromCtx(t *testing.T) ir.Context {
	t.Helper()
	return ir.Context{ProjectPath: "testdata/from", HomeDir: t.TempDir()}
}

func TestMetaAndCapabilities(t *testing.T) {
	a := New()
	if a.Meta().ID != "codex" {
		t.Fatalf("id=%q", a.Meta().ID)
	}
	c := a.Capabilities()
	if c.Instructions.Imports {
		t.Fatal("codex has no imports")
	}
	if c.Instructions.CharBudget != 32768 {
		t.Fatalf("charBudget=%d", c.Instructions.CharBudget)
	}
	if c.MCP.SecretStyle != ir.SecretEnvIndirect || c.MCP.Format != "toml" || c.MCP.RootKey != "mcp_servers" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
	if c.Commands.Supported {
		t.Fatal("codex commands deprecated -> not supported")
	}
}

func TestExportInstructionsNoImports(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 1 || ins[0].Activation != ir.ActAlways || len(ins[0].Imports) != 0 {
		t.Fatalf("instructions=%+v", ins)
	}
	if !strings.Contains(ins[0].Body, "idiomatic Go") {
		t.Fatalf("body=%q", ins[0].Body)
	}
}

func TestExportMcpFromToml(t *testing.T) {
	servers, err := New().ExportMcpServers(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ir.McpServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if byName["ctx7"].Command != "npx" || byName["ctx7"].Transport != ir.TransportStdio {
		t.Fatalf("ctx7=%+v", byName["ctx7"])
	}
	fig := byName["figma"]
	if fig.Transport != ir.TransportHTTP || fig.Auth == nil || fig.Auth.BearerTokenEnvVar != "FIGMA_TOKEN" {
		t.Fatalf("figma=%+v auth=%+v", fig, fig.Auth)
	}
	if fig.SecretsStyle != ir.SecretEnvIndirect {
		t.Fatalf("secrets style=%s", fig.SecretsStyle)
	}
}

func TestExportSubagentsFromToml(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "reviewer" {
		t.Fatalf("subs=%+v", subs)
	}
	if !strings.Contains(subs[0].SystemPrompt, "careful reviewer") || subs[0].Model != "gpt-5-codex" {
		t.Fatalf("sub=%+v", subs[0])
	}
}

func TestExportCommandsNone(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil || cmds != nil {
		t.Fatalf("expected no commands, got %v %v", cmds, err)
	}
}

func TestMcpToTOMLEncode(t *testing.T) {
	out, _, err := encodeMCPTOML([]ir.McpServer{
		{Name: "ctx7", Transport: ir.TransportStdio, Command: "npx", Args: []string{"-y", "pkg"}, Enabled: true},
	}, "codex")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "[mcp_servers.ctx7]") || !strings.Contains(s, `command = "npx"`) {
		t.Fatalf("toml:\n%s", s)
	}
}

// Inline secret in a header must be externalized to a .env stub; the TOML must
// not contain the plaintext secret, and a manual warning must be emitted.
func TestPlanInlineSecretExternalized(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{{
		Name:      "figma",
		Transport: ir.TransportHTTP,
		URL:       "https://mcp.figma.com",
		Headers:   map[string]string{"Authorization": "Bearer sk-ant-secret0123456789abcdef0123"},
		Enabled:   true,
	}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})

	var sawEnv, sawToml bool
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, ".env") {
			sawEnv = true
			if !strings.Contains(string(f.Content), "sk-ant-secret") {
				t.Fatalf(".env should hold the secret:\n%s", f.Content)
			}
		}
		if strings.HasSuffix(f.Path, "config.toml") {
			sawToml = true
			if strings.Contains(string(f.Content), "sk-ant-secret") {
				t.Fatalf("TOML must not contain plaintext secret:\n%s", f.Content)
			}
		}
	}
	if !sawEnv || !sawToml {
		t.Fatalf("env=%v toml=%v files=%+v", sawEnv, sawToml, plan.Files)
	}
	var sawWarn bool
	for _, w := range plan.Warnings {
		if w.Action == ir.ActionManual && strings.Contains(w.Reason, "secret") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("expected manual secret warning, got %+v", plan.Warnings)
	}
}

func TestPlanCommandsBecomeSkills(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Commands = []ir.Command{{Common: ir.Common{Body: "do the thing"}, Name: "deploy", Description: "Deploy it"}}
	plan := a.PlanImport(b, ir.Context{ProjectPath: t.TempDir()}, adapter.ImportOptions{Categories: map[string]bool{"commands": true}})
	var sawSkill, sawWarn bool
	for _, f := range plan.Files {
		if strings.Contains(f.Path, filepath.Join(".agents", "skills", "deploy")) && strings.HasSuffix(f.Path, "SKILL.md") {
			sawSkill = true
		}
	}
	for _, w := range plan.Warnings {
		if w.Category == "commands" && w.Action == ir.ActionInline {
			sawWarn = true
		}
	}
	if !sawSkill || !sawWarn {
		t.Fatalf("commands->skill: skill=%v warn=%v files=%+v warns=%+v", sawSkill, sawWarn, plan.Files, plan.Warnings)
	}
}

func TestPlanFlattensImportsWithWarning(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common:  ir.Common{Body: "intro @sub.md outro", Scope: ir.ScopeProject},
		Imports: []ir.Import{{Kind: ir.ImpInline, Target: "sub.md", Resolved: "RESOLVED"}},
	}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	var body string
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "AGENTS.md") {
			body = string(f.Content)
		}
	}
	if !strings.Contains(body, "RESOLVED") || strings.Contains(body, "@sub.md") {
		t.Fatalf("imports not flattened: %q", body)
	}
	var sawWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("expected flatten warning: %+v", plan.Warnings)
	}
}

func TestPlanMemoryToHomeAgentsMd(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{Common: ir.Common{Body: "personal memory", Scope: ir.ScopeUser}}}
	home := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: t.TempDir(), HomeDir: home}, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	found := false
	for _, f := range plan.Files {
		if f.Path == filepath.Join(home, ".codex", "AGENTS.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("memory not written to ~/.codex/AGENTS.md: %+v", plan.Files)
	}
}
