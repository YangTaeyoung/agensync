package plan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestApplyDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("x"), Mode: ir.ModeCreate}}}
	res := Apply(p, ApplyOptions{DryRun: true})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write")
	}
	if len(res.Written) != 1 { // reported as would-write
		t.Fatalf("expected 1 planned write, got %d", len(res.Written))
	}
}

func TestApplyBacksUpExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(target, []byte("old"), 0o644)
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("new"), Existing: []byte("old"), Mode: ir.ModeOverwrite}}}
	res := Apply(p, ApplyOptions{Backup: true})
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target=%q", got)
	}
	bak, _ := os.ReadFile(target + ".bak")
	if string(bak) != "old" {
		t.Fatalf("backup=%q", bak)
	}
	if len(res.BackedUp) != 1 {
		t.Fatalf("backedup=%v", res.BackedUp)
	}
}

func TestApplySkipMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "skip.md")
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("x"), Mode: ir.ModeSkip}}}
	res := Apply(p, ApplyOptions{})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("skip must not write")
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("skipped=%v", res.Skipped)
	}
}

func TestApplySkipConflictKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.md")
	os.WriteFile(target, []byte("old"), 0o644)
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("new"), Existing: []byte("old"), Mode: ir.ModeOverwrite}}}
	res := Apply(p, ApplyOptions{OnConflict: ir.ActionSkip, Backup: true})
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("skip policy must keep existing, got %q", got)
	}
	if len(res.Skipped) != 1 || len(res.Written) != 0 {
		t.Fatalf("expected skipped=1 written=0, got %+v", res)
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatal("skip must not create a .bak")
	}
}

func TestApplyOverwriteConflictReplaces(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.md")
	os.WriteFile(target, []byte("old"), 0o644)
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("new"), Existing: []byte("old"), Mode: ir.ModeOverwrite}}}
	res := Apply(p, ApplyOptions{OnConflict: ir.ActionOverwrite, Backup: true})
	got, _ := os.ReadFile(target)
	if string(got) != "new" || len(res.Written) != 1 {
		t.Fatalf("overwrite policy must replace, got %q %+v", got, res)
	}
}

// A new (non-existing) file is always written regardless of conflict policy.
func TestApplySkipConflictStillCreatesNew(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.md")
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("x"), Mode: ir.ModeCreate}}}
	res := Apply(p, ApplyOptions{OnConflict: ir.ActionSkip})
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("new file should be created even under skip: %v", err)
	}
	if len(res.Written) != 1 {
		t.Fatalf("written=%v", res.Written)
	}
}

func TestApplyCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c.md")
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("deep"), Mode: ir.ModeCreate}}}
	res := Apply(p, ApplyOptions{})
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "deep" {
		t.Fatalf("nested write failed: %v %q", err, got)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("errors=%v", res.Errors)
	}
}
