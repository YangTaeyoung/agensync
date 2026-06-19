package adapter

import (
	"os"
	"path/filepath"
	"sort"
)

// ReadIfExists returns (content, true) or (nil, false) when the path is absent.
func ReadIfExists(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// FindSkillDirs returns each subdirectory of root containing a SKILL.md.
func FindSkillDirs(root string) []string {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, ok := ReadIfExists(filepath.Join(root, e.Name(), "SKILL.md")); ok {
				out = append(out, filepath.Join(root, e.Name()))
			}
		}
	}
	return out
}

// ListFiles returns files directly under dir matching the given suffix,
// sorted for deterministic golden output. Returns nil if dir is absent.
func ListFiles(dir, suffix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && hasSuffix(e.Name(), suffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
