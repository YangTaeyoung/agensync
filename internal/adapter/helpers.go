package adapter

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

// PlanFile builds a PlannedFile for path with the given content, reading any
// existing file so the plan/apply layer can render diffs and back it up.
func PlanFile(path string, content []byte) ir.PlannedFile {
	existing, ok := ReadIfExists(path)
	pf := ir.PlannedFile{Path: path, Content: content, Mode: ir.ModeCreate}
	if ok {
		pf.Mode = ir.ModeOverwrite
		pf.Existing = existing
	}
	return pf
}

// ExportSkillDir reads <dir>/SKILL.md (frontmatter name/description + body) and
// bundles every sibling file as a resource FileRef (relative path + bytes).
func ExportSkillDir(dir string, scope ir.Scope, tool string) (ir.Skill, error) {
	raw, ok := ReadIfExists(filepath.Join(dir, "SKILL.md"))
	if !ok {
		return ir.Skill{}, os.ErrNotExist
	}
	fm, body, err := ParseFrontmatter(raw)
	if err != nil {
		return ir.Skill{}, err
	}
	name, _ := fm["name"].(string)
	if name == "" {
		name = filepath.Base(dir)
	}
	desc, _ := fm["description"].(string)
	s := ir.Skill{
		Common: ir.Common{
			ID:          ir.Slug(name),
			Scope:       scope,
			Origin:      dir,
			Body:        body,
			Frontmatter: fm,
			Provenance:  ir.Provenance{Tool: tool, Path: dir},
		},
		Name:        name,
		Description: desc,
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "SKILL.md" {
			return nil
		}
		b, _ := os.ReadFile(p)
		s.Resources = append(s.Resources, ir.FileRef{RelPath: rel, Bytes: b})
		return nil
	})
	return s, nil
}

// RenderSkillMarkdown serializes a Skill back into a SKILL.md document.
func RenderSkillMarkdown(s ir.Skill) ([]byte, error) {
	fm := map[string]any{"name": s.Name}
	if s.Description != "" {
		fm["description"] = s.Description
	}
	if len(s.AllowedTools) > 0 {
		fm["allowed-tools"] = s.AllowedTools
	}
	return RenderFrontmatter(fm, s.Body)
}

var (
	reArguments   = regexp.MustCompile(`\$ARGUMENTS\b`)
	rePositional  = regexp.MustCompile(`\$([0-9]+)`)
	reShellInject = regexp.MustCompile("!`([^`]*)`")
	reFileInject  = regexp.MustCompile(`(?:^|\s)@([A-Za-z0-9_./-]+\.[A-Za-z0-9]+)`)
)

// DetectArgSpec classifies a command body's argument style and placeholders.
func DetectArgSpec(body string) ir.ArgSpec {
	if reArguments.MatchString(body) {
		return ir.ArgSpec{Style: ir.ArgAll, Placeholders: []string{"$ARGUMENTS"}}
	}
	if m := rePositional.FindAllString(body, -1); len(m) > 0 {
		return ir.ArgSpec{Style: ir.ArgPositional, Placeholders: m}
	}
	return ir.ArgSpec{}
}

// DetectShellInjections returns the commands inside !`...` markers.
func DetectShellInjections(body string) []string {
	var out []string
	for _, m := range reShellInject.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// DetectFileInjections returns the paths referenced via @file markers.
func DetectFileInjections(body string) []string {
	var out []string
	for _, m := range reFileInject.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}
