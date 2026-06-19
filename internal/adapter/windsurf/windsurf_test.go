package windsurf

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func fromCtx() ir.Context {
	return ir.Context{ProjectPath: "testdata/from", HomeDir: "testdata/home"}
}

func TestMetaAndCapabilities(t *testing.T) {
	a := New()
	m := a.Meta()
	if m.ID != "windsurf" || m.DisplayName != "Windsurf" || m.Vendor != "Codeium" || m.Confidence != "medium" {
		t.Fatalf("meta=%+v", m)
	}
	c := a.Capabilities()
	if c.Skills {
		t.Fatalf("windsurf has no skills")
	}
	if c.Subagents != "false" {
		t.Fatalf("subagents=%q", c.Subagents)
	}
	if c.Memory != ir.MemoryFile {
		t.Fatalf("memory=%q", c.Memory)
	}
	if c.MCP.ProjectScope {
		t.Fatalf("mcp is global-only")
	}
	if c.MCP.RemoteURLKey != "serverUrl" || c.MCP.RootKey != "mcpServers" {
		t.Fatalf("mcp keys=%+v", c.MCP)
	}
	if c.Instructions.CharBudget != charBudget {
		t.Fatalf("charbudget=%d", c.Instructions.CharBudget)
	}
}

func TestDetect(t *testing.T) {
	res := New().Detect(fromCtx())
	if !res.Present {
		t.Fatalf("expected windsurf present")
	}
	if len(res.ScopesFound) == 0 {
		t.Fatalf("expected scopes")
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx())
	if err != nil {
		t.Fatal(err)
	}
	// 4 rules + legacy .windsurfrules (project) + 1 home memory = 6
	byID := map[string]ir.Instruction{}
	for _, in := range ins {
		byID[filepath.Base(in.Origin)] = in
	}
	if got := byID["style.md"].Activation; got != ir.ActGlob {
		t.Fatalf("style activation=%q", got)
	}
	if len(byID["style.md"].Globs) != 2 {
		t.Fatalf("style globs=%+v", byID["style.md"].Globs)
	}
	if got := byID["always.md"].Activation; got != ir.ActAlways {
		t.Fatalf("always activation=%q", got)
	}
	if got := byID["decide.md"].Activation; got != ir.ActModelDecision {
		t.Fatalf("decide activation=%q", got)
	}
	if got := byID["manual.md"].Activation; got != ir.ActManual {
		t.Fatalf("manual activation=%q", got)
	}
	// legacy .windsurfrules => project, always
	legacy := byID[".windsurfrules"]
	if legacy.Activation != ir.ActAlways || legacy.Scope != ir.ScopeProject {
		t.Fatalf("legacy=%+v", legacy)
	}
	// home global_rules.md => user scope == memory
	mem := byID["global_rules.md"]
	if !mem.IsMemory() {
		t.Fatalf("global_rules should be memory, got scope=%q", mem.Scope)
	}
}

func TestExportMcpGlobalOnly(t *testing.T) {
	servers, err := New().ExportMcpServers(fromCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers=%+v", servers)
	}
	for _, s := range servers {
		if s.Scope != ir.ScopeUser {
			t.Fatalf("server %q scope=%q (expected user/global)", s.Name, s.Scope)
		}
	}
	byName := map[string]ir.McpServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if byName["context7"].Command != "npx" {
		t.Fatalf("context7=%+v", byName["context7"])
	}
	if byName["remote-search"].URL == "" {
		t.Fatalf("remote-search url empty: %+v", byName["remote-search"])
	}
}

func TestExportSkillsAndSubagentsAreNil(t *testing.T) {
	a := New()
	skills, _ := a.ExportSkills(fromCtx())
	if skills != nil {
		t.Fatalf("expected nil skills, got %+v", skills)
	}
	subs, _ := a.ExportSubagents(fromCtx())
	if subs != nil {
		t.Fatalf("expected nil subagents, got %+v", subs)
	}
}

func TestExportCommands(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "deploy" {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].ArgSpec.Style != ir.ArgAll {
		t.Fatalf("argspec=%+v", cmds[0].ArgSpec)
	}
	if len(cmds[0].ShellInjections) != 1 || cmds[0].ShellInjections[0] != "git status" {
		t.Fatalf("shell=%+v", cmds[0].ShellInjections)
	}
	if cmds[0].Scope != ir.ScopeProject {
		t.Fatalf("scope=%q", cmds[0].Scope)
	}
}

func TestExportProjectStateZero(t *testing.T) {
	ps, err := New().ExportProjectState(fromCtx())
	if err != nil {
		t.Fatal(err)
	}
	if ps.Trust != "" || len(ps.Permissions.Allow) != 0 || len(ps.Hooks) != 0 {
		t.Fatalf("expected zero project state, got %+v", ps)
	}
}

// PlanImport: round trip from windsurf -> IR -> windsurf produces the right paths.
func TestPlanImportTargetPaths(t *testing.T) {
	a := New()
	src := fromCtx()
	b := ir.NewBundle(ir.Source{Tool: "windsurf"})
	b.Instructions, _ = a.ExportInstructions(src)
	b.McpServers, _ = a.ExportMcpServers(src)
	b.Commands, _ = a.ExportCommands(src)

	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	paths := map[string]bool{}
	for _, f := range plan.Files {
		rel, _ := filepath.Rel(out, f.Path)
		paths[rel] = true
		hrel, _ := filepath.Rel(home, f.Path)
		paths[hrel] = true
	}
	// project rules (slug derived from the instruction ID, which export prefixes)
	if !paths[filepath.Join(".windsurf", "rules", "project-always.md")] {
		t.Fatalf("expected always rule file, paths=%v", paths)
	}
	// command -> workflow
	if !paths[filepath.Join(".windsurf", "workflows", "deploy.md")] {
		t.Fatalf("expected deploy workflow, paths=%v", paths)
	}
	// global mcp
	if !paths[filepath.Join(".codeium", "windsurf", "mcp_config.json")] {
		t.Fatalf("expected global mcp_config.json, paths=%v", paths)
	}
	// memory global_rules
	if !paths[filepath.Join(".codeium", "windsurf", "memories", "global_rules.md")] {
		t.Fatalf("expected global_rules.md memory file, paths=%v", paths)
	}
}

// Verify the trigger frontmatter is rendered correctly per activation.
func TestPlanImportRuleTriggers(t *testing.T) {
	a := New()
	out := t.TempDir()
	home := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: "src"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "g", Scope: ir.ScopeProject, Body: "glob body"}, Activation: ir.ActGlob, Globs: []string{"**/*.go"}},
		{Common: ir.Common{ID: "a", Scope: ir.ScopeProject, Body: "always body"}, Activation: ir.ActAlways},
		{Common: ir.Common{ID: "m", Scope: ir.ScopeProject, Body: "manual body"}, Activation: ir.ActManual},
		{Common: ir.Common{ID: "d", Scope: ir.ScopeProject, Body: "decide body"}, Activation: ir.ActModelDecision},
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	content := map[string]string{}
	for _, f := range plan.Files {
		content[filepath.Base(f.Path)] = string(f.Content)
	}
	if !strings.Contains(content["g.md"], "trigger: glob") || !strings.Contains(content["g.md"], "**/*.go") {
		t.Fatalf("glob rule:\n%s", content["g.md"])
	}
	if !strings.Contains(content["a.md"], "trigger: always_on") {
		t.Fatalf("always rule:\n%s", content["a.md"])
	}
	if !strings.Contains(content["m.md"], "trigger: manual") {
		t.Fatalf("manual rule:\n%s", content["m.md"])
	}
	if !strings.Contains(content["d.md"], "trigger: model_decision") {
		t.Fatalf("decide rule:\n%s", content["d.md"])
	}
}

func TestPlanImportCharCapWarns(t *testing.T) {
	a := New()
	out := t.TempDir()
	big := strings.Repeat("x", charBudget+500)
	b := ir.NewBundle(ir.Source{Tool: "src"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "big", Scope: ir.ScopeProject, Body: big}, Activation: ir.ActAlways},
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	var capWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "instructions" && strings.Contains(w.Reason, "char cap") {
			capWarn = true
		}
	}
	if !capWarn {
		t.Fatalf("expected char-cap warning, warnings=%+v", plan.Warnings)
	}
	// body truncated under cap
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "big.md") {
			if len(f.Content) > charBudget+500 {
				t.Fatalf("expected truncation, len=%d", len(f.Content))
			}
		}
	}
}

func TestPlanImportProjectMcpWarnsGlobalOnly(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "proj", Command: "npx", Transport: ir.TransportStdio, Enabled: true},
	}
	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	var found bool
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && strings.Contains(w.Reason, "global-only") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected global-only mcp warning, warnings=%+v", plan.Warnings)
	}
	// still wrote a global config
	var wrote bool
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "mcp_config.json") {
			wrote = true
		}
	}
	if !wrote {
		t.Fatalf("expected mcp_config.json to be written despite warning")
	}
}

func TestPlanImportMemoryWhenRequested(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "m", Scope: ir.ScopeUser, Body: "personal memory body"}, Activation: ir.ActAlways},
		{Common: ir.Common{ID: "p", Scope: ir.ScopeProject, Body: "project body"}, Activation: ir.ActAlways},
	}
	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}

	// Only memory requested -> writes global_rules.md, NOT the project rule.
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	var mem, proj bool
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, filepath.Join("memories", "global_rules.md")) {
			mem = true
			if !strings.Contains(string(f.Content), "personal memory body") {
				t.Fatalf("memory content missing: %s", f.Content)
			}
		}
		if strings.Contains(f.Path, filepath.Join(".windsurf", "rules")) {
			proj = true
		}
	}
	if !mem {
		t.Fatalf("expected global_rules.md when memory requested")
	}
	if proj {
		t.Fatalf("project rule should NOT be written when only memory requested")
	}
}

// Project-state ignore patterns must be written to .codeiumignore (honoring the
// declared Ignore:"both" capability), and an index-only mode must collapse to the
// single block ignore with a structured warning instead of being silently lost.
func TestPlanImportIgnoreFileWritten(t *testing.T) {
	a := New()
	out := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{
		IgnorePatterns: []string{"secret.txt", "build/", "node_modules/"},
		IgnoreMode:     ir.IgnoreIndex,
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var content string
	var wrote bool
	for _, f := range plan.Files {
		if filepath.Base(f.Path) == ".codeiumignore" {
			wrote = true
			content = string(f.Content)
			if rel, _ := filepath.Rel(out, f.Path); rel != ".codeiumignore" {
				t.Fatalf(".codeiumignore written outside project root: %s", f.Path)
			}
		}
	}
	if !wrote {
		t.Fatalf("expected .codeiumignore to be written, files=%+v", plan.Files)
	}
	for _, want := range []string{"secret.txt", "build/", "node_modules/"} {
		if !strings.Contains(content, want) {
			t.Fatalf("ignore file missing %q:\n%s", want, content)
		}
	}
	// index-only mode must collapse with a warning (never silently dropped).
	var collapseWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "ignore" && strings.Contains(w.Reason, "collapsed") {
			collapseWarn = true
		}
	}
	if !collapseWarn {
		t.Fatalf("expected index-only collapse warning, warnings=%+v", plan.Warnings)
	}
}

// A block-mode ignore writes the file but must NOT emit a collapse warning.
func TestPlanImportIgnoreBlockNoCollapseWarn(t *testing.T) {
	a := New()
	out := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{
		IgnorePatterns: []string{"dist/"},
		IgnoreMode:     ir.IgnoreBlock,
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})
	for _, w := range plan.Warnings {
		if w.Category == "ignore" {
			t.Fatalf("block-mode ignore should not warn, got %+v", w)
		}
	}
	var wrote bool
	for _, f := range plan.Files {
		if filepath.Base(f.Path) == ".codeiumignore" {
			wrote = true
		}
	}
	if !wrote {
		t.Fatalf("expected .codeiumignore for block-mode ignore")
	}
}

// Project-state permissions/hooks/trust must each emit a structured warning
// instead of being silently dropped (spec §7 "never silently drop").
func TestPlanImportProjectStateWarns(t *testing.T) {
	a := New()
	from := "claude-code"
	out := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: from})
	b.ProjectState = ir.ProjectState{
		Trust:       "trusted",
		Permissions: ir.Permissions{Allow: []string{"Bash(ls)"}, Deny: []string{"Bash(rm)"}},
		Hooks:       []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}},
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"project-state": true}})

	var perm, hooks, trust bool
	for _, w := range plan.Warnings {
		if w.Category != "project-state" {
			continue
		}
		switch w.Artifact {
		case "permissions":
			perm = w.Action == ir.ActionSkip
		case "hooks":
			hooks = w.Action == ir.ActionSkip
		case "trust":
			trust = w.Action == ir.ActionManual
		}
	}
	if !perm {
		t.Errorf("expected permissions skip warning, warnings=%+v", plan.Warnings)
	}
	if !hooks {
		t.Errorf("expected hooks skip warning, warnings=%+v", plan.Warnings)
	}
	if !trust {
		t.Errorf("expected trust manual warning, warnings=%+v", plan.Warnings)
	}
}

// Project-state is only touched when its category is requested.
func TestPlanImportProjectStateGatedByOpts(t *testing.T) {
	a := New()
	out := t.TempDir()
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.ProjectState = ir.ProjectState{
		IgnorePatterns: []string{"x/"},
		Permissions:    ir.Permissions{Deny: []string{"y"}},
	}
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	// instructions-only: no project-state work should happen.
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	for _, f := range plan.Files {
		if filepath.Base(f.Path) == ".codeiumignore" {
			t.Fatalf(".codeiumignore must not be written without project-state opt")
		}
	}
	for _, w := range plan.Warnings {
		if w.Category == "project-state" || w.Category == "ignore" {
			t.Fatalf("no project-state warnings expected, got %+v", w)
		}
	}
}

// THE critical test: every category the bundle carries that windsurf can't
// natively represent must produce a structured warning.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	from := "claude-code"
	b := ir.NewBundle(ir.Source{Tool: from})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "i", Scope: ir.ScopeProject, Body: "inst"}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "m", Command: "npx", Transport: ir.TransportStdio, Enabled: true},
	}
	b.Skills = []ir.Skill{
		{Common: ir.Common{ID: "sk", Body: "skill body content"}, Name: "myskill", Description: "does things"},
	}
	b.Commands = []ir.Command{
		{Common: ir.Common{ID: "c", Body: "cmd body"}, Name: "mycmd"},
	}
	b.Subagents = []ir.Subagent{
		{Common: ir.Common{ID: "s"}, Name: "myagent", SystemPrompt: "agent prompt"},
	}
	out := t.TempDir()
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	cats := map[string]bool{}
	for _, w := range plan.Warnings {
		cats[w.Category] = true
	}
	// windsurf has NO skills and NO subagents -> must warn for each.
	if !cats["skills"] {
		t.Errorf("expected skills warning; warnings=%+v", plan.Warnings)
	}
	if !cats["subagents"] {
		t.Errorf("expected subagents warning; warnings=%+v", plan.Warnings)
	}

	// skills must ALSO be preserved as a rules file (never dropped).
	var skillPreserved bool
	for _, f := range plan.Files {
		if strings.Contains(f.Path, filepath.Join(".windsurf", "rules")) &&
			strings.Contains(string(f.Content), "skill body content") {
			skillPreserved = true
		}
	}
	if !skillPreserved {
		t.Errorf("skill body must be preserved as a rules file; files=%+v", plan.Files)
	}
}
