package copilot

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
	return ir.Context{ProjectPath: "testdata/from", HomeDir: "testdata/home"}
}

func TestMetaAndCapabilities(t *testing.T) {
	a := New()
	m := a.Meta()
	if m.ID != "copilot" || m.DisplayName != "GitHub Copilot" || m.Vendor != "GitHub" || m.Confidence != "high" {
		t.Fatalf("meta=%+v", m)
	}
	c := a.Capabilities()
	if c.Memory != ir.MemoryFile {
		t.Fatalf("memory=%v", c.Memory)
	}
	if !c.Skills || !c.Commands.Supported || c.Subagents != "true" || !c.Permissions {
		t.Fatalf("caps=%+v", c)
	}
	if c.MCP.RootKey != "mcpServers" || c.MCP.RemoteURLKey != "url" || c.MCP.Format != "json" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
	if c.Hooks {
		t.Fatalf("hooks should be false")
	}
}

func TestDetect(t *testing.T) {
	res := New().Detect(fromCtx(t))
	if !res.Present {
		t.Fatalf("expected present, got %+v", res)
	}
	if len(res.ScopesFound) == 0 {
		t.Fatalf("expected scopes, got %+v", res)
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	var always, glob, memory int
	var globIns ir.Instruction
	for _, in := range ins {
		switch in.Activation {
		case ir.ActAlways:
			always++
		case ir.ActGlob:
			glob++
			globIns = in
		}
		if in.IsMemory() {
			memory++
		}
	}
	if always < 2 {
		// copilot-instructions.md + AGENTS.md (both project, ActAlways)
		t.Fatalf("expected >=2 always project instructions, got %d (%+v)", always, ins)
	}
	if glob != 1 {
		t.Fatalf("expected 1 glob instruction, got %d", glob)
	}
	if len(globIns.Globs) != 2 || globIns.Globs[0] != "**/*.go" {
		t.Fatalf("globs=%+v", globIns.Globs)
	}
	if memory != 1 {
		t.Fatalf("expected 1 memory (user) instruction, got %d", memory)
	}
}

func TestExportMcpServers(t *testing.T) {
	servers, err := New().ExportMcpServers(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ir.McpServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if s, ok := byName["ctx7"]; !ok || s.Command != "npx" || s.Scope != ir.ScopeProject {
		t.Fatalf("ctx7=%+v ok=%v", s, ok)
	}
	if s, ok := byName["remote-api"]; !ok || s.Transport != ir.TransportHTTP {
		t.Fatalf("remote-api=%+v ok=%v", s, ok)
	}
	if s, ok := byName["user-srv"]; !ok || s.Scope != ir.ScopeUser {
		t.Fatalf("user-srv=%+v ok=%v", s, ok)
	}
}

func TestExportSkills(t *testing.T) {
	skills, err := New().ExportSkills(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ir.Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	if d, ok := byName["demo"]; !ok || d.Scope != ir.ScopeProject {
		t.Fatalf("demo=%+v ok=%v", d, ok)
	}
	if m, ok := byName["mem-skill"]; !ok || m.Scope != ir.ScopeUser {
		t.Fatalf("mem-skill=%+v ok=%v", m, ok)
	}
}

func TestExportCommands(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "foo" {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].ArgSpec.Style != ir.ArgAll {
		t.Fatalf("argspec=%+v", cmds[0].ArgSpec)
	}
	if cmds[0].Description != "Run the foo workflow." {
		t.Fatalf("desc=%q", cmds[0].Description)
	}
}

func TestExportSubagents(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ir.Subagent{}
	for _, s := range subs {
		byName[s.Name] = s
	}
	bar, ok := byName["bar"]
	if !ok {
		t.Fatalf("bar missing: %+v", subs)
	}
	if bar.Scope != ir.ScopeProject || len(bar.Tools) != 2 || bar.Model != "gpt-4o" {
		t.Fatalf("bar=%+v", bar)
	}
	if !strings.Contains(bar.SystemPrompt, "code reviewer") {
		t.Fatalf("system prompt=%q", bar.SystemPrompt)
	}
	if h, ok := byName["helper"]; !ok || h.Scope != ir.ScopeUser {
		t.Fatalf("helper=%+v ok=%v", h, ok)
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
}

func planPaths(plan ir.WritePlan) []string {
	out := make([]string, 0, len(plan.Files))
	for _, f := range plan.Files {
		out = append(out, f.Path)
	}
	return out
}

func hasSuffixPath(paths []string, suffix string) bool {
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

func TestPlanImportTargets(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	b := ir.NewBundle(ir.Source{Tool: "copilot"})
	b.Instructions, _ = a.ExportInstructions(ctx)
	b.McpServers, _ = a.ExportMcpServers(ctx)
	b.Skills, _ = a.ExportSkills(ctx)
	b.Commands, _ = a.ExportCommands(ctx)
	b.Subagents, _ = a.ExportSubagents(ctx)
	b.ProjectState, _ = a.ExportProjectState(ctx)

	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	paths := planPaths(plan)

	want := []string{
		filepath.Join(".github", "copilot-instructions.md"),
		filepath.Join(".github", "instructions", "go.instructions.md"),
		filepath.Join(".github", "mcp.json"),
		filepath.Join(".copilot", "skills", "demo", "SKILL.md"),
		filepath.Join(".github", "prompts", "foo.prompt.md"),
		filepath.Join(".github", "agents", "bar.agent.md"),
		filepath.Join(".copilot", "copilot-instructions.md"),
		filepath.Join(".copilot", "permissions-config.json"),
	}
	for _, w := range want {
		if !hasSuffixPath(paths, w) {
			t.Errorf("expected a planned file ending in %s; got %v", w, paths)
		}
	}

	// glob instruction frontmatter applyTo
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join("instructions", "go.instructions.md")) {
			if !strings.Contains(string(f.Content), "applyTo") || !strings.Contains(string(f.Content), "**/*.go") {
				t.Fatalf("glob instruction missing applyTo: %s", f.Content)
			}
		}
		if strings.HasSuffix(f.Path, filepath.Join(".github", "mcp.json")) {
			if !strings.Contains(string(f.Content), "mcpServers") {
				t.Fatalf("mcp.json root key wrong: %s", f.Content)
			}
		}
	}
}

// MCP always emits the VS Code remap warning and the cloud out-of-scope skip.
func TestMcpAlwaysWarns(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{{
		Common:    ir.Common{Scope: ir.ScopeProject},
		Name:      "srv",
		Transport: ir.TransportStdio,
		Command:   "x",
		Enabled:   true,
	}}
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	var vscode, cloud bool
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && strings.Contains(w.Reason, "servers") {
			vscode = true
		}
		if w.Category == "mcp" && w.Action == ir.ActionSkip && strings.Contains(w.Reason, "cloud") {
			cloud = true
		}
	}
	if !vscode || !cloud {
		t.Fatalf("expected vscode remap + cloud skip warnings, got %+v", plan.Warnings)
	}
}

// Memory: user-scope instructions written to ~/.copilot/copilot-instructions.md
func TestMemoryWrittenToFile(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common:     ir.Common{Scope: ir.ScopeUser, Body: "personal memory body"},
		Activation: ir.ActAlways,
	}}
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	found := false
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join(".copilot", "copilot-instructions.md")) {
			found = true
			if !strings.Contains(string(f.Content), "personal memory body") {
				t.Fatalf("memory body missing: %s", f.Content)
			}
		}
	}
	if !found {
		t.Fatalf("memory file not planned: %+v", planPaths(plan))
	}
}

// Memory with empty HomeDir -> warn, do not silently drop.
func TestMemoryWarnsWhenNoHome(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common:     ir.Common{Scope: ir.ScopeUser, Body: "mem"},
		Activation: ir.ActAlways,
	}}
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: ""}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	if len(plan.Warnings) == 0 {
		t.Fatalf("expected a warning when HomeDir empty")
	}
}

// The critical requirement: every category the bundle carries that copilot
// supports lands somewhere, and categories that require home but lack it warn.
// Copilot supports all standard categories, so this test asserts that a fully
// populated bundle produces no silent drops for skills (which need a home).
func TestSkillsWarnWhenNoHome(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Skills = []ir.Skill{{
		Common: ir.Common{ID: "s1", Body: "skill body"},
		Name:   "s1",
	}}
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: ""}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"skills": true}})
	if len(plan.Warnings) == 0 {
		t.Fatalf("expected a warning when skills cannot be written (no home)")
	}
}

// Feed a bundle with every category and confirm a full round of PlanImport
// produces files for each supported category and warnings where lossy.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{Scope: ir.ScopeProject, Body: "proj"}, Activation: ir.ActAlways},
		{Common: ir.Common{Scope: ir.ScopeUser, Body: "mem"}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{{
		Common: ir.Common{Scope: ir.ScopeProject}, Name: "srv",
		Transport: ir.TransportStdio, Command: "x", Enabled: true,
	}}
	b.Skills = []ir.Skill{{Common: ir.Common{ID: "s1", Body: "b"}, Name: "s1"}}
	b.Commands = []ir.Command{{Common: ir.Common{ID: "c1", Body: "do $ARGUMENTS"}, Name: "c1", Description: "d"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{ID: "a1"}, Name: "a1", SystemPrompt: "sp"}}
	b.ProjectState = ir.ProjectState{Permissions: ir.Permissions{Allow: []string{"X"}}}

	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	paths := planPaths(plan)

	// every supported category must produce at least one file
	checks := map[string]string{
		"instructions": filepath.Join(".github", "copilot-instructions.md"),
		"memory":       filepath.Join(".copilot", "copilot-instructions.md"),
		"mcp":          filepath.Join(".github", "mcp.json"),
		"skills":       filepath.Join(".copilot", "skills", "s1", "SKILL.md"),
		"commands":     filepath.Join(".github", "prompts", "c1.prompt.md"),
		"subagents":    filepath.Join(".github", "agents", "a1.agent.md"),
		"permissions":  filepath.Join(".copilot", "permissions-config.json"),
	}
	for cat, suffix := range checks {
		if !hasSuffixPath(paths, suffix) {
			t.Errorf("category %s: expected file ending %s; got %v", cat, suffix, paths)
		}
	}
	// mcp always warns (vscode + cloud)
	if len(plan.Warnings) < 2 {
		t.Fatalf("expected at least the two mcp warnings, got %+v", plan.Warnings)
	}

	// Apply should succeed end to end.
	res := a.Apply(plan, adapter.ApplyOptions{})
	if len(res.Errors) != 0 {
		t.Fatalf("apply errors: %v", res.Errors)
	}
	if _, err := os.Stat(filepath.Join(out, ".github", "mcp.json")); err != nil {
		t.Fatalf("mcp.json not written: %v", err)
	}
}

// Imports must be flattened inline (Capabilities.Imports==false) and a warning
// emitted — never written verbatim, never dropped.
func TestInstructionImportsFlattenedAndWarned(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common: ir.Common{
			Scope:  ir.ScopeProject,
			Origin: "CLAUDE.md",
			Body:   "see @shared.md for rules",
		},
		Activation: ir.ActAlways,
		Imports: []ir.Import{{
			Kind:     ir.ImpInline,
			Target:   "shared.md",
			Resolved: "FLATTENED IMPORT CONTENT",
		}},
	}}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})

	var found bool
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join(".github", "copilot-instructions.md")) {
			found = true
			if !strings.Contains(string(f.Content), "FLATTENED IMPORT CONTENT") {
				t.Fatalf("import not flattened into body: %s", f.Content)
			}
			if strings.Contains(string(f.Content), "@shared.md") {
				t.Fatalf("raw import marker still present: %s", f.Content)
			}
		}
	}
	if !found {
		t.Fatalf("copilot-instructions.md not planned: %v", planPaths(plan))
	}
	var warned bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" && w.Action == ir.ActionInline && strings.Contains(w.Reason, "flattened") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected an instructions flatten warning, got %+v", plan.Warnings)
	}
}

// Inline MCP secrets (env values and Authorization headers) must be externalized
// to env-var refs + a .env stub; the rendered mcp.json must not contain the
// plaintext, and a manual warning must be emitted per externalized secret.
func TestMcpInlineSecretExternalized(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	const envSecret = "sk-envvalue0123456789abcdef0123456789"
	const hdrSecret = "ghp_0123456789abcdefghij0123456789abcdef"
	b.McpServers = []ir.McpServer{
		{
			Common:    ir.Common{Scope: ir.ScopeProject},
			Name:      "stdio-srv",
			Transport: ir.TransportStdio,
			Command:   "x",
			Env:       map[string]string{"API_KEY": envSecret, "PLAIN": "keepme"},
			Enabled:   true,
		},
		{
			Common:    ir.Common{Scope: ir.ScopeProject},
			Name:      "http-srv",
			Transport: ir.TransportHTTP,
			URL:       "https://example.com",
			Headers:   map[string]string{"Authorization": "Bearer " + hdrSecret},
			Enabled:   true,
		},
	}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})

	var mcpJSON, envFile string
	for _, f := range plan.Files {
		switch {
		case strings.HasSuffix(f.Path, filepath.Join(".github", "mcp.json")):
			mcpJSON = string(f.Content)
		case strings.HasSuffix(f.Path, ".env"):
			envFile = string(f.Content)
		}
	}
	if mcpJSON == "" {
		t.Fatalf("mcp.json not planned: %v", planPaths(plan))
	}
	if strings.Contains(mcpJSON, envSecret) || strings.Contains(mcpJSON, hdrSecret) {
		t.Fatalf("mcp.json must not contain plaintext secret:\n%s", mcpJSON)
	}
	if !strings.Contains(mcpJSON, "${") {
		t.Fatalf("mcp.json should reference an env var:\n%s", mcpJSON)
	}
	if !strings.Contains(mcpJSON, "keepme") {
		t.Fatalf("non-secret env value should be preserved:\n%s", mcpJSON)
	}
	if envFile == "" {
		t.Fatalf(".env stub not planned: %v", planPaths(plan))
	}
	if !strings.Contains(envFile, envSecret) || !strings.Contains(envFile, hdrSecret) {
		t.Fatalf(".env should hold the secrets:\n%s", envFile)
	}
	var secretWarns int
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && w.Action == ir.ActionManual && strings.Contains(w.Reason, "secret") {
			secretWarns++
		}
	}
	if secretWarns < 2 {
		t.Fatalf("expected a manual secret warning per externalized secret, got %+v", plan.Warnings)
	}
}

// ProjectState hooks and trust have no copilot representation: never drop them
// silently — each must emit a structured warning.
func TestProjectStateHooksAndTrustWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.ProjectState = ir.ProjectState{
		Hooks: []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}},
		Trust: "trusted",
	}
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var hooksWarn, trustWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "hooks" && strings.Contains(w.Reason, "hooks") {
			hooksWarn = true
		}
		if w.Category == "project-state" && w.Artifact == "trust" {
			trustWarn = true
		}
	}
	if !hooksWarn {
		t.Fatalf("expected a hooks warning, got %+v", plan.Warnings)
	}
	if !trustWarn {
		t.Fatalf("expected a trust warning, got %+v", plan.Warnings)
	}
}
