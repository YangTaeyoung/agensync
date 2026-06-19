package engine

import (
	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// Export reads all requested categories from the source adapter into an IR bundle.
// The "memory" category shares the instructions export, since personal/global
// memory is carried as user-scope Instruction records.
func Export(src adapter.ToolAdapter, ctx ir.Context, opts adapter.ImportOptions) (ir.AgentConfigBundle, error) {
	b := ir.NewBundle(ir.Source{Tool: src.Meta().ID, Version: src.Meta().DisplayName, ProjectPath: ctx.ProjectPath})
	var err error
	if opts.Wants("instructions") || opts.Wants("memory") {
		if b.Instructions, err = src.ExportInstructions(ctx); err != nil {
			return b, err
		}
	}
	if opts.Wants("mcp") {
		if b.McpServers, err = src.ExportMcpServers(ctx); err != nil {
			return b, err
		}
	}
	if opts.Wants("skills") {
		if b.Skills, err = src.ExportSkills(ctx); err != nil {
			return b, err
		}
	}
	if opts.Wants("commands") {
		if b.Commands, err = src.ExportCommands(ctx); err != nil {
			return b, err
		}
	}
	if opts.Wants("subagents") {
		if b.Subagents, err = src.ExportSubagents(ctx); err != nil {
			return b, err
		}
	}
	if opts.Wants("project-state") {
		if b.ProjectState, err = src.ExportProjectState(ctx); err != nil {
			return b, err
		}
	}
	return b, nil
}

// Plan asks the destination adapter to produce a WritePlan for the bundle.
// The adapter's PlanImport is responsible for capability-driven gotcha warnings.
func Plan(dst adapter.ToolAdapter, b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	return dst.PlanImport(b, ctx, opts)
}
