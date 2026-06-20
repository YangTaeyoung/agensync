package adapter

import (
	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/plan"
)

// Base provides the shared, side-effecting Apply implementation. Every adapter
// embeds it so dry-run / backup / conflict behavior is identical across tools;
// the per-tool intelligence lives entirely in PlanImport (which is pure).
type Base struct{}

func (Base) Apply(p ir.WritePlan, opts ApplyOptions) ir.ApplyResult {
	return plan.Apply(p, plan.ApplyOptions{
		DryRun:     opts.DryRun,
		Backup:     opts.Backup,
		OnConflict: opts.OnConflict,
	})
}
