package engine

import (
	"testing"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// stubAdapter returns its configured slices from the matching Export* methods.
type stubAdapter struct {
	id    string
	instr []ir.Instruction
	mcp   []ir.McpServer
}

func (s stubAdapter) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: s.id, DisplayName: s.id}
}
func (s stubAdapter) Detect(ir.Context) ir.DetectionResult { return ir.DetectionResult{} }
func (s stubAdapter) ExportInstructions(ir.Context) ([]ir.Instruction, error) {
	return s.instr, nil
}
func (s stubAdapter) ExportMcpServers(ir.Context) ([]ir.McpServer, error) { return s.mcp, nil }
func (s stubAdapter) ExportSkills(ir.Context) ([]ir.Skill, error)         { return nil, nil }
func (s stubAdapter) ExportCommands(ir.Context) ([]ir.Command, error)     { return nil, nil }
func (s stubAdapter) ExportSubagents(ir.Context) ([]ir.Subagent, error)   { return nil, nil }
func (s stubAdapter) ExportProjectState(ir.Context) (ir.ProjectState, error) {
	return ir.ProjectState{}, nil
}
func (s stubAdapter) Capabilities() ir.Capabilities { return ir.Capabilities{} }
func (s stubAdapter) PlanImport(b ir.AgentConfigBundle, _ ir.Context, _ adapter.ImportOptions) ir.WritePlan {
	return ir.WritePlan{Tool: s.id, Files: []ir.PlannedFile{{Path: "out", Content: []byte(b.Source.Tool)}}}
}
func (s stubAdapter) Apply(ir.WritePlan, adapter.ApplyOptions) ir.ApplyResult {
	return ir.ApplyResult{}
}

func TestExportBuildsBundle(t *testing.T) {
	src := stubAdapter{
		id:    "src",
		instr: []ir.Instruction{{Common: ir.Common{ID: "main", Body: "hi"}}},
		mcp:   []ir.McpServer{{Name: "s1", Transport: ir.TransportStdio, Command: "npx"}},
	}
	b, err := Export(src, ir.Context{ProjectPath: "/p"}, adapter.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Instructions) != 1 || len(b.McpServers) != 1 {
		t.Fatalf("bundle: %+v", b)
	}
	if b.Source.Tool != "src" {
		t.Fatalf("source tool %q", b.Source.Tool)
	}
}

func TestPlanFiltersByCategory(t *testing.T) {
	src := stubAdapter{id: "src", mcp: []ir.McpServer{{Name: "s1"}}, instr: []ir.Instruction{{Common: ir.Common{ID: "m"}}}}
	dst := stubAdapter{id: "dst"}
	b, _ := Export(src, ir.Context{}, adapter.ImportOptions{})
	plan := Plan(dst, b, ir.Context{}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	if plan.Tool != "dst" {
		t.Fatalf("plan tool %q", plan.Tool)
	}
}

// The "memory" category must still pull instructions (which carry user-scope
// memory records) even when "instructions" itself was not requested.
func TestExportMemoryCategoryPullsInstructions(t *testing.T) {
	src := stubAdapter{
		id:    "src",
		instr: []ir.Instruction{{Common: ir.Common{ID: "global", Scope: ir.ScopeUser, Body: "remember me"}}},
	}
	b, err := Export(src, ir.Context{}, adapter.ImportOptions{Categories: map[string]bool{"memory": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Instructions) != 1 || !b.Instructions[0].IsMemory() {
		t.Fatalf("memory instruction not exported: %+v", b.Instructions)
	}
}
