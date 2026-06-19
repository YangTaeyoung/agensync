package claudecode

import (
	"os"
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
	if a.Meta().ID != "claude-code" {
		t.Fatalf("id=%q", a.Meta().ID)
	}
	c := a.Capabilities()
	if !c.Instructions.Imports || !c.Skills || c.Memory != ir.MemoryFile {
		t.Fatalf("caps=%+v", c)
	}
}

func TestExportInstructionsResolvesImports(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) == 0 {
		t.Fatal("no instructions")
	}
	if ins[0].Activation != ir.ActAlways {
		t.Fatalf("activation=%s", ins[0].Activation)
	}
	if len(ins[0].Imports) != 1 || !strings.Contains(ins[0].Imports[0].Resolved, "composition") {
		t.Fatalf("imports not resolved: %+v", ins[0].Imports)
	}
}

func TestExportMcpFromDotMcpJson(t *testing.T) {
	servers, err := New().ExportMcpServers(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Name != "ctx7" || servers[0].Command != "npx" {
		t.Fatalf("servers=%+v", servers)
	}
	if servers[0].Scope != ir.ScopeProject {
		t.Fatalf("scope=%s", servers[0].Scope)
	}
}

func TestExportSkillsCommandsSubagents(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	skills, _ := a.ExportSkills(ctx)
	if len(skills) != 1 || skills[0].Name != "demo" {
		t.Fatalf("skills=%+v", skills)
	}
	cmds, _ := a.ExportCommands(ctx)
	if len(cmds) != 1 || cmds[0].Name != "foo" {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].ArgSpec.Style != ir.ArgAll {
		t.Fatalf("argspec=%+v", cmds[0].ArgSpec)
	}
	if len(cmds[0].ShellInjections) != 1 || cmds[0].ShellInjections[0] != "git status" {
		t.Fatalf("shell=%+v", cmds[0].ShellInjections)
	}
	subs, _ := a.ExportSubagents(ctx)
	if len(subs) != 1 || subs[0].Name != "bar" || len(subs[0].Tools) != 2 {
		t.Fatalf("subagents=%+v", subs)
	}
	if !strings.Contains(subs[0].SystemPrompt, "code reviewer") {
		t.Fatalf("system prompt=%q", subs[0].SystemPrompt)
	}
}

func TestExportProjectStatePermissions(t *testing.T) {
	ps, err := New().ExportProjectState(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Permissions.Allow) != 2 || len(ps.Permissions.Deny) != 1 {
		t.Fatalf("perms=%+v", ps.Permissions)
	}
	if len(ps.Hooks) != 1 {
		t.Fatalf("hooks=%+v", ps.Hooks)
	}
}

// Round-trip: claude-code -> IR -> claude-code reproduces the core files.
func TestPlanImportRoundTrip(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions, _ = a.ExportInstructions(ctx)
	b.McpServers, _ = a.ExportMcpServers(ctx)
	b.Skills, _ = a.ExportSkills(ctx)
	b.Commands, _ = a.ExportCommands(ctx)
	b.Subagents, _ = a.ExportSubagents(ctx)
	b.ProjectState, _ = a.ExportProjectState(ctx)

	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	res := a.Apply(plan, adapter.ApplyOptions{})
	if len(res.Errors) != 0 {
		t.Fatalf("apply errors: %v", res.Errors)
	}

	mustExist := []string{
		"CLAUDE.md",
		".mcp.json",
		filepath.Join(".claude", "skills", "demo", "SKILL.md"),
		filepath.Join(".claude", "commands", "foo.md"),
		filepath.Join(".claude", "agents", "bar.md"),
		filepath.Join(".claude", "settings.json"),
	}
	for _, rel := range mustExist {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("expected %s to be written: %v", rel, err)
		}
	}
	mcp, _ := os.ReadFile(filepath.Join(out, ".mcp.json"))
	if !strings.Contains(string(mcp), "ctx7") || !strings.Contains(string(mcp), "npx") {
		t.Fatalf(".mcp.json content:\n%s", mcp)
	}
}

func TestPlanImportSerializesHooks(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.ProjectState = ir.ProjectState{
		Permissions: ir.Permissions{Allow: []string{"Read"}},
		Hooks:       []ir.Hook{{Event: "PostToolUse", Raw: map[string]any{"config": `[{"matcher":"Edit"}]`}}},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})
	var settings string
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join(".claude", "settings.json")) {
			settings = string(f.Content)
		}
	}
	if !strings.Contains(settings, "hooks") || !strings.Contains(settings, "PostToolUse") {
		t.Fatalf("hooks not serialized into settings.json:\n%s", settings)
	}
}

// Hooks from a non-claude source use the IR-canonical Event/Command fields and
// must be reconstructed into settings.json (never silently dropped).
func TestPlanImportReconstructsCommandHook(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{Hooks: []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})
	var settings string
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join(".claude", "settings.json")) {
			settings = string(f.Content)
		}
	}
	if !strings.Contains(settings, "PreToolUse") || !strings.Contains(settings, "echo hi") {
		t.Fatalf("command hook not reconstructed:\n%s", settings)
	}
}

func TestPlanImportUserMcpAndTrustToHomeJson(t *testing.T) {
	a := New()
	home := t.TempDir()
	proj := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeUser}, Name: "personal", Transport: ir.TransportStdio, Command: "node", Enabled: true},
	}
	b.ProjectState = ir.ProjectState{Trust: "trusted"}
	plan := a.PlanImport(b, ir.Context{ProjectPath: proj, HomeDir: home}, adapter.ImportOptions{})
	var homeJSON string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(home, ".claude.json") {
			homeJSON = string(f.Content)
		}
	}
	if homeJSON == "" {
		t.Fatalf("~/.claude.json not planned: %v", plan.Files)
	}
	if !strings.Contains(homeJSON, "personal") || !strings.Contains(homeJSON, "hasTrustDialogAccepted") {
		t.Fatalf("home json missing personal mcp or trust:\n%s", homeJSON)
	}
	// the personal server must NOT have been dropped to nowhere
	abs, _ := filepath.Abs(proj)
	if !strings.Contains(homeJSON, abs) {
		t.Fatalf("home json missing project abs key %q:\n%s", abs, homeJSON)
	}
}

func TestPlanImportWritesMemoryWhenRequested(t *testing.T) {
	a := New()
	home := t.TempDir()
	// seed a personal/global memory file
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("personal memory body\n"), 0o644)
	src := ir.Context{ProjectPath: "testdata/from", HomeDir: home}
	ins, _ := a.ExportInstructions(src)
	var memCount int
	for _, in := range ins {
		if in.IsMemory() {
			memCount++
		}
	}
	if memCount == 0 {
		t.Fatal("expected a user-scope memory instruction from ~/.claude/CLAUDE.md")
	}

	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = ins
	destHome := t.TempDir()
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: destHome}
	// memory category requested -> personal memory file planned
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	found := false
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join(".claude", "CLAUDE.md")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("memory file not planned: %+v", plan.Files)
	}
}
