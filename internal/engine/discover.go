package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// FindProjectRoot walks up from start looking for a directory containing a
// `.git` entry, returning that directory as the project root. It stops at (and
// includes) home, and at the filesystem root. If no `.git` is found, start is
// returned unchanged.
func FindProjectRoot(start, home string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	homeAbs, _ := filepath.Abs(home)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if dir == homeAbs {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir { // filesystem root
			break
		}
		dir = parent
	}
	return start
}

// skipDirNames are never recursed into during project discovery.
var skipDirNames = map[string]bool{
	"node_modules": true, "vendor": true, "venv": true,
	"dist": true, "build": true, "target": true, "out": true,
}

// DiscoverProjectDirs walks root's subtree and returns every directory in which
// src reports project-scope configuration (via Detect), so each can be migrated
// in place. VCS, dependency and hidden directories (e.g. .git, .claude,
// node_modules) are skipped. Results are sorted; root comes first if present.
func DiscoverProjectDirs(root string, src adapter.ToolAdapter) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != root && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if src.Detect(ir.Context{ProjectPath: p}).Present {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs
}

// MigrationContexts returns the directories to migrate as resolved contexts.
// Non-recursive: just ctx. Recursive: the project root (nearest .git ancestor)
// plus every nested directory with source config; only the root carries HomeDir
// so the home-dir/memory layer is migrated exactly once.
func MigrationContexts(ctx ir.Context, src adapter.ToolAdapter, recursive bool) []ir.Context {
	if !recursive {
		return []ir.Context{ctx}
	}
	root := FindProjectRoot(ctx.ProjectPath, ctx.HomeDir)
	dirs := DiscoverProjectDirs(root, src)
	if len(dirs) == 0 {
		dirs = []string{root}
	}
	out := make([]ir.Context, len(dirs))
	for i, d := range dirs {
		c := ir.Context{ProjectPath: d}
		if d == root {
			c.HomeDir = ctx.HomeDir
		}
		out[i] = c
	}
	return out
}

func shouldSkipDir(name string) bool {
	if name == "" {
		return false
	}
	if name[0] == '.' { // .git, .claude, .codex, .idea, hidden dirs
		return true
	}
	return skipDirNames[name]
}
