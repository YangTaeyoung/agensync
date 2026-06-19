// Package cline implements the Cline adapter (Tier 2). Cline keeps project
// instructions in .clinerules/ (plus AGENTS.md), personal memory in
// ~/Documents/Cline/Rules, a single global MCP settings file, skills under
// several skill roots, and slash "workflows" under .clinerules/workflows. It
// has no subagent concept, so PlanImport warns rather than silently dropping.
package cline

import (
	"bufio"
	"bytes"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const ignoreFile = ".clineignore"

const id = "cline"

// Cline is the Cline adapter.
type Cline struct{ adapter.Base }

// New returns the Cline adapter.
func New() adapter.ToolAdapter { return Cline{} }

func (Cline) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Cline", Vendor: "Cline", Confidence: "medium"}
}

func (Cline) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: false, ActivationModes: []ir.Activation{ir.ActAlways}},
		MCP: ir.MCPCaps{
			ProjectScope: false, // global-only cline_mcp_settings.json
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "url",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgAll}, Format: "md"},
		Subagents:   "false",
		HomeKeying:  ir.HomeKeyNone,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       false,
		Ignore:      "both",
	}
}

func (Cline) Detect(ctx ir.Context) ir.DetectionResult {
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
		check(filepath.Join(ctx.ProjectPath, "AGENTS.md"), ir.ScopeProject)
		for _, p := range adapter.ListFiles(filepath.Join(ctx.ProjectPath, ".clinerules"), ".md") {
			check(p, ir.ScopeProject)
		}
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".cline", "cline_mcp_settings.json"), ir.ScopeUser)
		for _, p := range adapter.ListFiles(filepath.Join(ctx.HomeDir, "Documents", "Cline", "Rules"), ".md") {
			check(p, ir.ScopeUser)
		}
	}
	return res
}

// ---- Export ----

func (Cline) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		// .clinerules/*.md (all files in the dir), project scope, ActAlways.
		for _, p := range adapter.ListFiles(filepath.Join(ctx.ProjectPath, ".clinerules"), ".md") {
			if b, ok := adapter.ReadIfExists(p); ok {
				out = append(out, mkInstruction(p, string(b), ir.ScopeProject))
			}
		}
		// AGENTS.md (project).
		p := filepath.Join(ctx.ProjectPath, "AGENTS.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, mkInstruction(p, string(b), ir.ScopeProject))
		}
	}
	if ctx.HomeDir != "" {
		// ~/Documents/Cline/Rules/*.md => personal memory (user scope).
		for _, p := range adapter.ListFiles(filepath.Join(ctx.HomeDir, "Documents", "Cline", "Rules"), ".md") {
			if b, ok := adapter.ReadIfExists(p); ok {
				out = append(out, mkInstruction(p, string(b), ir.ScopeUser))
			}
		}
	}
	return out, nil
}

func mkInstruction(path, body string, scope ir.Scope) ir.Instruction {
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	return ir.Instruction{
		Common: ir.Common{
			ID:         ir.Slug(scope2id(scope) + "-" + name),
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

// ExportMcpServers reads the single global ~/.cline/cline_mcp_settings.json.
// Cline has no project-scoped MCP, so everything is user scope.
func (Cline) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	if ctx.HomeDir == "" {
		return nil, nil
	}
	p := filepath.Join(ctx.HomeDir, ".cline", "cline_mcp_settings.json")
	b, ok := adapter.ReadIfExists(p)
	if !ok {
		return nil, nil
	}
	servers, err := adapter.ParseMCPServersJSON(b, adapter.MCPJSONOptions{RootKey: "mcpServers"})
	if err != nil {
		return nil, err
	}
	for i := range servers {
		servers[i].Scope = ir.ScopeUser
		servers[i].Origin = p
		servers[i].Provenance = ir.Provenance{Tool: id, Path: p}
	}
	return servers, nil
}

// ExportSkills scans every skill root cline recognizes: project-scope roots
// (.cline/skills, .clinerules/skills, .claude/skills) and the user root
// (~/.cline/skills).
func (Cline) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".cline", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.ProjectPath, ".clinerules", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.ProjectPath, ".claude", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".cline", "skills"), ir.ScopeUser},
	}
	for _, r := range roots {
		if r.dir == "" || (r.scope == ir.ScopeProject && ctx.ProjectPath == "") || (r.scope == ir.ScopeUser && ctx.HomeDir == "") {
			continue
		}
		for _, d := range adapter.FindSkillDirs(r.dir) {
			if s, err := adapter.ExportSkillDir(d, r.scope, id); err == nil {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// ExportCommands reads cline "workflows" under .clinerules/workflows/*.md.
// Invocation is the file name with its .md suffix, prefixed with "/".
func (Cline) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	if ctx.ProjectPath == "" {
		return nil, nil
	}
	var out []ir.Command
	dir := filepath.Join(ctx.ProjectPath, ".clinerules", "workflows")
	for _, p := range adapter.ListFiles(dir, ".md") {
		b, ok := adapter.ReadIfExists(p)
		if !ok {
			continue
		}
		fm, body, err := adapter.ParseFrontmatter(b)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		desc, _ := fm["description"].(string)
		out = append(out, ir.Command{
			Common: ir.Common{
				ID:          ir.Slug(name),
				Scope:       ir.ScopeProject,
				Origin:      p,
				Body:        body,
				Frontmatter: fm,
				Provenance:  ir.Provenance{Tool: id, Path: p},
			},
			Name:             name,
			Description:      desc,
			ArgSpec:          adapter.DetectArgSpec(body),
			ShellInjections:  adapter.DetectShellInjections(body),
			FileInjections:   adapter.DetectFileInjections(body),
			InvocationFormat: "/" + name + ".md",
		})
	}
	return out, nil
}

// ExportSubagents: cline has no subagent concept.
func (Cline) ExportSubagents(ir.Context) ([]ir.Subagent, error) { return nil, nil }

// ExportProjectState: cline config is profile-global (no path-keyed
// permissions/hooks/trust), but a project-local .clineignore is migratable.
// Honors the declared Ignore:"both" capability by reading it.
func (Cline) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.ProjectPath == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ignoreFile))
	if !ok {
		return ps, nil
	}
	ps.IgnorePatterns = parseIgnore(b)
	ps.IgnoreMode = ir.IgnoreBlock
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

func renderIgnore(patterns []string) []byte {
	sorted := append([]string(nil), patterns...)
	sort.Strings(sorted)
	return []byte(strings.Join(sorted, "\n") + "\n")
}

// ---- PlanImport ----

func (a Cline) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Instructions (project) -> .clinerules/<slug>.md
	// Memory (user) -> ~/Documents/Cline/Rules/<slug>.md
	if opts.Wants("instructions") || opts.Wants("memory") {
		for _, in := range b.Instructions {
			// Cline has no instruction imports; flatten transclusions inline
			// and warn before writing (never silently drop).
			if len(in.Imports) > 0 {
				in = engine.FlattenInstruction(in)
				plan.Warnings = append(plan.Warnings, engine.Warn(
					"instructions", from, id, in.Origin, ir.ActionInline,
					"cline has no imports; transclusions flattened inline"))
			}
			if in.IsMemory() {
				if !opts.Wants("memory") || ctx.HomeDir == "" {
					continue
				}
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.HomeDir, "Documents", "Cline", "Rules", instrSlug(in)+".md"),
					[]byte(in.Body),
				))
			} else {
				if !opts.Wants("instructions") {
					continue
				}
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.ProjectPath, ".clinerules", instrSlug(in)+".md"),
					[]byte(in.Body),
				))
			}
		}
	}

	// MCP -> global-only ~/.cline/cline_mcp_settings.json. Project-scope servers
	// are merged into the single global file; isolation is lost (warn). The
	// per-server isolation warnings must fire regardless of whether a HomeDir
	// was resolved, and when no HomeDir is available the servers cannot be
	// written at all (warn rather than silently dropping them).
	if opts.Wants("mcp") && len(b.McpServers) > 0 {
		for _, s := range b.McpServers {
			if s.Scope == ir.ScopeProject {
				plan.Warnings = append(plan.Warnings, engine.Warn(
					"mcp", from, id, s.Name, ir.ActionMerge,
					"cline MCP is global-only; project isolation lost"))
			}
		}
		if ctx.HomeDir == "" {
			for _, s := range b.McpServers {
				plan.Warnings = append(plan.Warnings, engine.Skip(
					"mcp", from, id, s.Name,
					"cline MCP is global-only and no home dir was resolved; server not written"))
			}
		} else {
			content, err := adapter.RenderMCPServersJSON(b.McpServers, adapter.MCPJSONOptions{
				RootKey:      "mcpServers",
				RemoteURLKey: "url",
				EmitType:     false,
			})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.HomeDir, ".cline", "cline_mcp_settings.json"), content))
			}
		}
	}

	// Skills -> .clinerules/skills/<slug>/SKILL.md (+ resources)
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			dir := filepath.Join(ctx.ProjectPath, ".clinerules", "skills", skillSlug(s))
			if content, err := adapter.RenderSkillMarkdown(s); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
			}
			for _, r := range s.Resources {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
			}
		}
	}

	// Commands -> .clinerules/workflows/<slug>.md (invocation /name.md)
	if opts.Wants("commands") {
		for _, c := range b.Commands {
			fm := map[string]any{}
			if c.Description != "" {
				fm["description"] = c.Description
			}
			content, err := adapter.RenderFrontmatter(fm, c.Body)
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".clinerules", "workflows", cmdSlug(c)+".md"), content))
		}
	}

	// Subagents: UNSUPPORTED -> never silently drop.
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSubagent(from, id, sub.Name))
		}
	}

	// Project state: cline honors a .clineignore file (Ignore:"both" capability)
	// but has no project permission/hook model and trust is built-in. Nothing
	// is silently dropped: ignore patterns are written (collapsing index-only to
	// a block ignore with a warn), and permissions/hooks/trust each warn.
	if opts.Wants("project-state") {
		ps := b.ProjectState
		if len(ps.IgnorePatterns) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ignoreFile), renderIgnore(ps.IgnorePatterns)))
			if ps.IgnoreMode == ir.IgnoreIndex {
				plan.Warnings = append(plan.Warnings, engine.Warn(
					"project-state", from, id, ignoreFile, ir.ActionMerge,
					"cline .clineignore is block-only; index-only ignore collapsed to a block ignore"))
			}
		}
		if len(ps.Permissions.Allow) > 0 || len(ps.Permissions.Deny) > 0 || len(ps.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip(
				"project-state", from, id, "permissions", "cline has no project permission model"))
		}
		if len(ps.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip(
				"project-state", from, id, "hooks", "cline has no hooks model"))
		}
		if ps.Trust != "" {
			plan.Warnings = append(plan.Warnings, engine.Warn(
				"project-state", from, id, "trust", ir.ActionManual,
				"cline trust is built-in; re-grant in the editor if needed"))
		}
	}

	return plan
}

func instrSlug(in ir.Instruction) string {
	if s := ir.Slug(in.ID); s != "" {
		return s
	}
	return "instructions"
}

func skillSlug(s ir.Skill) string {
	if s.Name != "" {
		return ir.Slug(s.Name)
	}
	return ir.Slug(s.ID)
}

func cmdSlug(c ir.Command) string {
	if c.Name != "" {
		return ir.Slug(c.Name)
	}
	return ir.Slug(c.ID)
}
