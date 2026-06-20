package adapter

import (
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

type fakeAdapter struct{ id string }

func (f fakeAdapter) Meta() AdapterMeta {
	return AdapterMeta{ID: f.id, DisplayName: f.id, Confidence: "high"}
}
func (f fakeAdapter) Detect(ir.Context) ir.DetectionResult                    { return ir.DetectionResult{Present: true} }
func (f fakeAdapter) ExportInstructions(ir.Context) ([]ir.Instruction, error) { return nil, nil }
func (f fakeAdapter) ExportMcpServers(ir.Context) ([]ir.McpServer, error)     { return nil, nil }
func (f fakeAdapter) ExportSkills(ir.Context) ([]ir.Skill, error)             { return nil, nil }
func (f fakeAdapter) ExportCommands(ir.Context) ([]ir.Command, error)         { return nil, nil }
func (f fakeAdapter) ExportSubagents(ir.Context) ([]ir.Subagent, error)       { return nil, nil }
func (f fakeAdapter) ExportProjectState(ir.Context) (ir.ProjectState, error) {
	return ir.ProjectState{}, nil
}
func (f fakeAdapter) Capabilities() ir.Capabilities { return ir.Capabilities{} }
func (f fakeAdapter) PlanImport(ir.AgentConfigBundle, ir.Context, ImportOptions) ir.WritePlan {
	return ir.WritePlan{}
}
func (f fakeAdapter) Apply(ir.WritePlan, ApplyOptions) ir.ApplyResult { return ir.ApplyResult{} }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{id: "x"})
	got, ok := r.Get("x")
	if !ok || got.Meta().ID != "x" {
		t.Fatalf("get failed: %v %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("expected missing to be absent")
	}
	if len(r.IDs()) != 1 {
		t.Fatalf("IDs len %d", len(r.IDs()))
	}
}

func TestImportOptionsWants(t *testing.T) {
	all := ImportOptions{}
	if !all.Wants("anything") {
		t.Fatal("empty categories should want everything")
	}
	only := ImportOptions{Categories: map[string]bool{"mcp": true}}
	if !only.Wants("mcp") || only.Wants("skills") {
		t.Fatalf("category filter wrong: %+v", only)
	}
}
