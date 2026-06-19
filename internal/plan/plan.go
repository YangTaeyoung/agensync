// Package plan renders WritePlan diffs and applies them with backups and a
// conflict policy. Apply is the only side-effecting step in the pipeline.
package plan

import (
	"fmt"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

// RenderDiff produces a human-readable preview of a WritePlan: which files are
// new, overwritten or merged, plus the structured loss/transformation warnings.
func RenderDiff(p ir.WritePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan for %s:\n", p.Tool)
	for _, f := range p.Files {
		switch {
		case f.Mode == ir.ModeSkip:
			fmt.Fprintf(&b, "  = %s (skip)\n", f.Path)
		case f.Existing == nil:
			fmt.Fprintf(&b, "  + %s (new file, %d bytes)\n", f.Path, len(f.Content))
		case f.Mode == ir.ModeMerge:
			fmt.Fprintf(&b, "  ~ %s (merge)\n", f.Path)
		default:
			fmt.Fprintf(&b, "  ! %s (overwrite, was %d bytes)\n", f.Path, len(f.Existing))
		}
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "  warn: %s\n", w.String())
	}
	return b.String()
}
