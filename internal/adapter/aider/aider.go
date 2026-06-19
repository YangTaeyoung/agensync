// Package aider implements the Aider adapter — a Tier 3, instructions-mostly
// target. Aider only natively consumes a CONVENTIONS.md-style instructions file
// (wired via .aider.conf.yml read:) and an .aiderignore; it has no MCP, skills,
// commands, subagents, or personal-memory concept, so PlanImport reports heavy
// loss: every other category the bundle carries is preserved-or-warned, never
// silently dropped.
package aider

import (
	"bufio"
	"bytes"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "aider"

const conventionsFile = "CONVENTIONS.md"
const confFile = ".aider.conf.yml"
const ignoreFile = ".aiderignore"

// Aider is the Aider adapter.
type Aider struct{ adapter.Base }

// New returns the Aider adapter.
func New() adapter.ToolAdapter { return Aider{} }

func (Aider) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Aider", Vendor: "Aider", Confidence: "high"}
}

func (Aider) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: false, ActivationModes: []ir.Activation{ir.ActAlways}},
		MCP:          ir.MCPCaps{ProjectScope: false, Transports: nil, RootKey: "", Format: ""},
		Skills:       false,
		Commands:     ir.CommandCaps{Supported: false, Format: "none"},
		Subagents:    "false",
		HomeKeying:   ir.HomeKeyNone,
		Memory:       ir.MemoryNone,
		Permissions:  false,
		Hooks:        false,
		Ignore:       "block",
	}
}

func (Aider) Detect(ctx ir.Context) ir.DetectionResult {
	var res ir.DetectionResult
	check := func(p string, scope ir.Scope) {
		if p == "" {
			return
		}
		if _, ok := adapter.ReadIfExists(p); ok {
			res.Present = true
			res.ScopesFound = append(res.ScopesFound, scope)
			res.Evidence = append(res.Evidence, p)
		}
	}
	if ctx.ProjectPath != "" {
		check(filepath.Join(ctx.ProjectPath, conventionsFile), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, confFile), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ignoreFile), ir.ScopeProject)
	}
	return res
}

// ---- Export ----

func mkInstruction(path, body string, scope ir.Scope) ir.Instruction {
	return ir.Instruction{
		Common: ir.Common{
			ID:         ir.Slug(scope2id(scope) + "-" + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
			Scope:      scope,
			Origin:     path,
			Body:       body,
			Provenance: ir.Provenance{Tool: id, Path: path},
		},
		Activation: ir.ActAlways,
	}
}

func scope2id(s ir.Scope) string {
	if s == ir.ScopeUser {
		return "user"
	}
	return "project"
}

// ExportInstructions reads CONVENTIONS.md plus any files listed in the
// .aider.conf.yml read: list (those are also instruction context for Aider).
func (Aider) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	if ctx.ProjectPath == "" {
		return nil, nil
	}
	var out []ir.Instruction
	seen := map[string]bool{}

	add := func(rel string) {
		p := filepath.Join(ctx.ProjectPath, rel)
		if seen[p] {
			return
		}
		if b, ok := adapter.ReadIfExists(p); ok {
			seen[p] = true
			out = append(out, mkInstruction(p, string(b), ir.ScopeProject))
		}
	}

	add(conventionsFile)
	for _, rel := range readListFromConf(ctx.ProjectPath) {
		add(rel)
	}
	return out, nil
}

// readListFromConf parses .aider.conf.yml minimally for its read: list of
// referenced instruction files. Returns sorted entries for determinism.
func readListFromConf(projectPath string) []string {
	b, ok := adapter.ReadIfExists(filepath.Join(projectPath, confFile))
	if !ok {
		return nil
	}
	var conf map[string]any
	if yaml.Unmarshal(b, &conf) != nil {
		return nil
	}
	list := readListValue(conf["read"])
	sort.Strings(list)
	return list
}

// readListValue normalizes the read: value, which may be a single string or a
// list of strings, into a []string.
func readListValue(v any) []string {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}

// ExportMcpServers: aider has no MCP support.
func (Aider) ExportMcpServers(ir.Context) ([]ir.McpServer, error) { return nil, nil }

// ExportSkills: aider has no skills.
func (Aider) ExportSkills(ir.Context) ([]ir.Skill, error) { return nil, nil }

// ExportCommands: aider has no commands.
func (Aider) ExportCommands(ir.Context) ([]ir.Command, error) { return nil, nil }

// ExportSubagents: aider has no subagents.
func (Aider) ExportSubagents(ir.Context) ([]ir.Subagent, error) { return nil, nil }

// ExportProjectState extracts .aiderignore patterns (block mode).
func (Aider) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.ProjectPath == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ignoreFile))
	if !ok {
		return ps, nil
	}
	ps.IgnorePatterns = parseIgnore(b)
	if len(ps.IgnorePatterns) > 0 {
		ps.IgnoreMode = ir.IgnoreBlock
	}
	return ps, nil
}

func parseIgnore(b []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ---- PlanImport (heavy loss report) ----

func (Aider) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Collect everything that lands in CONVENTIONS.md: project instructions,
	// preserved memory bodies, and preserved skill bodies.
	var sections []string

	// Instructions: split by scope. Project -> CONVENTIONS.md.
	// Memory (user/enterprise) -> aider has no memory file (MemoryNone), so warn
	// and still preserve the body into CONVENTIONS.md so it is not lost.
	if opts.Wants("instructions") || opts.Wants("memory") {
		for _, in := range b.Instructions {
			if in.IsMemory() {
				if !opts.Wants("memory") {
					continue
				}
				name := memName(in)
				plan.Warnings = append(plan.Warnings, engine.MemoryUnsupported(from, id, ir.MemoryNone, name))
				if body := strings.TrimSpace(in.Body); body != "" {
					sections = append(sections, "<!-- agensync: personal memory from "+from+" ("+name+"); aider has no memory file -->\n"+body)
				}
				continue
			}
			if !opts.Wants("instructions") {
				continue
			}
			if body := strings.TrimSpace(in.Body); body != "" {
				sections = append(sections, body)
			}
		}
	}

	// Skills: aider has no skills. Warn AND preserve the skill body as
	// instructions so the guidance survives (auto-invoke is lost).
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			name := skillName(s)
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSkill(from, id, name))
			sections = append(sections, renderSkillSection(s))
		}
	}

	// Write CONVENTIONS.md if any content accumulated, and wire it via
	// .aider.conf.yml read: so aider actually loads it.
	if len(sections) > 0 {
		content := strings.Join(sections, "\n\n") + "\n"
		convPath := filepath.Join(ctx.ProjectPath, conventionsFile)
		plan.Files = append(plan.Files, adapter.PlanFile(convPath, []byte(content)))

		if confContent, ok := renderConfYml(ctx.ProjectPath); ok {
			plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, confFile), confContent))
		}
		plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, conventionsFile, ir.ActionManual,
			"aider does not auto-load CONVENTIONS.md; wired via .aider.conf.yml read:"))
	}

	// MCP: aider has no MCP support — skip each.
	if opts.Wants("mcp") {
		for _, s := range b.McpServers {
			plan.Warnings = append(plan.Warnings, engine.Skip("mcp", from, id, s.Name, "aider has no MCP support"))
		}
	}

	// Commands: aider has no commands.
	if opts.Wants("commands") {
		for _, c := range b.Commands {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedCommand(from, id, cmdName(c),
				"aider has no commands; see /load or run manually"))
		}
	}

	// Subagents: aider has no subagent concept.
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSubagent(from, id, subName(sub)))
		}
	}

	return plan
}

// renderConfYml produces a .aider.conf.yml that ensures CONVENTIONS.md is in the
// read: list. If a conf already exists, its YAML is merged (existing keys and
// read: entries are preserved). Returns false only if marshaling fails.
func renderConfYml(projectPath string) ([]byte, bool) {
	conf := map[string]any{}
	if b, ok := adapter.ReadIfExists(filepath.Join(projectPath, confFile)); ok {
		var existing map[string]any
		if yaml.Unmarshal(b, &existing) == nil && existing != nil {
			conf = existing
		}
	}

	read := readListValue(conf["read"])
	if !contains(read, conventionsFile) {
		read = append(read, conventionsFile)
	}
	sort.Strings(read)
	conf["read"] = read

	out, err := yaml.Marshal(conf)
	if err != nil {
		return nil, false
	}
	return out, true
}

func renderSkillSection(s ir.Skill) string {
	name := skillName(s)
	var b strings.Builder
	b.WriteString("## Skill: " + name + "\n")
	if s.Description != "" {
		b.WriteString("\n" + s.Description + "\n")
	}
	if body := strings.TrimSpace(s.Body); body != "" {
		b.WriteString("\n" + body)
	}
	return strings.TrimRight(b.String(), "\n")
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func memName(in ir.Instruction) string {
	if in.ID != "" {
		return in.ID
	}
	if in.Origin != "" {
		return filepath.Base(in.Origin)
	}
	return "memory"
}

func skillName(s ir.Skill) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

func cmdName(c ir.Command) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

func subName(s ir.Subagent) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}
