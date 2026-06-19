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
		all, err := src.ExportInstructions(ctx)
		if err != nil {
			return b, err
		}
		// Keep only the requested scope(s): project-scope under "instructions",
		// user/enterprise-scope (personal memory) under "memory". This prevents a
		// target from receiving — and having to silently drop — instructions the
		// user did not ask to migrate.
		for _, in := range all {
			if in.IsMemory() {
				if opts.Wants("memory") {
					b.Instructions = append(b.Instructions, in)
				}
			} else if opts.Wants("instructions") {
				b.Instructions = append(b.Instructions, in)
			}
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
