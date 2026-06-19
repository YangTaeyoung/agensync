package antigravity

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
	if a.Meta().ID != "antigravity" {
		t.Fatalf("id=%q", a.Meta().ID)
	}
	if a.Meta().DisplayName != "Antigravity" || a.Meta().Vendor != "Google" || a.Meta().Confidence != "medium" {
		t.Fatalf("meta=%+v", a.Meta())
	}
	c := a.Capabilities()
	if c.Instructions.Imports {
		t.Fatal("antigravity has no imports")
	}
	if c.MCP.RemoteURLKey != "serverUrl" || c.MCP.RootKey != "mcpServers" || c.MCP.Format != "json" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
	if !c.Skills || !c.Commands.Supported || c.Commands.Format != "md" {
		t.Fatalf("skills/commands caps=%+v %+v", c.Skills, c.Commands)
	}
	if c.Subagents != "false" {
		t.Fatalf("subagents=%q", c.Subagents)
	}
	if c.Memory != ir.MemoryFile {
		t.Fatalf("memory=%q", c.Memory)
	}
}

func TestDetect(t *testing.T) {
	res := New().Detect(fromCtx(t))
	if !res.Present {
		t.Fatalf("expected present, got %+v", res)
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	// AGENTS.md + GEMINI.md + .agents/rules/style.md
	var sawAgents, sawGemini, sawRule bool
	for _, in := range ins {
		if in.Activation != ir.ActAlways {
			t.Fatalf("activation=%q for %s", in.Activation, in.Origin)
		}
		switch {
		case strings.HasSuffix(in.Origin, "AGENTS.md"):
			sawAgents = true
			if !strings.Contains(in.Body, "idiomatic Go") {
				t.Fatalf("AGENTS.md body=%q", in.Body)
			}
		case strings.HasSuffix(in.Origin, "GEMINI.md"):
			sawGemini = true
		case strings.HasSuffix(in.Origin, filepath.Join("rules", "style.md")):
			sawRule = true
		}
	}
	if !sawAgents || !sawGemini || !sawRule {
		t.Fatalf("agents=%v gemini=%v rule=%v ins=%+v", sawAgents, sawGemini, sawRule, ins)
	}
}

func TestExportMcpServersServerURL(t *testing.T) {
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
	if fig.Transport != ir.TransportHTTP || fig.URL != "https://mcp.figma.com/sse" {
		t.Fatalf("figma serverUrl not parsed: %+v", fig)
	}
}

func TestExportSkills(t *testing.T) {
	skills, err := New().ExportSkills(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "demo" {
		t.Fatalf("skills=%+v", skills)
	}
}

func TestExportCommands(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "deploy" {
		t.Fatalf("cmds=%+v", cmds)
	}
	if cmds[0].ArgSpec.Style != ir.ArgAll {
		t.Fatalf("argspec=%+v", cmds[0].ArgSpec)
	}
}

func TestExportSubagentsNone(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil || subs != nil {
		t.Fatalf("expected no subagents, got %v %v", subs, err)
	}
}

func TestPlanImportInstructionsAndGeminiConflict(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{Body: "project rules", Scope: ir.ScopeProject}},
	}
	// Carry a GEMINI.md provenance to imply a conflict.
	b.Unmapped = []ir.RawArtifact{{Category: "instructions", OrigPath: "GEMINI.md", Content: []byte("gemini")}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	var sawAgents bool
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, "AGENTS.md") {
			sawAgents = true
			if !strings.Contains(string(f.Content), "project rules") {
				t.Fatalf("AGENTS.md content=%q", f.Content)
			}
		}
	}
	if !sawAgents {
		t.Fatalf("AGENTS.md not planned: %+v", plan.Files)
	}
	var sawConflict bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" && strings.Contains(w.Artifact, "GEMINI.md") {
			sawConflict = true
		}
	}
	if !sawConflict {
		t.Fatalf("expected GEMINI.md conflict warning: %+v", plan.Warnings)
	}
}

func TestPlanImportMemoryToHome(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{Common: ir.Common{Body: "personal memory", Scope: ir.ScopeUser}}}
	home := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: t.TempDir(), HomeDir: home}, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	found := false
	for _, f := range plan.Files {
		if f.Path == filepath.Join(home, ".gemini", "AGENTS.md") {
			found = true
			if !strings.Contains(string(f.Content), "personal memory") {
				t.Fatalf("home memory content=%q", f.Content)
			}
		}
	}
	if !found {
		t.Fatalf("memory not written to ~/.gemini/AGENTS.md: %+v", plan.Files)
	}
}

func TestPlanImportMcpServerURLNoComments(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Name: "figma", Transport: ir.TransportHTTP, URL: "https://mcp.figma.com/sse", Enabled: true, Timeout: 30000},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	var content string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".agents", "mcp_config.json") {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf("mcp_config.json not planned: %+v", plan.Files)
	}
	if !strings.Contains(content, "serverUrl") {
		t.Fatalf("expected serverUrl remap, got:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			t.Fatalf("output JSON must be comment-free:\n%s", content)
		}
	}
	if strings.Contains(content, "timeout") {
		t.Fatalf("timeout must be dropped:\n%s", content)
	}
	if strings.Contains(content, `"type"`) {
		t.Fatalf("type must not be emitted:\n%s", content)
	}
}

func TestPlanImportSkillsAndCommands(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Skills = []ir.Skill{{Common: ir.Common{Body: "skill body"}, Name: "demo", Description: "d"}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "do it"}, Name: "deploy"}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{})
	var sawSkill, sawCmd bool
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".agents", "skills", "demo", "SKILL.md") {
			sawSkill = true
		}
		if f.Path == filepath.Join(out, ".agents", "workflows", "deploy.md") {
			sawCmd = true
		}
	}
	if !sawSkill || !sawCmd {
		t.Fatalf("skill=%v cmd=%v files=%+v", sawSkill, sawCmd, plan.Files)
	}
}

func TestPlanImportSSEServerSkippedWithWarning(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Name: "sse-srv", Transport: ir.TransportSSE, URL: "https://sse.example.com/sse", Enabled: true},
		{Name: "http-srv", Transport: ir.TransportHTTP, URL: "https://http.example.com", Enabled: true},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})

	// The SSE server must be skipped with a warning, never coerced to serverUrl.
	var sawSSEWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && w.Artifact == "sse-srv" && w.Action == ir.ActionSkip {
			sawSSEWarn = true
		}
	}
	if !sawSSEWarn {
		t.Fatalf("expected SSE skip warning: %+v", plan.Warnings)
	}

	var content string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".agents", "mcp_config.json") {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf("mcp_config.json not planned (http server should still be written): %+v", plan.Files)
	}
	// The SSE server's URL must not appear in the rendered output.
	if strings.Contains(content, "https://sse.example.com/sse") || strings.Contains(content, "sse-srv") {
		t.Fatalf("SSE server must not be rendered:\n%s", content)
	}
	if !strings.Contains(content, "http-srv") {
		t.Fatalf("http server should still be rendered:\n%s", content)
	}
}

func TestPlanImportSSEOnlyNoFileWritten(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Name: "sse-only", Transport: ir.TransportSSE, URL: "https://sse.example.com/sse", Enabled: true},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".agents", "mcp_config.json") {
			t.Fatalf("no mcp_config.json should be planned when only SSE servers exist:\n%s", f.Content)
		}
	}
	var sawSSEWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && w.Artifact == "sse-only" && w.Action == ir.ActionSkip {
			sawSSEWarn = true
		}
	}
	if !sawSSEWarn {
		t.Fatalf("expected SSE skip warning: %+v", plan.Warnings)
	}
}

func TestPlanImportHooksSkipWarning(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.ProjectState = ir.ProjectState{
		Hooks: []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var sawHooksWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "project-state" && w.Artifact == "hooks" && w.Action == ir.ActionSkip {
			sawHooksWarn = true
		}
	}
	if !sawHooksWarn {
		t.Fatalf("expected hooks skip warning: %+v", plan.Warnings)
	}
}

func TestPlanImportFlattensImports(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{
			Common: ir.Common{
				Body:   "see @sub.md for details",
				Scope:  ir.ScopeProject,
				Origin: "AGENTS.md",
			},
			Imports: []ir.Import{{Kind: ir.ImpInline, Target: "sub.md", Resolved: "SUB CONTENT"}},
		},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})

	var content string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, "AGENTS.md") {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf("AGENTS.md not planned: %+v", plan.Files)
	}
	if !strings.Contains(content, "SUB CONTENT") {
		t.Fatalf("import not flattened inline:\n%s", content)
	}
	if strings.Contains(content, "@sub.md") {
		t.Fatalf("import marker should be replaced:\n%s", content)
	}
	var sawFlattenWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" && w.Action == ir.ActionInline && strings.Contains(w.Reason, "flattened") {
			sawFlattenWarn = true
		}
	}
	if !sawFlattenWarn {
		t.Fatalf("expected imports-flattened warning: %+v", plan.Warnings)
	}
}

// TestUnsupportedCategoriesWarn feeds a bundle containing every category and
// asserts a warning exists for each category antigravity cannot represent.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{Common: ir.Common{Body: "rules", Scope: ir.ScopeProject}}}
	b.McpServers = []ir.McpServer{{Name: "ctx7", Transport: ir.TransportStdio, Command: "npx", Enabled: true}}
	b.Skills = []ir.Skill{{Common: ir.Common{Body: "s"}, Name: "demo"}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "c"}, Name: "deploy"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{Body: "sp"}, Name: "reviewer", SystemPrompt: "review"}}
	b.ProjectState = ir.ProjectState{Trust: "trusted"}

	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{})

	// Subagents are unsupported -> must warn (never drop).
	var sawSubagentWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "subagents" && strings.Contains(w.Artifact, "reviewer") {
			sawSubagentWarn = true
		}
	}
	if !sawSubagentWarn {
		t.Fatalf("expected subagent unsupported warning: %+v", plan.Warnings)
	}

	// Trust is not natively writable -> must warn manual.
	var sawTrustWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "project-state" && w.Action == ir.ActionManual {
			sawTrustWarn = true
		}
	}
	if !sawTrustWarn {
		t.Fatalf("expected trust manual warning: %+v", plan.Warnings)
	}

	// Supported categories must still produce files (not dropped).
	var sawInstr, sawMcp, sawSkill, sawCmd bool
	for _, f := range plan.Files {
		switch f.Path {
		case filepath.Join(out, "AGENTS.md"):
			sawInstr = true
		case filepath.Join(out, ".agents", "mcp_config.json"):
			sawMcp = true
		case filepath.Join(out, ".agents", "skills", "demo", "SKILL.md"):
			sawSkill = true
		case filepath.Join(out, ".agents", "workflows", "deploy.md"):
			sawCmd = true
		}
	}
	if !sawInstr || !sawMcp || !sawSkill || !sawCmd {
		t.Fatalf("supported categories dropped: instr=%v mcp=%v skill=%v cmd=%v", sawInstr, sawMcp, sawSkill, sawCmd)
	}
}
