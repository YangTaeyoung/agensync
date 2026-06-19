package aider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func fixtureCtx(t *testing.T) ir.Context {
	t.Helper()
	abs, err := filepath.Abs("testdata/from")
	if err != nil {
		t.Fatal(err)
	}
	return ir.Context{ProjectPath: abs, HomeDir: t.TempDir()}
}

func TestMeta(t *testing.T) {
	m := New().Meta()
	if m.ID != "aider" {
		t.Fatalf("id = %q, want aider", m.ID)
	}
	if m.DisplayName != "Aider" || m.Vendor != "Aider" || m.Confidence != "high" {
		t.Fatalf("unexpected meta: %+v", m)
	}
}

func TestCapabilities(t *testing.T) {
	c := New().Capabilities()
	if c.Memory != ir.MemoryNone {
		t.Errorf("Memory = %q, want none", c.Memory)
	}
	if c.Skills {
		t.Errorf("Skills should be false")
	}
	if c.Commands.Supported {
		t.Errorf("Commands should be unsupported")
	}
	if c.Subagents != "false" {
		t.Errorf("Subagents = %q, want false", c.Subagents)
	}
	if c.MCP.ProjectScope {
		t.Errorf("MCP.ProjectScope should be false")
	}
	if c.Ignore != "block" {
		t.Errorf("Ignore = %q, want block", c.Ignore)
	}
	if c.Instructions.Imports {
		t.Errorf("Instructions.Imports should be false")
	}
}

func TestDetect(t *testing.T) {
	ctx := fixtureCtx(t)
	res := New().Detect(ctx)
	if !res.Present {
		t.Fatalf("expected Present true; evidence=%v", res.Evidence)
	}
}

func TestExportInstructions(t *testing.T) {
	ctx := fixtureCtx(t)
	ins, err := New().ExportInstructions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) < 1 {
		t.Fatalf("expected at least the CONVENTIONS.md instruction, got %d", len(ins))
	}
	var conv *ir.Instruction
	var style *ir.Instruction
	for i := range ins {
		if strings.HasSuffix(ins[i].Origin, "CONVENTIONS.md") {
			conv = &ins[i]
		}
		if strings.HasSuffix(ins[i].Origin, filepath.Join("docs", "STYLE.md")) {
			style = &ins[i]
		}
	}
	if conv == nil {
		t.Fatalf("CONVENTIONS.md instruction not found; got %+v", ins)
	}
	if conv.Activation != ir.ActAlways {
		t.Errorf("CONVENTIONS activation = %q, want always", conv.Activation)
	}
	if conv.Scope != ir.ScopeProject {
		t.Errorf("CONVENTIONS scope = %q, want project", conv.Scope)
	}
	if !strings.Contains(conv.Body, "Project Conventions") {
		t.Errorf("CONVENTIONS body missing content: %q", conv.Body)
	}
	// The .aider.conf.yml read: list references docs/STYLE.md — must be pulled in.
	if style == nil {
		t.Fatalf("expected docs/STYLE.md (referenced by .aider.conf.yml read:) to be exported; got %+v", ins)
	}
	if !strings.Contains(style.Body, "Style Guide") {
		t.Errorf("STYLE body missing content: %q", style.Body)
	}
}

func TestExportUnsupportedReturnNil(t *testing.T) {
	ctx := fixtureCtx(t)
	a := New()
	if mcp, err := a.ExportMcpServers(ctx); err != nil || mcp != nil {
		t.Errorf("ExportMcpServers = %v, %v; want nil, nil", mcp, err)
	}
	if sk, err := a.ExportSkills(ctx); err != nil || sk != nil {
		t.Errorf("ExportSkills = %v, %v; want nil, nil", sk, err)
	}
	if cmd, err := a.ExportCommands(ctx); err != nil || cmd != nil {
		t.Errorf("ExportCommands = %v, %v; want nil, nil", cmd, err)
	}
	if sub, err := a.ExportSubagents(ctx); err != nil || sub != nil {
		t.Errorf("ExportSubagents = %v, %v; want nil, nil", sub, err)
	}
}

func TestExportProjectState(t *testing.T) {
	ctx := fixtureCtx(t)
	ps, err := New().ExportProjectState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ps.IgnoreMode != ir.IgnoreBlock {
		t.Errorf("IgnoreMode = %q, want block", ps.IgnoreMode)
	}
	found := false
	for _, p := range ps.IgnorePatterns {
		if p == "dist/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dist/ in ignore patterns; got %v", ps.IgnorePatterns)
	}
}

// --- PlanImport ---

func findFile(plan ir.WritePlan, suffix string) *ir.PlannedFile {
	for i := range plan.Files {
		if strings.HasSuffix(plan.Files[i].Path, suffix) {
			return &plan.Files[i]
		}
	}
	return nil
}

func hasWarning(plan ir.WritePlan, cat, artifact string) bool {
	for _, w := range plan.Warnings {
		if w.Category == cat && (artifact == "" || strings.Contains(w.Artifact, artifact)) {
			return true
		}
	}
	return false
}

func TestPlanImportInstructions(t *testing.T) {
	out := t.TempDir()
	ctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{
			Common:     ir.Common{ID: "project", Scope: ir.ScopeProject, Body: "# Hello\n\nProject rules."},
			Activation: ir.ActAlways,
		},
	}
	plan := New().PlanImport(b, ctx, adapter.ImportOptions{})

	conv := findFile(plan, "CONVENTIONS.md")
	if conv == nil {
		t.Fatalf("expected CONVENTIONS.md in plan; files=%v", paths(plan))
	}
	if !strings.Contains(string(conv.Content), "Project rules.") {
		t.Errorf("CONVENTIONS.md content missing body: %q", conv.Content)
	}
	yml := findFile(plan, ".aider.conf.yml")
	if yml == nil {
		t.Fatalf("expected .aider.conf.yml in plan; files=%v", paths(plan))
	}
	if !strings.Contains(string(yml.Content), "CONVENTIONS.md") {
		t.Errorf(".aider.conf.yml missing read: CONVENTIONS.md: %q", yml.Content)
	}
	if !hasWarning(plan, "instructions", "CONVENTIONS.md") {
		t.Errorf("expected a manual-wiring instructions warning; warnings=%v", plan.Warnings)
	}
}

func TestPlanImportMergesExistingConfYml(t *testing.T) {
	out := t.TempDir()
	existing := "read:\n  - EXISTING.md\nauto-commits: false\n"
	if err := os.WriteFile(filepath.Join(out, ".aider.conf.yml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	b := ir.NewBundle(ir.Source{Tool: "codex"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "rules"}, Activation: ir.ActAlways},
	}
	plan := New().PlanImport(b, ctx, adapter.ImportOptions{})
	yml := findFile(plan, ".aider.conf.yml")
	if yml == nil {
		t.Fatal("expected .aider.conf.yml in plan")
	}
	c := string(yml.Content)
	if !strings.Contains(c, "EXISTING.md") {
		t.Errorf("merge dropped existing read entry: %q", c)
	}
	if !strings.Contains(c, "CONVENTIONS.md") {
		t.Errorf("merge did not add CONVENTIONS.md: %q", c)
	}
	if strings.Count(c, "auto-commits") != 1 {
		t.Errorf("expected existing auto-commits key preserved once: %q", c)
	}
	if yml.Mode != ir.ModeOverwrite {
		t.Errorf("expected ModeOverwrite for existing conf, got %v", yml.Mode)
	}
}

func TestPlanImportMemoryUnsupported(t *testing.T) {
	out := t.TempDir()
	ctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "user-memory", Scope: ir.ScopeUser, Body: "Personal memory body"}, Activation: ir.ActAlways},
	}
	plan := New().PlanImport(b, ctx, adapter.ImportOptions{})
	if !hasWarning(plan, "memory", "") {
		t.Fatalf("expected a memory warning for user-scope instruction; warnings=%v", plan.Warnings)
	}
	// Memory body must be preserved into CONVENTIONS.md (not silently dropped).
	conv := findFile(plan, "CONVENTIONS.md")
	if conv == nil || !strings.Contains(string(conv.Content), "Personal memory body") {
		t.Errorf("expected memory body preserved in CONVENTIONS.md; conv=%v", conv)
	}
}

// TestUnsupportedCategoriesWarn feeds a bundle containing every category and
// asserts each unsupported category produces a warning, and skill bodies are
// preserved into CONVENTIONS.md.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	out := t.TempDir()
	ctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "proj instructions"}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "fs", Transport: ir.TransportStdio, Command: "mcp-fs"},
	}
	b.Skills = []ir.Skill{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "skill body text"}, Name: "deploy", Description: "deploy helper"},
	}
	b.Commands = []ir.Command{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "cmd body"}, Name: "review", Description: "review cmd"},
	}
	b.Subagents = []ir.Subagent{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "agent body"}, Name: "planner", SystemPrompt: "plan things"},
	}

	plan := New().PlanImport(b, ctx, adapter.ImportOptions{})

	for _, cat := range []string{"mcp", "skills", "commands", "subagents"} {
		if !hasWarning(plan, cat, "") {
			t.Errorf("expected a warning for unsupported category %q; warnings=%v", cat, plan.Warnings)
		}
	}
	// Skill body preserved into CONVENTIONS.md.
	conv := findFile(plan, "CONVENTIONS.md")
	if conv == nil {
		t.Fatalf("expected CONVENTIONS.md in plan; files=%v", paths(plan))
	}
	if !strings.Contains(string(conv.Content), "skill body text") {
		t.Errorf("skill body not preserved in CONVENTIONS.md: %q", conv.Content)
	}
}

func TestPlanImportRespectsCategoryGating(t *testing.T) {
	out := t.TempDir()
	ctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{{Common: ir.Common{Scope: ir.ScopeProject}, Name: "fs"}}
	// Only request instructions; mcp must not warn because it's not wanted.
	plan := New().PlanImport(b, ctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	if hasWarning(plan, "mcp", "") {
		t.Errorf("did not request mcp; should not warn; warnings=%v", plan.Warnings)
	}
}

func paths(plan ir.WritePlan) []string {
	var out []string
	for _, f := range plan.Files {
		out = append(out, f.Path)
	}
	return out
}
