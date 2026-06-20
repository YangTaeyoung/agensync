package all

import (
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// fullBundle carries one record in every category, including a user-scope
// (personal-memory) instruction.
func fullBundle() ir.AgentConfigBundle {
	b := ir.NewBundle(ir.Source{Tool: "claude-code"})
	b.Instructions = []ir.Instruction{
		{Common: ir.Common{ID: "proj", Scope: ir.ScopeProject, Body: "project rules"}, Activation: ir.ActAlways},
		{Common: ir.Common{ID: "mem", Scope: ir.ScopeUser, Body: "personal memory"}, Activation: ir.ActAlways},
	}
	b.McpServers = []ir.McpServer{
		{Common: ir.Common{Scope: ir.ScopeProject}, Name: "ctx7", Transport: ir.TransportStdio, Command: "npx", Args: []string{"-y", "pkg"}, Enabled: true},
	}
	b.Skills = []ir.Skill{{Common: ir.Common{ID: "demo", Body: "do it"}, Name: "demo", Description: "demo"}}
	b.Commands = []ir.Command{{Common: ir.Common{ID: "deploy", Body: "deploy now"}, Name: "deploy", Description: "Deploy"}}
	b.Subagents = []ir.Subagent{{Common: ir.Common{ID: "rev"}, Name: "rev", Description: "review", SystemPrompt: "be careful"}}
	b.ProjectState = ir.ProjectState{
		Trust:       "trusted",
		Permissions: ir.Permissions{Allow: []string{"Bash(ls)"}, Deny: []string{"Bash(rm)"}},
	}
	return b
}

// warnCategories returns the set of categories that appear in plan warnings.
func warnCategories(p ir.WritePlan) map[string]bool {
	out := map[string]bool{}
	for _, w := range p.Warnings {
		out[w.Category] = true
	}
	return out
}

// TestNoSilentDropAcrossAdapters is the load-bearing guarantee: for every
// adapter, any category it cannot natively represent (per its own Capabilities)
// MUST surface a structured warning when present in the bundle — never a silent
// drop. This simultaneously checks capability honesty.
func TestNoSilentDropAcrossAdapters(t *testing.T) {
	reg := Default()
	b := fullBundle()
	for _, id := range reg.IDs() {
		a, _ := reg.Get(id)
		caps := a.Capabilities()
		ctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: t.TempDir()}

		var plan ir.WritePlan
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: PlanImport panicked: %v", id, r)
				}
			}()
			plan = a.PlanImport(b, ctx, adapter.ImportOptions{})
		}()
		warned := warnCategories(plan)

		if !caps.Skills && !warned["skills"] {
			t.Errorf("%s: skills unsupported but no warning (silent drop)", id)
		}
		if !caps.Commands.Supported && !warned["commands"] {
			t.Errorf("%s: commands unsupported but no warning (silent drop)", id)
		}
		if caps.Subagents == "false" && !warned["subagents"] {
			t.Errorf("%s: subagents unsupported but no warning (silent drop)", id)
		}
		if caps.Memory != ir.MemoryFile && !warned["memory"] {
			t.Errorf("%s: memory style %q but no memory warning (silent drop)", id, caps.Memory)
		}
		if caps.MCP.Format == "" && !warned["mcp"] {
			t.Errorf("%s: no MCP support but no mcp warning (silent drop)", id)
		}
	}
}

// TestMemoryFileTargetsWriteMemory verifies that every adapter declaring
// Memory==file actually plans a write somewhere under HomeDir for a user-scope
// instruction (personal memory is migrated, not dropped).
func TestMemoryFileTargetsWriteMemory(t *testing.T) {
	reg := Default()
	for _, id := range reg.IDs() {
		a, _ := reg.Get(id)
		if a.Capabilities().Memory != ir.MemoryFile {
			continue
		}
		home := t.TempDir()
		ctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: home}
		b := ir.NewBundle(ir.Source{Tool: "claude-code"})
		b.Instructions = []ir.Instruction{{Common: ir.Common{ID: "mem", Scope: ir.ScopeUser, Body: "personal memory"}, Activation: ir.ActAlways}}
		plan := a.PlanImport(b, ctx, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
		wroteHome := false
		for _, f := range plan.Files {
			if strings.HasPrefix(f.Path, home) {
				wroteHome = true
			}
		}
		if !wroteHome {
			t.Errorf("%s: Memory==file but no write under HomeDir for personal memory: files=%v warns=%v", id, paths(plan), plan.Warnings)
		}
	}
}

// TestNoPlaintextSecretLeak enforces spec §8: env-indirect targets (Codex,
// Copilot) must never re-serialize an inline secret as plaintext.
func TestNoPlaintextSecretLeak(t *testing.T) {
	const token = "sk-ant-leakcanary0123456789abcdef0123"
	reg := Default()
	for _, id := range []string{"codex", "copilot"} {
		a, _ := reg.Get(id)
		b := ir.NewBundle(ir.Source{Tool: "claude-code"})
		b.McpServers = []ir.McpServer{
			{Common: ir.Common{Scope: ir.ScopeProject}, Name: "envsrv", Transport: ir.TransportStdio, Command: "node", Env: map[string]string{"API_KEY": token}, Enabled: true},
			{Common: ir.Common{Scope: ir.ScopeProject}, Name: "httpsrv", Transport: ir.TransportHTTP, URL: "https://x", Headers: map[string]string{"Authorization": "Bearer " + token}, Enabled: true},
		}
		ctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: t.TempDir()}
		plan := a.PlanImport(b, ctx, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
		for _, f := range plan.Files {
			// the .env stub is allowed to hold the secret; config files are not
			if strings.HasSuffix(f.Path, ".env") {
				continue
			}
			if strings.Contains(string(f.Content), token) {
				t.Errorf("%s: plaintext secret leaked into %s", id, f.Path)
			}
		}
	}
}

func paths(p ir.WritePlan) []string {
	var out []string
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

// TestExportUnsupportedReturnsEmptyNotError: exporting an absent category must
// return empty, never an error (spec §3.2).
func TestExportEmptyProjectIsClean(t *testing.T) {
	reg := Default()
	ctx := ir.Context{ProjectPath: t.TempDir(), HomeDir: t.TempDir()}
	for _, id := range reg.IDs() {
		a, _ := reg.Get(id)
		if _, err := engine.Export(a, ctx, adapter.ImportOptions{}); err != nil {
			t.Errorf("%s: export of empty project errored: %v", id, err)
		}
	}
}
