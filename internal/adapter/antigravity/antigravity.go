// Package antigravity implements the Google Antigravity adapter (Tier 2). It is
// a native MD/JSON tool with version churn, so config dirs are matched fuzzily
// (.agents/ or .agent/) and the MCP JSON dialect uses a serverUrl remote key and
// permits // comments on read.
package antigravity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "antigravity"

// AG is the Antigravity adapter.
type AG struct{ adapter.Base }

// New returns the Antigravity adapter.
func New() adapter.ToolAdapter { return AG{} }

func (AG) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Antigravity", Vendor: "Google", Confidence: "medium"}
}

func (AG) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: false, ActivationModes: []ir.Activation{ir.ActAlways}},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "serverUrl",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgAll}, Format: "md"},
		Subagents:   "false",
		HomeKeying:  ir.HomeKeyPath,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       false,
		Ignore:      "none",
	}
}

// agentsDir returns whichever of .agents/ or .agent/ exists under base.
// Reads prefer the existing dir; the default ".agents" is returned when neither
// exists so writes have a deterministic target (version churn -> fuzzy paths).
func agentsDir(base string) string {
	if base == "" {
		return ".agents"
	}
	for _, cand := range []string{".agents", ".agent"} {
		if fi, err := os.Stat(filepath.Join(base, cand)); err == nil && fi.IsDir() {
			return cand
		}
	}
	return ".agents"
}

func (AG) Detect(ctx ir.Context) ir.DetectionResult {
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
		dir := agentsDir(ctx.ProjectPath)
		check(filepath.Join(ctx.ProjectPath, "AGENTS.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, "GEMINI.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, dir, "mcp_config.json"), ir.ScopeProject)
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".gemini", "AGENTS.md"), ir.ScopeUser)
		check(filepath.Join(ctx.HomeDir, ".gemini", "config", "mcp_config.json"), ir.ScopeUser)
	}
	return res
}

// ---- Export ----

func mkInstruction(path, body string, scope ir.Scope, idHint string) ir.Instruction {
	return ir.Instruction{
		Common:     ir.Common{ID: ir.Slug(idHint), Scope: scope, Origin: path, Body: body, Provenance: ir.Provenance{Tool: id, Path: path}},
		Activation: ir.ActAlways,
	}
}

func (AG) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, "AGENTS.md")); ok {
			out = append(out, mkInstruction(filepath.Join(ctx.ProjectPath, "AGENTS.md"), string(b), ir.ScopeProject, "project-agents-md"))
		}
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, "GEMINI.md")); ok {
			out = append(out, mkInstruction(filepath.Join(ctx.ProjectPath, "GEMINI.md"), string(b), ir.ScopeProject, "project-gemini-md"))
		}
		rulesDir := filepath.Join(ctx.ProjectPath, agentsDir(ctx.ProjectPath), "rules")
		for _, p := range adapter.ListFiles(rulesDir, ".md") {
			if b, ok := adapter.ReadIfExists(p); ok {
				name := strings.TrimSuffix(filepath.Base(p), ".md")
				out = append(out, mkInstruction(p, string(b), ir.ScopeProject, "project-rule-"+name))
			}
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".gemini", "AGENTS.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, mkInstruction(p, string(b), ir.ScopeUser, "user-agents-md"))
		}
	}
	return out, nil
}

func tagScope(servers []ir.McpServer, scope ir.Scope, origin string) []ir.McpServer {
	for i := range servers {
		servers[i].Scope = scope
		servers[i].Origin = origin
		servers[i].Provenance = ir.Provenance{Tool: id, Path: origin}
	}
	return servers
}

func (AG) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	opts := adapter.MCPJSONOptions{RootKey: "mcpServers", StripComments: true}
	configs := []struct {
		path  string
		scope ir.Scope
	}{}
	if ctx.ProjectPath != "" {
		configs = append(configs, struct {
			path  string
			scope ir.Scope
		}{filepath.Join(ctx.ProjectPath, agentsDir(ctx.ProjectPath), "mcp_config.json"), ir.ScopeProject})
	}
	if ctx.HomeDir != "" {
		configs = append(configs, struct {
			path  string
			scope ir.Scope
		}{filepath.Join(ctx.HomeDir, ".gemini", "config", "mcp_config.json"), ir.ScopeUser})
	}
	for _, c := range configs {
		b, ok := adapter.ReadIfExists(c.path)
		if !ok {
			continue
		}
		servers, err := adapter.ParseMCPServersJSON(b, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, tagScope(servers, c.scope, c.path)...)
	}
	return out, nil
}

func (AG) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{}
	if ctx.ProjectPath != "" {
		roots = append(roots, struct {
			dir   string
			scope ir.Scope
		}{filepath.Join(ctx.ProjectPath, agentsDir(ctx.ProjectPath), "skills"), ir.ScopeProject})
	}
	if ctx.HomeDir != "" {
		roots = append(roots, struct {
			dir   string
			scope ir.Scope
		}{filepath.Join(ctx.HomeDir, ".gemini", "skills"), ir.ScopeUser})
	}
	for _, r := range roots {
		for _, d := range adapter.FindSkillDirs(r.dir) {
			if s, err := adapter.ExportSkillDir(d, r.scope, id); err == nil {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (AG) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{}
	if ctx.ProjectPath != "" {
		dirs = append(dirs, struct {
			dir   string
			scope ir.Scope
		}{filepath.Join(ctx.ProjectPath, agentsDir(ctx.ProjectPath), "workflows"), ir.ScopeProject})
	}
	if ctx.HomeDir != "" {
		dirs = append(dirs, struct {
			dir   string
			scope ir.Scope
		}{filepath.Join(ctx.HomeDir, ".gemini", "antigravity", "global_workflows"), ir.ScopeUser})
	}
	for _, d := range dirs {
		for _, p := range adapter.ListFiles(d.dir, ".md") {
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
					Scope:       d.scope,
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
				InvocationFormat: "/" + name,
			})
		}
	}
	return out, nil
}

// ExportSubagents: Antigravity has no subagent concept.
func (AG) ExportSubagents(ir.Context) ([]ir.Subagent, error) { return nil, nil }

func (AG) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.HomeDir == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.HomeDir, ".gemini", "antigravity-cli", "settings.json"))
	if !ok {
		return ps, nil
	}
	var root struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
	}
	if json.Unmarshal(b, &root) != nil {
		return ps, nil
	}
	abs, _ := filepath.Abs(ctx.ProjectPath)
	for _, w := range root.TrustedWorkspaces {
		wAbs, _ := filepath.Abs(w)
		if wAbs == abs || w == ctx.ProjectPath {
			ps.Trust = "trusted"
			break
		}
	}
	return ps, nil
}

// ---- PlanImport ----

func (a AG) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool
	dir := agentsDir(ctx.ProjectPath)

	// Instructions (project) -> AGENTS.md ; memory (user) -> ~/.gemini/AGENTS.md
	if opts.Wants("instructions") || opts.Wants("memory") {
		var projectBodies, memoryBodies []string
		for _, in := range b.Instructions {
			// antigravity has no instruction imports (caps.Imports=false): flatten
			// transclusions inline and warn so nothing is silently dropped.
			if len(in.Imports) > 0 {
				in = engine.FlattenInstruction(in)
				plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, in.Origin, ir.ActionInline,
					"antigravity has no imports; transclusions flattened inline"))
			}
			if in.IsMemory() {
				memoryBodies = append(memoryBodies, in.Body)
			} else {
				projectBodies = append(projectBodies, in.Body)
			}
		}
		if opts.Wants("instructions") && len(projectBodies) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, "AGENTS.md"),
				[]byte(strings.Join(projectBodies, "\n\n")),
			))
			if geminiConflict(b) {
				plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, "GEMINI.md", ir.ActionManual,
					"AGENTS.md and GEMINI.md both apply; agensync prefers AGENTS.md"))
			}
		}
		if opts.Wants("memory") && len(memoryBodies) > 0 && ctx.HomeDir != "" {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.HomeDir, ".gemini", "AGENTS.md"),
				[]byte(strings.Join(memoryBodies, "\n\n")),
			))
		}
	}

	// MCP -> <fuzzydir>/mcp_config.json (serverUrl remap, no comments, drop timeout, no type).
	// antigravity's serverUrl dialect has no transport discriminator and no SSE
	// support (caps: stdio+http only); an SSE server would be silently coerced to
	// an HTTP serverUrl entry, so skip it with a warning instead (never drop).
	if opts.Wants("mcp") && len(b.McpServers) > 0 {
		var servers []ir.McpServer
		for _, s := range b.McpServers {
			if s.Transport == ir.TransportSSE {
				plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, s.Name, ir.ActionSkip,
					"antigravity has no SSE transport (serverUrl is HTTP-only); server skipped"))
				continue
			}
			servers = append(servers, s)
		}
		if len(servers) > 0 {
			content, err := adapter.RenderMCPServersJSON(servers, adapter.MCPJSONOptions{
				RootKey:      "mcpServers",
				RemoteURLKey: "serverUrl",
				EmitType:     false,
				DropTimeout:  true,
			})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.ProjectPath, dir, "mcp_config.json"), content))
			}
		}
	}

	// Skills -> <fuzzydir>/skills/<slug>/SKILL.md (+ resources)
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			skillDir := filepath.Join(ctx.ProjectPath, dir, "skills", skillSlug(s))
			if content, err := adapter.RenderSkillMarkdown(s); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(skillDir, "SKILL.md"), content))
			}
			for _, r := range s.Resources {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(skillDir, r.RelPath), r.Bytes))
			}
		}
	}

	// Commands (Workflows) -> <fuzzydir>/workflows/<slug>.md
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
				filepath.Join(ctx.ProjectPath, dir, "workflows", cmdSlug(c)+".md"), content))
		}
	}

	// Subagents: UNSUPPORTED -> warn for each (never drop).
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSubagent(from, id, sub.Name))
		}
	}

	// Project-state: trust is not writable as a project file -> manual warn.
	if opts.Wants("project-state") {
		if len(b.ProjectState.Permissions.Allow) > 0 || len(b.ProjectState.Permissions.Deny) > 0 || len(b.ProjectState.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "permissions", ir.ActionSkip, "antigravity has no project permission model"))
		}
		if len(b.ProjectState.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "hooks", "antigravity has no hooks model"))
		}
		if b.ProjectState.Trust != "" {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual,
				"add this workspace to trustedWorkspaces in ~/.gemini/antigravity-cli/settings.json"))
		}
	}

	return plan
}

// geminiConflict reports whether the bundle implies a GEMINI.md also applies,
// either via a source provenance path or a raw unmapped artifact.
func geminiConflict(b ir.AgentConfigBundle) bool {
	for _, in := range b.Instructions {
		if strings.HasSuffix(in.Origin, "GEMINI.md") || strings.HasSuffix(in.Provenance.Path, "GEMINI.md") {
			return true
		}
	}
	for _, u := range b.Unmapped {
		if strings.HasSuffix(u.OrigPath, "GEMINI.md") {
			return true
		}
	}
	return false
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
