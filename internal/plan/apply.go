package plan

import (
	"os"
	"path/filepath"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

type ApplyOptions struct {
	DryRun     bool
	Backup     bool
	OnConflict ir.Action
}

// Apply writes a plan's files. It honors dry-run (report would-writes, touch
// nothing), backs up overwritten files to <file>.bak, creates parent dirs, and
// skips ModeSkip files. It never deletes; it only adds or overwrites.
func Apply(p ir.WritePlan, opts ApplyOptions) ir.ApplyResult {
	var res ir.ApplyResult
	for _, f := range p.Files {
		if f.Mode == ir.ModeSkip {
			res.Skipped = append(res.Skipped, f.Path)
			continue
		}
		// Conflict policy: skip leaves an existing file untouched. Overwrite (and
		// the empty default) replaces it, backed up below. New files are always written.
		if f.Existing != nil && opts.OnConflict == ir.ActionSkip {
			res.Skipped = append(res.Skipped, f.Path)
			continue
		}
		if opts.DryRun {
			res.Written = append(res.Written, f.Path) // would-write
			continue
		}
		if f.Existing != nil && opts.Backup {
			// Preserve the pre-migration original: never overwrite an existing
			// .bak (e.g. when several targets write the same file in one run).
			if _, err := os.Stat(f.Path + ".bak"); os.IsNotExist(err) {
				if err := os.WriteFile(f.Path+".bak", f.Existing, 0o644); err != nil {
					res.Errors = append(res.Errors, err)
					continue
				}
				res.BackedUp = append(res.BackedUp, f.Path+".bak")
			}
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if err := os.WriteFile(f.Path, f.Content, 0o644); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Written = append(res.Written, f.Path)
	}
	return res
}
