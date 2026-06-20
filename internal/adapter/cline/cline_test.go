package cline

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
	m := a.Meta()
	if m.ID != "cline" || m.DisplayName != "Cline" || m.Vendor != "Cline" || m.Confidence != "medium" {
		t.Fatalf("meta=%+v", m)
	}
	c := a.Capabilities()
	if c.Instructions.Imports {
		t.Fatal("cline instructions have no imports")
	}
	if c.MCP.ProjectScope {
		t.Fatal("cline MCP is global-only (ProjectScope must be false)")
	}
	if c.MCP.RootKey != "mcpServers" || c.MCP.RemoteURLKey != "url" || c.MCP.Format != "json" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
	if c.MCP.SecretStyle != ir.SecretInline {
		t.Fatalf("secret style=%s", c.MCP.SecretStyle)
	}
	if !c.Skills {
		t.Fatal("cline supports skills")
	}
	if !c.Commands.Supported || c.Commands.Format != "md" {
		t.Fatalf("commands caps=%+v", c.Commands)
	}
	if c.Subagents != "false" {
		t.Fatalf("subagents=%q", c.Subagents)
	}
	if c.Memory != ir.MemoryFile {
		t.Fatalf("memory=%s", c.Memory)
	}
	if c.Ignore != "both" {
		t.Fatalf("ignore=%s", c.Ignore)
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	// .clinerules/coding-style.md + AGENTS.md, both project-scope ActAlways.
	var sawRules, sawAgents bool
	for _, in := range ins {
		if in.Activation != ir.ActAlways {
			t.Fatalf("activation=%s for %s", in.Activation, in.Origin)
		}
		if in.Scope != ir.ScopeProject {
			t.Fatalf("scope=%s for %s", in.Scope, in.Origin)
		}
		if strings.HasSuffix(in.Origin, filepath.Join(".clinerules", "coding-style.md")) {
			sawRules = true
			if !strings.Contains(in.Body, "idiomatic Go") {
				t.Fatalf("rules body=%q", in.Body)
			}
		}
		if strings.HasSuffix(in.Origin, "AGENTS.md") {
			sawAgents = true
		}
	}
	if !sawRules || !sawAgents {
		t.Fatalf("rules=%v agents=%v ins=%+v", sawRules, sawAgents, ins)
	}
}

func TestExportInstructionsHomeMemory(t *testing.T) {
	home := t.TempDir()
	rulesDir := filepath.Join(home, "Documents", "Cline", "Rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "global.md"), []byte("personal memory body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ins, err := New().ExportInstructions(ir.Context{ProjectPath: "testdata/from", HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	var memCount int
	for _, in := range ins {
		if in.IsMemory() {
			memCount++
			if in.Scope != ir.ScopeUser {
				t.Fatalf("memory scope=%s", in.Scope)
			}
		}
	}
	if memCount != 1 {
		t.Fatalf("expected 1 user-scope memory instruction, got %d (%+v)", memCount, ins)
	}
}

func TestExportMcpGlobalOnly(t *testing.T) {
	home := t.TempDir()
	clineDir := filepath.Join(home, ".cline")
	if err := os.MkdirAll(clineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{
  "mcpServers": {
    "ctx7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"],
      "disabled": false,
      "autoApprove": ["search"],
      "timeout": 60
    }
  }
}`
	if err := os.WriteFile(filepath.Join(clineDir, "cline_mcp_settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	servers, err := New().ExportMcpServers(ir.Context{ProjectPath: "testdata/from", HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Name != "ctx7" || servers[0].Command != "npx" {
		t.Fatalf("servers=%+v", servers)
	}
	if servers[0].Scope != ir.ScopeUser {
		t.Fatalf("global MCP must be user scope, got %s", servers[0].Scope)
	}
	if len(servers[0].AutoApprove) != 1 || servers[0].Timeout != 60 {
		t.Fatalf("autoApprove/timeout lost: %+v", servers[0])
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
	if _, ok := byName["research"]; !ok {
		t.Fatalf("research skill missing: %+v", skills)
	}
	if _, ok := byName["demo"]; !ok {
		t.Fatalf("demo skill missing: %+v", skills)
	}
	res := byName["research"]
	var sawResource bool
	for _, r := range res.Resources {
		if r.RelPath == "reference.md" {
			sawResource = true
		}
	}
	if !sawResource {
		t.Fatalf("research resource not bundled: %+v", res.Resources)
	}
}

func TestExportCommandsWorkflows(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "deploy" {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].InvocationFormat != "/deploy.md" {
		t.Fatalf("invocation=%q", cmds[0].InvocationFormat)
	}
	if cmds[0].ArgSpec.Style != ir.ArgAll {
		t.Fatalf("argspec=%+v", cmds[0].ArgSpec)
	}
	if len(cmds[0].ShellInjections) != 1 || cmds[0].ShellInjections[0] != "kubectl get pods" {
		t.Fatalf("shell=%+v", cmds[0].ShellInjections)
	}
}

func TestExportSubagentsNone(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil || subs != nil {
		t.Fatalf("expected no subagents, got %v %v", subs, err)
	}
}

func TestExportProjectStateZero(t *testing.T) {
	ps, err := New().ExportProjectState(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if ps.Trust != "" || len(ps.Permissions.Allow) != 0 || len(ps.Hooks) != 0 {
		t.Fatalf("expected zero project state, got %+v", ps)
	}
}

func TestPlanImportInstructionsAndMemory(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "proj", Body: "project body", Scope: ir.ScopeProject}},
		{Common: ir.Common{ID: "mem", Body: "memory body", Scope: ir.ScopeUser}},
	}
	out := t.TempDir()
	home := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: home}, adapter.ImportOptions{})

	var sawProj, sawMem bool
	for _, f := range plan.Files {
		if strings.HasPrefix(f.Path, filepath.Join(out, ".clinerules")) && strings.HasSuffix(f.Path, ".md") {
			sawProj = true
			if !strings.Contains(string(f.Content), "project body") {
				t.Fatalf("project instruction content=%q", f.Content)
			}
		}
		if strings.HasPrefix(f.Path, filepath.Join(home, "Documents", "Cline", "Rules")) {
			sawMem = true
			if !strings.Contains(string(f.Content), "memory body") {
				t.Fatalf("memory content=%q", f.Content)
			}
		}
	}
	if !sawProj {
		t.Fatalf("project instruction not planned to .clinerules: %+v", plan.Files)
	}
	if !sawMem {
		t.Fatalf("memory not planned to ~/Documents/Cline/Rules: %+v", plan.Files)
	}
}

func TestPlanImportMcpGlobalWarnsOnProjectScope(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeUser}, Name: "global1", Transport: ir.TransportStdio, Command: "npx", Enabled: true},
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "projsrv", Transport: ir.TransportStdio, Command: "node", Enabled: true},
	}
	home := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: t.TempDir(), HomeDir: home}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})

	// Both servers should land in the single global settings file.
	var settings string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(home, ".cline", "cline_mcp_settings.json") {
			settings = string(f.Content)
		}
	}
	if settings == "" {
		t.Fatalf("global mcp settings not planned: %+v", plan.Files)
	}
	if !strings.Contains(settings, "global1") || !strings.Contains(settings, "projsrv") {
		t.Fatalf("both servers should be in global settings:\n%s", settings)
	}
	// project-scope server must produce a merge warning about losing isolation.
	var sawWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && w.Artifact == "projsrv" && w.Action == ir.ActionMerge {
			sawWarn = true
			if !strings.Contains(w.Reason, "global-only") {
				t.Fatalf("warn reason=%q", w.Reason)
			}
		}
	}
	if !sawWarn {
		t.Fatalf("expected project-scope merge warning, got %+v", plan.Warnings)
	}
}

func TestPlanImportSkillsAndCommands(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Skills = []ir.Skill{{
		Common:    ir.Common{ID: "research", Body: "skill body"},
		Name:      "research",
		Resources: []ir.FileRef{{RelPath: "reference.md", Bytes: []byte("ref")}},
	}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "do it"}, Name: "deploy", Description: "Deploy"}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{})

	want := map[string]bool{
		filepath.Join(out, ".clinerules", "skills", "research", "SKILL.md"):     false,
		filepath.Join(out, ".clinerules", "skills", "research", "reference.md"): false,
		filepath.Join(out, ".clinerules", "workflows", "deploy.md"):             false,
	}
	for _, f := range plan.Files {
		if _, ok := want[f.Path]; ok {
			want[f.Path] = true
		}
	}
	for p, got := range want {
		if !got {
			t.Errorf("expected planned file %s", p)
		}
	}
}

// Every category the bundle carries that cline cannot natively represent must
// produce a structured warning. Cline lacks: subagents.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{Common: ir.Common{Body: "i", Scope: ir.ScopeProject}}}
	b.McpServers = []ir.McpServer{{Common: ir.Common{Scope: ir.ScopeUser}, Name: "m", Transport: ir.TransportStdio, Command: "x", Enabled: true}}
	b.Skills = []ir.Skill{{Common: ir.Common{ID: "s", Body: "b"}, Name: "s"}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "c"}, Name: "c"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{ID: "sub"}, Name: "reviewer", SystemPrompt: "review"}}

	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{})

	var sawSubagent bool
	for _, w := range plan.Warnings {
		if w.Category == "subagents" && w.Artifact == "reviewer" && w.Action == ir.ActionSkip {
			sawSubagent = true
		}
	}
	if !sawSubagent {
		t.Fatalf("expected subagent skip warning, got %+v", plan.Warnings)
	}
}

func TestExportProjectStateIgnore(t *testing.T) {
	// .clineignore in testdata must round-trip into ProjectState.IgnorePatterns.
	ps, err := New().ExportProjectState(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if ps.IgnoreMode != ir.IgnoreBlock {
		t.Fatalf("ignore mode=%q", ps.IgnoreMode)
	}
	var sawNodeModules, sawSecrets, sawComment bool
	for _, p := range ps.IgnorePatterns {
		switch p {
		case "node_modules":
			sawNodeModules = true
		case "secrets.env":
			sawSecrets = true
		case "# a comment":
			sawComment = true
		}
	}
	if !sawNodeModules || !sawSecrets {
		t.Fatalf("ignore patterns lost: %+v", ps.IgnorePatterns)
	}
	if sawComment {
		t.Fatalf("comment line should be stripped: %+v", ps.IgnorePatterns)
	}
}

func TestPlanImportIgnoreWritesClineignore(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{
		IgnorePatterns: []string{"dist", "node_modules"},
		IgnoreMode:     ir.IgnoreBlock,
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()},
		adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var content string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".clineignore") {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf(".clineignore not planned: %+v", plan.Files)
	}
	if !strings.Contains(content, "dist") || !strings.Contains(content, "node_modules") {
		t.Fatalf(".clineignore content=%q", content)
	}
}

func TestPlanImportIgnoreIndexModeCollapsesWithWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{
		IgnorePatterns: []string{"build"},
		IgnoreMode:     ir.IgnoreIndex,
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()},
		adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var sawFile, sawWarn bool
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".clineignore") {
			sawFile = true
		}
	}
	for _, w := range plan.Warnings {
		if w.Category == "project-state" && w.Artifact == ".clineignore" && w.Action == ir.ActionMerge {
			sawWarn = true
			if !strings.Contains(w.Reason, "block-only") {
				t.Fatalf("warn reason=%q", w.Reason)
			}
		}
	}
	if !sawFile {
		t.Fatalf("index-only ignore must still be written as block file: %+v", plan.Files)
	}
	if !sawWarn {
		t.Fatalf("index-only collapse must warn: %+v", plan.Warnings)
	}
}

func TestPlanImportProjectStatePermissionsHooksTrustWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.ProjectState = ir.ProjectState{
		Permissions: ir.Permissions{Allow: []string{"Read"}, Deny: []string{"Bash"}},
		Hooks:       []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}},
		Trust:       "trusted",
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()},
		adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var sawPerms, sawHooks, sawTrust bool
	for _, w := range plan.Warnings {
		if w.Category != "project-state" {
			continue
		}
		switch w.Artifact {
		case "permissions":
			if w.Action == ir.ActionSkip {
				sawPerms = true
			}
		case "hooks":
			if w.Action == ir.ActionSkip {
				sawHooks = true
			}
		case "trust":
			if w.Action == ir.ActionManual {
				sawTrust = true
			}
		}
	}
	if !sawPerms || !sawHooks || !sawTrust {
		t.Fatalf("project-state drops must warn: perms=%v hooks=%v trust=%v (%+v)", sawPerms, sawHooks, sawTrust, plan.Warnings)
	}
}

func TestPlanImportInstructionsFlattenImports(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common: ir.Common{
			ID:     "proj",
			Origin: "CLAUDE.md",
			Body:   "before @sub.md after",
			Scope:  ir.ScopeProject,
		},
		Imports: []ir.Import{{Kind: ir.ImpInline, Target: "sub.md", Resolved: "FLATTENED-CONTENT"}},
	}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()},
		adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})

	var body string
	for _, f := range plan.Files {
		if strings.HasPrefix(f.Path, filepath.Join(out, ".clinerules")) && strings.HasSuffix(f.Path, ".md") {
			body = string(f.Content)
		}
	}
	if !strings.Contains(body, "FLATTENED-CONTENT") {
		t.Fatalf("imports not flattened into body: %q", body)
	}
	var sawWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" && w.Action == ir.ActionInline {
			sawWarn = true
			if !strings.Contains(w.Reason, "imports") {
				t.Fatalf("flatten warn reason=%q", w.Reason)
			}
		}
	}
	if !sawWarn {
		t.Fatalf("flattening imports must warn: %+v", plan.Warnings)
	}
}

func TestPlanImportMcpNoHomeDirWarnsNotSilent(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeUser}, Name: "global1", Transport: ir.TransportStdio, Command: "npx", Enabled: true},
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "projsrv", Transport: ir.TransportStdio, Command: "node", Enabled: true},
	}
	out := t.TempDir()
	// HomeDir intentionally empty: servers cannot be written globally.
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: ""},
		adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})

	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "cline_mcp_settings.json") {
			t.Fatalf("no settings file should be written without a home dir: %s", f.Path)
		}
	}
	// Both servers must be reported as skipped (not silently dropped), and the
	// project-scope server must still get its isolation-loss merge warning.
	skipped := map[string]bool{}
	var sawProjMerge bool
	for _, w := range plan.Warnings {
		if w.Category != "mcp" {
			continue
		}
		if w.Action == ir.ActionSkip {
			skipped[w.Artifact] = true
		}
		if w.Artifact == "projsrv" && w.Action == ir.ActionMerge {
			sawProjMerge = true
		}
	}
	if !skipped["global1"] || !skipped["projsrv"] {
		t.Fatalf("both servers must be skip-warned when no home dir: %+v", plan.Warnings)
	}
	if !sawProjMerge {
		t.Fatalf("project-scope isolation warning must still fire: %+v", plan.Warnings)
	}
}

func TestRoundTripExportThenImport(t *testing.T) {
	a := New()
	home := t.TempDir()
	clineDir := filepath.Join(home, ".cline")
	if err := os.MkdirAll(clineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"mcpServers":{"ctx7":{"command":"npx","args":["-y","pkg"]}}}`
	if err := os.WriteFile(filepath.Join(clineDir, "cline_mcp_settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	src := ir.Context{ProjectPath: "testdata/from", HomeDir: home}

	b := ir.NewBundle(ir.Source{Tool: "cline"})
	b.Instructions, _ = a.ExportInstructions(src)
	b.McpServers, _ = a.ExportMcpServers(src)
	b.Skills, _ = a.ExportSkills(src)
	b.Commands, _ = a.ExportCommands(src)
	b.Subagents, _ = a.ExportSubagents(src)
	b.ProjectState, _ = a.ExportProjectState(src)

	out := t.TempDir()
	destHome := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: destHome}, adapter.ImportOptions{})
	res := a.Apply(plan, adapter.ApplyOptions{})
	if len(res.Errors) != 0 {
		t.Fatalf("apply errors: %v", res.Errors)
	}

	mustExist := []string{
		filepath.Join(out, ".clinerules", "workflows", "deploy.md"),
		filepath.Join(out, ".clinerules", "skills", "research", "SKILL.md"),
		filepath.Join(destHome, ".cline", "cline_mcp_settings.json"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
	}
}
