package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestPlanFileNewVsOverwrite(t *testing.T) {
	dir := t.TempDir()
	newp := filepath.Join(dir, "new.md")
	pf := PlanFile(newp, []byte("hi"))
	if pf.Mode != ir.ModeCreate || pf.Existing != nil {
		t.Fatalf("new file plan wrong: %+v", pf)
	}
	existp := filepath.Join(dir, "old.md")
	os.WriteFile(existp, []byte("old"), 0o644)
	pf2 := PlanFile(existp, []byte("new"))
	if pf2.Mode != ir.ModeOverwrite || string(pf2.Existing) != "old" {
		t.Fatalf("overwrite plan wrong: %+v", pf2)
	}
}

func TestExportSkillDir(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "demo")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: demo\ndescription: a demo\n---\nDo things\n"), 0o644)
	os.WriteFile(filepath.Join(skill, "helper.py"), []byte("print(1)"), 0o644)
	s, err := ExportSkillDir(skill, ir.ScopeProject, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "demo" || s.Description != "a demo" {
		t.Fatalf("skill meta: %+v", s)
	}
	if len(s.Resources) != 1 || s.Resources[0].RelPath != "helper.py" {
		t.Fatalf("resources: %+v", s.Resources)
	}
}

func TestRenderSkillMarkdown(t *testing.T) {
	s := ir.Skill{Name: "demo", Description: "a demo", Common: ir.Common{Body: "Do things\n"}}
	out, err := RenderSkillMarkdown(s)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, _ := ParseFrontmatter(out)
	if fm["name"] != "demo" || fm["description"] != "a demo" || body != "Do things\n" {
		t.Fatalf("rendered skill bad: fm=%v body=%q", fm, body)
	}
}

func TestDetectArgSpec(t *testing.T) {
	if sp := DetectArgSpec("run $ARGUMENTS now"); sp.Style != ir.ArgAll {
		t.Fatalf("ARGUMENTS -> all, got %+v", sp)
	}
	sp := DetectArgSpec("first $1 second $2")
	if sp.Style != ir.ArgPositional || len(sp.Placeholders) != 2 {
		t.Fatalf("positional detect: %+v", sp)
	}
	if sp := DetectArgSpec("no args here").Style; sp != "" {
		t.Fatalf("no args -> empty, got %q", sp)
	}
}

func TestDetectInjections(t *testing.T) {
	sh := DetectShellInjections("status: !`git status` end")
	if len(sh) != 1 || sh[0] != "git status" {
		t.Fatalf("shell injections: %+v", sh)
	}
	fi := DetectFileInjections("see @docs/spec.md for details")
	if len(fi) != 1 || fi[0] != "docs/spec.md" {
		t.Fatalf("file injections: %+v", fi)
	}
}
