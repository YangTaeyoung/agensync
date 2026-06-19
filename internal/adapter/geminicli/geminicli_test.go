package geminicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func fromCtx(t *testing.T) ir.Context {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return ir.Context{
		ProjectPath: filepath.Join(wd, "testdata", "from"),
		HomeDir:     filepath.Join(wd, "testdata", "home"),
	}
}

func TestMeta(t *testing.T) {
	m := New().Meta()
	if m.ID != "gemini-cli" {
		t.Fatalf("ID = %q, want gemini-cli", m.ID)
	}
	if m.DisplayName != "Gemini CLI" || m.Vendor != "Google" || m.Confidence != "high" {
		t.Fatalf("unexpected meta: %+v", m)
	}
}

func TestDetect(t *testing.T) {
	res := New().Detect(fromCtx(t))
	if !res.Present {
		t.Fatal("expected Present=true")
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 2 {
		t.Fatalf("want 2 instructions (project + user), got %d", len(ins))
	}
	var project, user *ir.Instruction
	for i := range ins {
		if ins[i].IsMemory() {
			user = &ins[i]
		} else {
			project = &ins[i]
		}
	}
	if project == nil || user == nil {
		t.Fatalf("missing project or memory instruction: %+v", ins)
	}
	if project.Activation != ir.ActAlways {
		t.Fatalf("project activation = %q, want always", project.Activation)
	}
	if len(project.Imports) != 1 || project.Imports[0].Kind != ir.ImpInline {
		t.Fatalf("expected one inline @import, got %+v", project.Imports)
	}
	if !strings.Contains(project.Imports[0].Resolved, "adapters and a canonical IR") {
		t.Fatalf("import not resolved: %+v", project.Imports[0])
	}
	if user.Scope != ir.ScopeUser {
		t.Fatalf("user instruction scope = %q", user.Scope)
	}
}

func TestExportMcpServers(t *testing.T) {
	servers, err := New().ExportMcpServers(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ir.McpServer{}
	for _, s := range servers {
		by[s.Name] = s
	}
	if got := by["filesystem"]; got.Transport != ir.TransportStdio || got.Command != "npx" {
		t.Fatalf("filesystem server wrong: %+v", got)
	}
	if got := by["remote-http"]; got.Transport != ir.TransportHTTP || got.URL != "https://api.example.com/mcp" {
		t.Fatalf("remote-http should be HTTP via httpUrl: %+v", got)
	}
	if got := by["remote-sse"]; got.Transport != ir.TransportSSE || got.URL != "https://sse.example.com/mcp" {
		t.Fatalf("remote-sse should be SSE via url: %+v", got)
	}
	if got, ok := by["personal-tools"]; !ok || got.Scope != ir.ScopeUser {
		t.Fatalf("expected user-scope personal-tools server: %+v", got)
	}
}

func TestExportSkillsNil(t *testing.T) {
	skills, err := New().ExportSkills(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if skills != nil {
		t.Fatalf("gemini has no skills; want nil, got %+v", skills)
	}
}

func TestExportCommands(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ir.Command{}
	for _, c := range cmds {
		by[c.Name] = c
	}
	rev, ok := by["review"]
	if !ok {
		t.Fatalf("missing review command: %+v", cmds)
	}
	if rev.Description != "Review the staged diff for bugs" {
		t.Fatalf("review description wrong: %q", rev.Description)
	}
	if !strings.Contains(rev.Body, "{{args}}") {
		t.Fatalf("review body should contain {{args}}: %q", rev.Body)
	}
	if rev.ArgSpec.Style != ir.ArgAll || len(rev.ArgSpec.Placeholders) != 1 || rev.ArgSpec.Placeholders[0] != "{{args}}" {
		t.Fatalf("review argspec wrong: %+v", rev.ArgSpec)
	}
	sum, ok := by["summarize"]
	if !ok {
		t.Fatalf("missing summarize command")
	}
	if sum.ArgSpec.Style != "" {
		t.Fatalf("summarize should have no argspec: %+v", sum.ArgSpec)
	}
}

func TestExportSubagents(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 subagent, got %d", len(subs))
	}
	p := subs[0]
	if p.Name != "planner" {
		t.Fatalf("name = %q", p.Name)
	}
	if p.Description == "" {
		t.Fatalf("missing description")
	}
	if !strings.Contains(p.SystemPrompt, "planning agent") {
		t.Fatalf("system prompt wrong: %q", p.SystemPrompt)
	}
	if p.Extras["temperature"] == nil || p.Extras["max_turns"] == nil || p.Extras["kind"] == nil {
		t.Fatalf("expected frontmatter extras: %+v", p.Extras)
	}
}

func TestExportProjectStateTrust(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	gem := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(gem, 0o755); err != nil {
		t.Fatal(err)
	}
	tf := map[string]string{dir: "TRUST_FOLDER"}
	b, _ := json.Marshal(tf)
	if err := os.WriteFile(filepath.Join(gem, "trustedFolders.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := New().ExportProjectState(ir.Context{ProjectPath: dir, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if ps.Trust != "trusted" {
		t.Fatalf("expected trusted, got %q", ps.Trust)
	}
}

// ---- PlanImport ----

func basicBundle() ir.AgentConfigBundle {
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "proj", Scope: ir.ScopeProject, Body: "# Project rules\n@docs/x.md"}, Activation: ir.ActAlways},
		{Common: ir.Common{ID: "mem", Scope: ir.ScopeUser, Body: "# Personal memory"}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "fs", Transport: ir.TransportStdio, Command: "npx", Args: []string{"-y", "server"}, Enabled: true},
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "http", Transport: ir.TransportHTTP, URL: "https://h.example.com", Enabled: true},
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "sse", Transport: ir.TransportSSE, URL: "https://s.example.com", Enabled: true},
	}
	b.Skills = []ir.Skill{
		{Common: ir.Common{ID: "deploy", Body: "Run the deploy script."}, Name: "deploy", Description: "Deploy"},
	}
	b.Commands = []ir.Command{
		{Common: ir.Common{ID: "review", Body: "Review $ARGUMENTS carefully."}, Name: "review", Description: "Review", ArgSpec: ir.ArgSpec{Style: ir.ArgAll, Placeholders: []string{"$ARGUMENTS"}}},
	}
	b.Subagents = []ir.Subagent{
		{Common: ir.Common{ID: "tester"}, Name: "tester", Description: "Writes tests", SystemPrompt: "You write tests."},
	}
	b.ProjectState = ir.ProjectState{Trust: "trusted"}
	return b
}

func findFile(plan ir.WritePlan, suffix string) (ir.PlannedFile, bool) {
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, suffix) {
			return f, true
		}
	}
	return ir.PlannedFile{}, false
}

func TestPlanImportPaths(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	ctx := ir.Context{ProjectPath: proj, HomeDir: home}
	plan := New().PlanImport(basicBundle(), ctx, adapter.ImportOptions{})

	// project instructions -> GEMINI.md
	gem, ok := findFile(plan, filepath.Join(proj, "GEMINI.md"))
	if !ok {
		t.Fatalf("missing GEMINI.md; files: %v", paths(plan))
	}
	if !strings.Contains(string(gem.Content), "Project rules") {
		t.Fatalf("GEMINI.md missing project body")
	}
	// skill body should be appended to GEMINI.md (skills->instructions fallback)
	if !strings.Contains(string(gem.Content), "Run the deploy script") {
		t.Fatalf("skill body should be appended to GEMINI.md: %q", gem.Content)
	}

	// memory -> ~/.gemini/GEMINI.md
	if _, ok := findFile(plan, filepath.Join(home, ".gemini", "GEMINI.md")); !ok {
		t.Fatalf("missing user memory GEMINI.md; files: %v", paths(plan))
	}

	// mcp -> .gemini/settings.json
	settings, ok := findFile(plan, filepath.Join(proj, ".gemini", "settings.json"))
	if !ok {
		t.Fatalf("missing settings.json; files: %v", paths(plan))
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(settings.Content, &doc); err != nil {
		t.Fatalf("settings.json invalid: %v", err)
	}
	raw, ok := doc["mcpServers"]
	if !ok {
		t.Fatalf("settings.json missing mcpServers key")
	}
	var ms map[string]map[string]any
	if err := json.Unmarshal(raw, &ms); err != nil {
		t.Fatal(err)
	}
	// HTTP uses httpUrl, SSE uses url, no type field anywhere.
	if _, has := ms["http"]["httpUrl"]; !has {
		t.Fatalf("http server must use httpUrl: %+v", ms["http"])
	}
	if _, has := ms["sse"]["url"]; !has {
		t.Fatalf("sse server must use url: %+v", ms["sse"])
	}
	for name, s := range ms {
		if _, has := s["type"]; has {
			t.Fatalf("server %q must not have a type field: %+v", name, s)
		}
	}

	// commands -> .gemini/commands/review.toml
	cmd, ok := findFile(plan, filepath.Join(proj, ".gemini", "commands", "review.toml"))
	if !ok {
		t.Fatalf("missing review.toml; files: %v", paths(plan))
	}
	if !strings.Contains(string(cmd.Content), "{{args}}") {
		t.Fatalf("command toml must translate args to {{args}}: %q", cmd.Content)
	}
	if !strings.Contains(string(cmd.Content), "prompt") || !strings.Contains(string(cmd.Content), "description") {
		t.Fatalf("command toml missing prompt/description: %q", cmd.Content)
	}

	// subagents -> .gemini/agents/tester.md
	if _, ok := findFile(plan, filepath.Join(proj, ".gemini", "agents", "tester.md")); !ok {
		t.Fatalf("missing tester.md; files: %v", paths(plan))
	}

	// trust -> ~/.gemini/trustedFolders.json
	tf, ok := findFile(plan, filepath.Join(home, ".gemini", "trustedFolders.json"))
	if !ok {
		t.Fatalf("missing trustedFolders.json; files: %v", paths(plan))
	}
	if !strings.Contains(string(tf.Content), proj) {
		t.Fatalf("trustedFolders.json should contain project path: %q", tf.Content)
	}
}

func TestPlanImportSettingsMerge(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	gdir := filepath.Join(proj, ".gemini")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"theme":"Dark","mcpServers":{"old":{"command":"old"}},"telemetry":{"enabled":true}}`
	if err := os.WriteFile(filepath.Join(gdir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := New().PlanImport(basicBundle(), ir.Context{ProjectPath: proj, HomeDir: home}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	settings, ok := findFile(plan, filepath.Join(gdir, "settings.json"))
	if !ok {
		t.Fatal("missing settings.json")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(settings.Content, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["theme"]; !ok {
		t.Fatalf("merge must preserve other keys (theme); got %s", settings.Content)
	}
	if _, ok := doc["telemetry"]; !ok {
		t.Fatalf("merge must preserve telemetry key; got %s", settings.Content)
	}
	var ms map[string]map[string]any
	_ = json.Unmarshal(doc["mcpServers"], &ms)
	if _, ok := ms["fs"]; !ok {
		t.Fatalf("mcpServers should be replaced with imported set; got %+v", ms)
	}
}

func TestUnsupportedCategoriesWarn(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	plan := New().PlanImport(basicBundle(), ir.Context{ProjectPath: proj, HomeDir: home}, adapter.ImportOptions{})

	want := map[string]bool{
		"skills":   false, // gemini has no skills
		"commands": false, // md->toml transform warning
	}
	for _, w := range plan.Warnings {
		if _, ok := want[w.Category]; ok {
			want[w.Category] = true
		}
	}
	for cat, seen := range want {
		if !seen {
			t.Fatalf("expected a warning for category %q; warnings: %+v", cat, plan.Warnings)
		}
	}
}

func TestPlanImportMemoryWritesFile(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	// memory only
	plan := New().PlanImport(basicBundle(), ir.Context{ProjectPath: proj, HomeDir: home}, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	f, ok := findFile(plan, filepath.Join(home, ".gemini", "GEMINI.md"))
	if !ok {
		t.Fatalf("memory must be written to ~/.gemini/GEMINI.md; files: %v", paths(plan))
	}
	if !strings.Contains(string(f.Content), "Personal memory") {
		t.Fatalf("memory file missing body: %q", f.Content)
	}
	// project instruction must NOT be written when only memory requested
	if _, ok := findFile(plan, filepath.Join(proj, "GEMINI.md")); ok {
		t.Fatalf("project GEMINI.md should not be written for memory-only import")
	}
}

func TestPlanImportSkillsOnlyWritesFallback(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	ctx := ir.Context{ProjectPath: proj, HomeDir: home}
	// skills selected, instructions NOT selected: the skill body must still be
	// written to GEMINI.md (the only fallback), matching the emitted warning.
	plan := New().PlanImport(basicBundle(), ctx, adapter.ImportOptions{Categories: map[string]bool{"skills": true}})

	gem, ok := findFile(plan, filepath.Join(proj, "GEMINI.md"))
	if !ok {
		t.Fatalf("skills-only import must write GEMINI.md fallback; files: %v", paths(plan))
	}
	if !strings.Contains(string(gem.Content), "Run the deploy script") {
		t.Fatalf("GEMINI.md must contain the skill body: %q", gem.Content)
	}
	// the project instruction body must NOT leak in (instructions not requested)
	if strings.Contains(string(gem.Content), "Project rules") {
		t.Fatalf("project instruction body must not be written for skills-only import: %q", gem.Content)
	}

	// the skills warning must be present and claim instruction emission (matches reality now)
	var sawSkill bool
	for _, w := range plan.Warnings {
		if w.Category == "skills" {
			sawSkill = true
			if w.Action != ir.ActionInline {
				t.Fatalf("skills warning action = %q, want inline", w.Action)
			}
		}
	}
	if !sawSkill {
		t.Fatalf("expected a skills warning; warnings: %+v", plan.Warnings)
	}
}

func TestPlanImportSkillsOnlyNoSkillsNoFile(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	ctx := ir.Context{ProjectPath: proj, HomeDir: home}
	b := basicBundle()
	b.Skills = nil // skills requested but bundle has none
	plan := New().PlanImport(b, ctx, adapter.ImportOptions{Categories: map[string]bool{"skills": true}})
	if _, ok := findFile(plan, filepath.Join(proj, "GEMINI.md")); ok {
		t.Fatalf("no GEMINI.md should be written when there are no skill bodies: %v", paths(plan))
	}
}

func TestPlanImportInstructionsOnlyNoSkillBody(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	ctx := ir.Context{ProjectPath: proj, HomeDir: home}
	// instructions selected, skills NOT: GEMINI.md must contain the instruction
	// body but not the skill fallback body.
	plan := New().PlanImport(basicBundle(), ctx, adapter.ImportOptions{Categories: map[string]bool{"instructions": true}})
	gem, ok := findFile(plan, filepath.Join(proj, "GEMINI.md"))
	if !ok {
		t.Fatalf("instructions-only import must write GEMINI.md; files: %v", paths(plan))
	}
	if !strings.Contains(string(gem.Content), "Project rules") {
		t.Fatalf("GEMINI.md must contain instruction body: %q", gem.Content)
	}
	if strings.Contains(string(gem.Content), "Run the deploy script") {
		t.Fatalf("skill body must not be written when skills not requested: %q", gem.Content)
	}
}

func paths(plan ir.WritePlan) []string {
	var out []string
	for _, f := range plan.Files {
		out = append(out, f.Path)
	}
	return out
}
