package kiro

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
	if m.ID != "kiro" || m.DisplayName != "Kiro" || m.Vendor != "AWS/Kiro" {
		t.Fatalf("meta=%+v", m)
	}
	c := a.Capabilities()
	if c.Memory != ir.MemoryFile {
		t.Fatalf("memory=%v", c.Memory)
	}
	if c.Commands.Supported {
		t.Fatalf("commands should be unsupported: %+v", c.Commands)
	}
	if !c.Skills || c.Subagents != "true" || !c.Hooks {
		t.Fatalf("caps=%+v", c)
	}
	if c.MCP.RootKey != "mcpServers" || c.MCP.RemoteURLKey != "url" {
		t.Fatalf("mcp caps=%+v", c.MCP)
	}
}

func TestDetect(t *testing.T) {
	res := New().Detect(fromCtx(t))
	if !res.Present {
		t.Fatalf("expected present: %+v", res)
	}
}

func TestExportInstructions(t *testing.T) {
	ins, err := New().ExportInstructions(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ir.Instruction{}
	for _, in := range ins {
		byID[in.ID] = in
	}
	style, ok := byID["style"]
	if !ok {
		t.Fatalf("missing style steering: %+v", byID)
	}
	if style.Activation != ir.ActAlways {
		t.Fatalf("style activation=%s", style.Activation)
	}
	if style.Scope != ir.ScopeProject {
		t.Fatalf("style scope=%s", style.Scope)
	}
	if len(style.Imports) != 1 || style.Imports[0].Kind != ir.ImpFileEmbed {
		t.Fatalf("style imports=%+v", style.Imports)
	}
	if style.Imports[0].Target != "shared/conventions.md" {
		t.Fatalf("import target=%q", style.Imports[0].Target)
	}
	if !strings.Contains(style.Imports[0].Resolved, "tests first") {
		t.Fatalf("import not resolved: %q", style.Imports[0].Resolved)
	}
	front, ok := byID["frontend"]
	if !ok {
		t.Fatalf("missing frontend steering")
	}
	if front.Activation != ir.ActGlob {
		t.Fatalf("frontend activation=%s", front.Activation)
	}
	if len(front.Globs) != 1 || front.Globs[0] != "src/**/*.tsx" {
		t.Fatalf("frontend globs=%+v", front.Globs)
	}
	// AGENTS.md is also exported as a project instruction.
	var foundAgents bool
	for _, in := range ins {
		if strings.HasSuffix(in.Origin, "AGENTS.md") {
			foundAgents = true
			if in.Scope != ir.ScopeProject {
				t.Fatalf("AGENTS.md scope=%s", in.Scope)
			}
		}
	}
	if !foundAgents {
		t.Fatal("AGENTS.md not exported")
	}
}

func TestExportInstructionsHomeMemory(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".kiro", "steering"), 0o755)
	os.WriteFile(filepath.Join(home, ".kiro", "steering", "global.md"),
		[]byte("---\ninclusion: manual\n---\nglobal memory body\n"), 0o644)
	ctx := ir.Context{ProjectPath: "testdata/from", HomeDir: home}
	ins, err := New().ExportInstructions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mem *ir.Instruction
	for i := range ins {
		if ins[i].Scope == ir.ScopeUser {
			mem = &ins[i]
		}
	}
	if mem == nil {
		t.Fatalf("expected a user-scope memory instruction: %+v", ins)
	}
	if !mem.IsMemory() {
		t.Fatal("home steering should be memory")
	}
	if mem.Activation != ir.ActManual {
		t.Fatalf("home activation=%s", mem.Activation)
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
	fetch, ok := byName["fetch"]
	if !ok {
		t.Fatalf("missing fetch server: %+v", byName)
	}
	if fetch.Command != "uvx" || fetch.Transport != ir.TransportStdio {
		t.Fatalf("fetch=%+v", fetch)
	}
	if fetch.Env["API_TOKEN"] != "${API_TOKEN}" {
		t.Fatalf("env not preserved: %+v", fetch.Env)
	}
	if len(fetch.AutoApprove) != 1 || fetch.AutoApprove[0] != "fetch" {
		t.Fatalf("autoApprove=%+v", fetch.AutoApprove)
	}
	if !fetch.Enabled {
		t.Fatalf("fetch should be enabled")
	}
	if fetch.Scope != ir.ScopeProject {
		t.Fatalf("fetch scope=%s", fetch.Scope)
	}
	remote, ok := byName["remote"]
	if !ok {
		t.Fatalf("missing remote server")
	}
	if remote.Enabled {
		t.Fatalf("remote should be disabled")
	}
	if remote.Transport != ir.TransportSSE {
		t.Fatalf("remote transport=%s", remote.Transport)
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
	if len(skills[0].Resources) != 1 {
		t.Fatalf("resources=%+v", skills[0].Resources)
	}
}

func TestExportCommandsNil(t *testing.T) {
	cmds, err := New().ExportCommands(fromCtx(t))
	if err != nil || cmds != nil {
		t.Fatalf("commands=%+v err=%v", cmds, err)
	}
}

func TestExportSubagents(t *testing.T) {
	subs, err := New().ExportSubagents(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "reviewer" {
		t.Fatalf("subagents=%+v", subs)
	}
	if len(subs[0].Tools) != 3 {
		t.Fatalf("tools=%+v", subs[0].Tools)
	}
	if subs[0].Model != "claude-sonnet" {
		t.Fatalf("model=%q", subs[0].Model)
	}
	if !strings.Contains(subs[0].SystemPrompt, "code reviewer") {
		t.Fatalf("prompt=%q", subs[0].SystemPrompt)
	}
	if subs[0].Extras["includeMcpJson"] != true {
		t.Fatalf("extras=%+v", subs[0].Extras)
	}
}

func TestExportProjectStateZero(t *testing.T) {
	ps, err := New().ExportProjectState(fromCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Permissions.Allow) != 0 || len(ps.Hooks) != 0 || ps.Trust != "" {
		t.Fatalf("expected zero project state: %+v", ps)
	}
}

// Round-trip kiro -> IR -> kiro reproduces the core target files.
func TestPlanImportRoundTrip(t *testing.T) {
	a := New()
	ctx := fromCtx(t)
	b := ir.NewBundle(ir.Source{Tool: "kiro"})
	b.Instructions, _ = a.ExportInstructions(ctx)
	b.McpServers, _ = a.ExportMcpServers(ctx)
	b.Skills, _ = a.ExportSkills(ctx)
	b.Subagents, _ = a.ExportSubagents(ctx)

	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	paths := map[string]string{}
	for _, f := range plan.Files {
		rel, _ := filepath.Rel(out, f.Path)
		paths[rel] = string(f.Content)
	}
	mustExist := []string{
		filepath.Join(".kiro", "steering", "style.md"),
		filepath.Join(".kiro", "steering", "frontend.md"),
		filepath.Join(".kiro", "settings", "mcp.json"),
		filepath.Join(".kiro", "skills", "demo", "SKILL.md"),
		filepath.Join(".kiro", "agents", "reviewer.md"),
	}
	for _, rel := range mustExist {
		if _, ok := paths[rel]; !ok {
			t.Errorf("expected %s in plan, got %v", rel, keys(paths))
		}
	}
	// fileMatch frontmatter derived from globs
	front := paths[filepath.Join(".kiro", "steering", "frontend.md")]
	if !strings.Contains(front, "inclusion: fileMatch") || !strings.Contains(front, "src/**/*.tsx") {
		t.Fatalf("frontend steering frontmatter wrong:\n%s", front)
	}
	// file-embed import preserved verbatim
	style := paths[filepath.Join(".kiro", "steering", "style.md")]
	if !strings.Contains(style, "#[[file:shared/conventions.md]]") {
		t.Fatalf("style steering lost import marker:\n%s", style)
	}
	mcp := paths[filepath.Join(".kiro", "settings", "mcp.json")]
	if !strings.Contains(mcp, "mcpServers") || !strings.Contains(mcp, "autoApprove") {
		t.Fatalf("mcp.json wrong:\n%s", mcp)
	}
	if strings.Contains(mcp, "\"type\"") {
		t.Fatalf("mcp.json should not emit type:\n%s", mcp)
	}
	// Subagent includeMcpJson extra survives the full export -> IR -> import trip.
	reviewer := paths[filepath.Join(".kiro", "agents", "reviewer.md")]
	if !strings.Contains(reviewer, "includeMcpJson") {
		t.Fatalf("reviewer subagent lost includeMcpJson on round-trip:\n%s", reviewer)
	}
}

func TestPlanImportMemoryWritesHomeFile(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "other"})
	b.Instructions = []ir.Instruction{{
		Common:     ir.Common{ID: "global", Scope: ir.ScopeUser, Body: "personal memory body\n"},
		Activation: ir.ActAlways,
	}}
	home := t.TempDir()
	dctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: home}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	var found string
	for _, f := range plan.Files {
		if strings.Contains(f.Path, filepath.Join(".kiro", "steering")) && strings.HasPrefix(f.Path, home) {
			found = f.Path
		}
	}
	if found == "" {
		t.Fatalf("memory not written to home steering: %+v", plan.Files)
	}
}

// commands -> manual steering + UnsupportedCommand warning (NEVER drop).
func TestPlanImportCommandsBecomeSteeringAndWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Commands = []ir.Command{{
		Common: ir.Common{ID: "deploy", Body: "Run the deploy.\n"},
		Name:   "deploy",
	}}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	var steering string
	for _, f := range plan.Files {
		if strings.Contains(f.Path, filepath.Join(".kiro", "steering", "deploy.md")) {
			steering = string(f.Content)
		}
	}
	if steering == "" {
		t.Fatalf("command not converted to steering: %+v", plan.Files)
	}
	if !strings.Contains(steering, "inclusion: manual") {
		t.Fatalf("command steering should be manual:\n%s", steering)
	}
	if !hasWarning(plan.Warnings, "commands") {
		t.Fatalf("expected commands warning: %+v", plan.Warnings)
	}
}

func TestPlanImportHooksWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.ProjectState = ir.ProjectState{
		Hooks:       []ir.Hook{{Event: "PreToolUse", Command: "echo hi"}},
		Permissions: ir.Permissions{Allow: []string{"Bash(*)"}},
	}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})
	if !hasWarning(plan.Warnings, "hooks") {
		t.Fatalf("expected hooks warning: %+v", plan.Warnings)
	}
	if !hasWarning(plan.Warnings, "permissions") {
		t.Fatalf("expected permissions warning: %+v", plan.Warnings)
	}
}

// The critical requirement: feed a bundle containing every category and assert a
// warning exists for each category kiro cannot natively represent (commands).
func TestUnsupportedCategoriesWarn(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{{
		Common:     ir.Common{ID: "i", Scope: ir.ScopeProject, Body: "x"},
		Activation: ir.ActAlways,
	}}
	b.McpServers = []ir.McpServer{{
		Common: ir.Common{Scope: ir.ScopeProject}, Name: "s",
		Transport: ir.TransportStdio, Command: "x", Enabled: true,
	}}
	b.Skills = []ir.Skill{{Common: ir.Common{ID: "sk", Body: "y"}, Name: "sk"}}
	b.Commands = []ir.Command{{Common: ir.Common{ID: "c", Body: "z"}, Name: "c"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{ID: "a"}, Name: "a", SystemPrompt: "p"}}

	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	// kiro supports instructions, mcp, skills, subagents natively; only commands
	// is unsupported and MUST warn.
	if !hasWarning(plan.Warnings, "commands") {
		t.Fatalf("commands must warn: %+v", plan.Warnings)
	}
	// supported categories must still be planned (not dropped).
	wantFiles := []string{
		filepath.Join(".kiro", "steering", "i.md"),
		filepath.Join(".kiro", "settings", "mcp.json"),
		filepath.Join(".kiro", "skills", "sk", "SKILL.md"),
		filepath.Join(".kiro", "agents", "a.md"),
		filepath.Join(".kiro", "steering", "c.md"), // command -> manual steering
	}
	for _, rel := range wantFiles {
		if !planHas(plan, filepath.Join(out, rel)) {
			t.Errorf("expected planned file %s; files=%v", rel, planPaths(plan))
		}
	}
}

// Kiro's mcp.json has no transport discriminator, so it cannot honestly claim
// to represent http distinctly from sse. The capability must not advertise http.
func TestCapabilitiesNoHTTPTransport(t *testing.T) {
	c := New().Capabilities()
	for _, tr := range c.MCP.Transports {
		if tr == ir.TransportHTTP {
			t.Fatalf("kiro must not claim http transport (cannot distinguish from sse): %+v", c.MCP.Transports)
		}
	}
	// stdio and sse must still be representable.
	var hasStdio, hasSSE bool
	for _, tr := range c.MCP.Transports {
		switch tr {
		case ir.TransportStdio:
			hasStdio = true
		case ir.TransportSSE:
			hasSSE = true
		}
	}
	if !hasStdio || !hasSSE {
		t.Fatalf("kiro should still claim stdio+sse: %+v", c.MCP.Transports)
	}
}

// An incoming http MCP server is coerced to a plain url (sse on re-read) because
// Kiro has no type discriminator; this lossy coercion MUST warn (never silent).
func TestPlanImportHTTPMcpWarnsTransportLoss(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.McpServers = []ir.McpServer{
		{
			Common:    ir.Common{Scope: ir.ScopeProject},
			Name:      "httpsrv",
			Transport: ir.TransportHTTP,
			URL:       "https://mcp.example.com/mcp",
			Enabled:   true,
		},
		{
			Common:    ir.Common{Scope: ir.ScopeProject},
			Name:      "ssesrv",
			Transport: ir.TransportSSE,
			URL:       "https://mcp.example.com/sse",
			Enabled:   true,
		},
	}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	var transportWarns int
	for _, w := range plan.Warnings {
		if w.Category == "mcp" && w.Artifact == "httpsrv" {
			transportWarns++
			if w.Action != ir.ActionManual {
				t.Fatalf("transport-loss warning action=%s, want manual", w.Action)
			}
		}
		// The sse server must NOT warn (it round-trips faithfully).
		if w.Category == "mcp" && w.Artifact == "ssesrv" {
			t.Fatalf("sse server should not produce a transport-loss warning: %+v", w)
		}
	}
	if transportWarns != 1 {
		t.Fatalf("expected exactly one transport-loss warning for httpsrv, got %d: %+v", transportWarns, plan.Warnings)
	}
	// Both servers are still written (not dropped).
	mcpPath := filepath.Join(out, ".kiro", "settings", "mcp.json")
	var content string
	for _, f := range plan.Files {
		if f.Path == mcpPath {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf("mcp.json not planned: %+v", planPaths(plan))
	}
	if !strings.Contains(content, "httpsrv") || !strings.Contains(content, "ssesrv") {
		t.Fatalf("both servers must be written, not dropped:\n%s", content)
	}
}

// includeMcpJson (Extras) must round-trip into subagent frontmatter on import.
func TestPlanImportSubagentIncludeMcpJsonRoundTrips(t *testing.T) {
	a := New()
	b := ir.NewBundle(ir.Source{Tool: "kiro"})
	b.Subagents = []ir.Subagent{{
		Common:       ir.Common{ID: "reviewer"},
		Name:         "reviewer",
		Description:  "Reviews code",
		SystemPrompt: "You review code.",
		Extras:       map[string]any{"includeMcpJson": true},
	}}
	out := t.TempDir()
	dctx := ir.Context{ProjectPath: out, HomeDir: t.TempDir()}
	plan := a.PlanImport(b, dctx, adapter.ImportOptions{})

	agentPath := filepath.Join(out, ".kiro", "agents", "reviewer.md")
	var content string
	for _, f := range plan.Files {
		if f.Path == agentPath {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatalf("reviewer agent not planned: %+v", planPaths(plan))
	}
	if !strings.Contains(content, "includeMcpJson") {
		t.Fatalf("includeMcpJson extra dropped from subagent frontmatter:\n%s", content)
	}
}

func hasWarning(ws []ir.Warning, cat string) bool {
	for _, w := range ws {
		if w.Category == cat {
			return true
		}
	}
	return false
}

func planHas(p ir.WritePlan, abs string) bool {
	for _, f := range p.Files {
		if f.Path == abs {
			return true
		}
	}
	return false
}

func planPaths(p ir.WritePlan) []string {
	var out []string
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
