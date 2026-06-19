package cursor

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
	if a.Meta().ID != "cursor" {
		t.Fatalf("id=%q", a.Meta().ID)
	}
	if a.Meta().DisplayName != "Cursor" || a.Meta().Vendor != "Anysphere" {
		t.Fatalf("meta=%+v", a.Meta())
	}
	c := a.Capabilities()
	if c.Memory != ir.MemoryUI {
		t.Fatalf("memory=%s", c.Memory)
	}
	if !c.Skills || !c.Commands.Supported || c.Subagents != "true" {
		t.Fatalf("caps=%+v", c)
	}
	if c.MCP.RootKey != "mcpServers" || c.MCP.RemoteURLKey != "url" || c.MCP.Format != "json" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
	if c.HomeKeying != ir.HomeKeyHash || c.Ignore != "both" {
		t.Fatalf("caps=%+v", c)
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
	byActivation := map[ir.Activation]int{}
	var agentsBody, cursorrulesBody string
	var globRule, modelRule ir.Instruction
	for _, in := range ins {
		byActivation[in.Activation]++
		if strings.HasSuffix(in.Origin, "AGENTS.md") {
			agentsBody = in.Body
		}
		if strings.HasSuffix(in.Origin, ".cursorrules") {
			cursorrulesBody = in.Body
		}
		if in.Activation == ir.ActGlob {
			globRule = in
		}
		if in.Activation == ir.ActModelDecision {
			modelRule = in
		}
	}
	if !strings.Contains(agentsBody, "idiomatic Go") {
		t.Fatalf("AGENTS.md body=%q", agentsBody)
	}
	if !strings.Contains(cursorrulesBody, "Legacy cursor") {
		t.Fatalf(".cursorrules body=%q", cursorrulesBody)
	}
	// always = AGENTS.md + .cursorrules + always-on.mdc
	if byActivation[ir.ActAlways] < 3 {
		t.Fatalf("expected >=3 always, got %d (%+v)", byActivation[ir.ActAlways], byActivation)
	}
	if byActivation[ir.ActGlob] != 1 {
		t.Fatalf("expected 1 glob rule, got %d", byActivation[ir.ActGlob])
	}
	if len(globRule.Globs) != 2 {
		t.Fatalf("glob rule globs=%+v", globRule.Globs)
	}
	if byActivation[ir.ActModelDecision] != 1 {
		t.Fatalf("expected 1 model-decision rule, got %d", byActivation[ir.ActModelDecision])
	}
	if !strings.Contains(modelRule.Body, "transaction") {
		t.Fatalf("model rule body=%q", modelRule.Body)
	}
	if byActivation[ir.ActManual] != 1 {
		t.Fatalf("expected 1 manual rule, got %d", byActivation[ir.ActManual])
	}
}

func TestExportMcp(t *testing.T) {
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
	if byName["ctx7"].Scope != ir.ScopeProject {
		t.Fatalf("scope=%s", byName["ctx7"].Scope)
	}
	if byName["figma"].Transport != ir.TransportHTTP || byName["figma"].URL != "https://mcp.figma.com" {
		t.Fatalf("figma=%+v", byName["figma"])
	}
}

func TestExportSkillsCommandsSubagents(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	skills, _ := a.ExportSkills(ctx)
	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
	}
	if !names["demo"] || !names["agdemo"] {
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
	if len(subs) != 1 || subs[0].Name != "bar" {
		t.Fatalf("subagents=%+v", subs)
	}
	if subs[0].Model != "gpt-5" {
		t.Fatalf("model=%q", subs[0].Model)
	}
	if subs[0].Extras["readonly"] != true {
		t.Fatalf("extras=%+v", subs[0].Extras)
	}
	if !strings.Contains(subs[0].SystemPrompt, "careful code reviewer") {
		t.Fatalf("system prompt=%q", subs[0].SystemPrompt)
	}
}

func TestExportProjectStateIgnore(t *testing.T) {
	ps, err := New().ExportProjectState(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if ps.IgnoreMode != ir.IgnoreBlock {
		t.Fatalf("mode=%s", ps.IgnoreMode)
	}
	if len(ps.IgnorePatterns) != 3 {
		t.Fatalf("patterns=%+v", ps.IgnorePatterns)
	}
	want := map[string]bool{"dist/": false, "node_modules/": false, "*.log": false}
	for _, p := range ps.IgnorePatterns {
		want[p] = true
	}
	for p, ok := range want {
		if !ok {
			t.Fatalf("missing ignore pattern %q in %+v", p, ps.IgnorePatterns)
		}
	}
}

// Round-trip: cursor -> IR -> cursor reproduces the core files.
func TestPlanImportRoundTrip(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	b := ir.NewBundle(ir.Source{Tool: "cursor"})
	b.Instructions, _ = a.ExportInstructions(ctx)
	b.McpServers, _ = a.ExportMcpServers(ctx)
	b.Skills, _ = a.ExportSkills(ctx)
	b.Commands, _ = a.ExportCommands(ctx)
	b.Subagents, _ = a.ExportSubagents(ctx)

	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	paths := map[string]bool{}
	for _, f := range plan.Files {
		rel, _ := filepath.Rel(out, f.Path)
		paths[rel] = true
	}
	mustHave := []string{
		"AGENTS.md",
		".cursor/mcp.json",
		filepath.Join(".cursor", "skills", "demo", "SKILL.md"),
		filepath.Join(".cursor", "commands", "foo.md"),
		filepath.Join(".cursor", "agents", "bar.md"),
		filepath.Join(".cursor", "rules", "go-files.mdc"),
		filepath.Join(".cursor", "rules", "use-this-rule-when-writing-database-migrations.mdc"),
	}
	for _, m := range mustHave {
		if !paths[m] {
			t.Errorf("expected planned file %s; have %+v", m, keys(paths))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPlanImportAlwaysAppendsToAgentsMd(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{Body: "first body", Scope: ir.ScopeProject}, Activation: ir.ActAlways},
		{Common: ir.Common{Body: "second body", Scope: ir.ScopeProject}, Activation: ir.ActAlways},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	var body string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, "AGENTS.md") {
			body = string(f.Content)
		}
	}
	if !strings.Contains(body, "first body") || !strings.Contains(body, "second body") {
		t.Fatalf("AGENTS.md should concatenate both: %q", body)
	}
}

func TestPlanImportNonAlwaysToMdcRules(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "go-rule", Body: "go body", Scope: ir.ScopeProject}, Activation: ir.ActGlob, Globs: []string{"**/*.go"}},
		{Common: ir.Common{ID: "db-rule", Body: "db body", Scope: ir.ScopeProject}, Activation: ir.ActModelDecision},
	}
	b.Instructions[1].Frontmatter = map[string]any{"description": "use for db"}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	var globMdc, modelMdc string
	for _, f := range plan.Files {
		if strings.Contains(f.Path, filepath.Join(".cursor", "rules")) {
			if strings.Contains(string(f.Content), "go body") {
				globMdc = string(f.Content)
			}
			if strings.Contains(string(f.Content), "db body") {
				modelMdc = string(f.Content)
			}
		}
	}
	if globMdc == "" || !strings.Contains(globMdc, "globs") {
		t.Fatalf("glob mdc missing globs frontmatter: %q", globMdc)
	}
	if strings.Contains(globMdc, "alwaysApply: true") {
		t.Fatalf("glob mdc must set alwaysApply false: %q", globMdc)
	}
	if modelMdc == "" {
		t.Fatalf("model mdc not written")
	}
}

// Memory (user scope) -> UI unsupported warning + preserved helper file.
func TestPlanImportMemoryWarnsAndPreserves(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "mem", Body: "personal memory body", Scope: ir.ScopeUser}, Activation: ir.ActAlways},
	}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})

	var preserved string
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "agensync-user-rules.md") {
			preserved = string(f.Content)
		}
	}
	if !strings.Contains(preserved, "personal memory body") {
		t.Fatalf("memory body not preserved: %q (files=%+v)", preserved, plan.Files)
	}
	var sawWarn bool
	for _, w := range plan.Warnings {
		if w.Category == "memory" && w.Action == ir.ActionManual {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("expected manual memory warning, got %+v", plan.Warnings)
	}
}

func TestPlanImportMcpSubagentsCommandsSkills(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{{Name: "ctx7", Transport: ir.TransportStdio, Command: "npx", Enabled: true}}
	b.Skills = []ir.Skill{{Common: ir.Common{Body: "skill body"}, Name: "demo", Description: "d"}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "cmd body"}, Name: "deploy", Description: "Deploy"}}
	b.Subagents = []ir.Subagent{{
		Common:       ir.Common{Body: "agent body"},
		Name:         "rev",
		Model:        "gpt-5",
		SystemPrompt: "agent body",
		Extras:       map[string]any{"readonly": true, "is_background": false},
	}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{})

	want := map[string]bool{
		filepath.Join(out, ".cursor", "mcp.json"):                   false,
		filepath.Join(out, ".cursor", "skills", "demo", "SKILL.md"): false,
		filepath.Join(out, ".cursor", "commands", "deploy.md"):      false,
		filepath.Join(out, ".cursor", "agents", "rev.md"):           false,
	}
	var subagentContent string
	for _, f := range plan.Files {
		if _, ok := want[f.Path]; ok {
			want[f.Path] = true
		}
		if f.Path == filepath.Join(out, ".cursor", "agents", "rev.md") {
			subagentContent = string(f.Content)
		}
	}
	for p, ok := range want {
		if !ok {
			t.Errorf("missing planned file %s", p)
		}
	}
	if !strings.Contains(subagentContent, "model: gpt-5") || !strings.Contains(subagentContent, "readonly: true") {
		t.Fatalf("subagent frontmatter missing extras: %q", subagentContent)
	}
}

// THE CRITICAL REQUIREMENT: every category the bundle carries that cursor cannot
// represent must produce a structured warning. Cursor supports everything except
// personal memory (UI) and SSE-less... actually cursor supports all transports,
// so the only unsupported native category here is user-scope memory.
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "proj", Body: "proj body", Scope: ir.ScopeProject}, Activation: ir.ActAlways},
		{Common: ir.Common{ID: "mem", Body: "mem body", Scope: ir.ScopeUser}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{{Name: "s1", Transport: ir.TransportStdio, Command: "x", Enabled: true}}
	b.Skills = []ir.Skill{{Common: ir.Common{Body: "sk"}, Name: "sk"}}
	b.Commands = []ir.Command{{Common: ir.Common{Body: "c"}, Name: "c"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{Body: "a"}, Name: "a"}}

	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out, HomeDir: t.TempDir()}, adapter.ImportOptions{})

	// memory is the category cursor cannot natively represent (MemoryUI).
	var sawMemory bool
	for _, w := range plan.Warnings {
		if w.Category == "memory" {
			sawMemory = true
		}
	}
	if !sawMemory {
		t.Fatalf("expected a memory warning for UI-only personal memory, got %+v", plan.Warnings)
	}

	// And the supported categories must still produce their files (no silent drop).
	mustHave := map[string]bool{
		filepath.Join(out, "AGENTS.md"):                           false,
		filepath.Join(out, ".cursor", "mcp.json"):                 false,
		filepath.Join(out, ".cursor", "skills", "sk", "SKILL.md"): false,
		filepath.Join(out, ".cursor", "commands", "c.md"):         false,
		filepath.Join(out, ".cursor", "agents", "a.md"):           false,
	}
	for _, f := range plan.Files {
		if _, ok := mustHave[f.Path]; ok {
			mustHave[f.Path] = true
		}
	}
	for p, ok := range mustHave {
		if !ok {
			t.Errorf("supported category dropped: %s missing", p)
		}
	}
}

func TestPlanImportSseMcpSupported(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{{Name: "sseSrv", Transport: ir.TransportSSE, URL: "https://sse.example.com", Enabled: true}}
	out := t.TempDir()
	plan := a.PlanImport(b, ir.Context{ProjectPath: out}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	var content string
	for _, f := range plan.Files {
		if f.Path == filepath.Join(out, ".cursor", "mcp.json") {
			content = string(f.Content)
		}
	}
	if !strings.Contains(content, "sse.example.com") {
		t.Fatalf("SSE server not rendered: %q", content)
	}
}
