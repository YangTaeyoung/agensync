package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

// fsAdapter reports project-scope presence when a CLAUDE.md exists in the dir.
type fsAdapter struct{ stubAdapter }

func (fsAdapter) Detect(ctx ir.Context) ir.DetectionResult {
	if _, err := os.Stat(filepath.Join(ctx.ProjectPath, "CLAUDE.md")); err == nil {
		return ir.DetectionResult{Present: true, ScopesFound: []ir.Scope{ir.ScopeProject}}
	}
	return ir.DetectionResult{}
}

func mkfile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindProjectRootWalksUpToGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(root, "pkg", "api")
	os.MkdirAll(start, 0o755)
	got := FindProjectRoot(start, t.TempDir())
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestFindProjectRootFallsBackToStart(t *testing.T) {
	// no .git anywhere up to home -> start is the root
	base := t.TempDir()
	start := filepath.Join(base, "a", "b")
	os.MkdirAll(start, 0o755)
	got := FindProjectRoot(start, base)
	if got != start {
		t.Fatalf("got %q want %q (no .git -> start)", got, start)
	}
}

func TestDiscoverProjectDirsFindsNestedSkipsJunk(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "CLAUDE.md"))
	mkfile(t, filepath.Join(root, "services", "a", "CLAUDE.md"))
	mkfile(t, filepath.Join(root, "services", "b", "CLAUDE.md"))
	mkfile(t, filepath.Join(root, "node_modules", "dep", "CLAUDE.md")) // must be skipped
	mkfile(t, filepath.Join(root, ".cache", "CLAUDE.md"))              // hidden -> skipped
	mkfile(t, filepath.Join(root, "services", "c", "README.md"))       // no source config -> not a unit

	got := DiscoverProjectDirs(root, fsAdapter{})
	want := []string{
		root,
		filepath.Join(root, "services", "a"),
		filepath.Join(root, "services", "b"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMigrationContextsSingle(t *testing.T) {
	ctx := ir.Context{ProjectPath: "/p", HomeDir: "/h"}
	got := MigrationContexts(ctx, fsAdapter{}, false)
	if len(got) != 1 || got[0] != ctx {
		t.Fatalf("non-recursive should return the ctx as-is, got %+v", got)
	}
}

func TestMigrationContextsRecursive(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	mkfile(t, filepath.Join(root, "CLAUDE.md"))
	mkfile(t, filepath.Join(root, "svc", "a", "CLAUDE.md"))

	ctx := ir.Context{ProjectPath: filepath.Join(root, "svc", "a"), HomeDir: "/home"}
	got := MigrationContexts(ctx, fsAdapter{}, true)
	if len(got) != 2 {
		t.Fatalf("expected 2 contexts (root + svc/a), got %+v", got)
	}
	byPath := map[string]ir.Context{}
	for _, c := range got {
		byPath[c.ProjectPath] = c
	}
	if byPath[root].HomeDir != "/home" {
		t.Fatalf("root context must carry home (memory layer once): %+v", byPath[root])
	}
	if byPath[filepath.Join(root, "svc", "a")].HomeDir != "" {
		t.Fatalf("nested context must NOT carry home: %+v", byPath[filepath.Join(root, "svc", "a")])
	}
}

func TestDiscoverProjectDirsEmptyWhenNone(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "README.md"))
	if got := DiscoverProjectDirs(root, fsAdapter{}); len(got) != 0 {
		t.Fatalf("expected none, got %v", got)
	}
}
