// Package codex implements the Codex CLI adapter — the reference pattern for a
// format-transform adapter (JSON/Markdown IR → TOML) with env-indirect secrets.
package codex

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/secret"
)

const id = "codex"

const charBudget = 32768 // 32 KiB AGENTS.md budget

// Codex is the Codex CLI adapter.
type Codex struct{ adapter.Base }

// New returns the Codex CLI adapter.
func New() adapter.ToolAdapter { return Codex{} }

func (Codex) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Codex CLI", Vendor: "OpenAI", Confidence: "high"}
}

func (Codex) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: false, ActivationModes: []ir.Activation{ir.ActAlways}, CharBudget: charBudget},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP},
			SecretStyle:  ir.SecretEnvIndirect,
			RemoteURLKey: "url",
			RootKey:      "mcp_servers",
			Format:       "toml",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: false, Format: "none"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyPath,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       true,
		Ignore:      "none",
	}
}

func (Codex) Detect(ctx ir.Context) ir.DetectionResult {
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
		check(filepath.Join(ctx.ProjectPath, ".codex", "config.toml"), ir.ScopeProject)
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".codex", "config.toml"), ir.ScopeUser)
		check(filepath.Join(ctx.HomeDir, ".codex", "AGENTS.md"), ir.ScopeUser)
	}
	return res
}

// ---- Export ----

func (Codex) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		// AGENTS.override.md wins over AGENTS.md when present.
		path := filepath.Join(ctx.ProjectPath, "AGENTS.md")
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, "AGENTS.override.md")); ok {
			out = append(out, mkInstruction(filepath.Join(ctx.ProjectPath, "AGENTS.override.md"), string(b), ir.ScopeProject))
		} else if b, ok := adapter.ReadIfExists(path); ok {
			out = append(out, mkInstruction(path, string(b), ir.ScopeProject))
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".codex", "AGENTS.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, mkInstruction(p, string(b), ir.ScopeUser))
		}
	}
	return out, nil
}

func mkInstruction(path, body string, scope ir.Scope) ir.Instruction {
	return ir.Instruction{
		Common:     ir.Common{ID: ir.Slug(scope2id(scope) + "-agents-md"), Scope: scope, Origin: path, Body: body, Provenance: ir.Provenance{Tool: id, Path: path}},
		Activation: ir.ActAlways,
	}
}

func scope2id(s ir.Scope) string {
	if s == ir.ScopeUser {
		return "user"
	}
	return "project"
}

func (Codex) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	configs := []struct {
		path  string
		scope ir.Scope
	}{
		{filepath.Join(ctx.HomeDir, ".codex", "config.toml"), ir.ScopeUser},
		{filepath.Join(ctx.ProjectPath, ".codex", "config.toml"), ir.ScopeProject},
	}
	for _, c := range configs {
		if c.path == "" {
			continue
		}
		b, ok := adapter.ReadIfExists(c.path)
		if !ok {
			continue
		}
		cfg, err := decodeConfigTOML(b)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(cfg.McpServers))
		for n := range cfg.McpServers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			raw := cfg.McpServers[name]
			s := ir.McpServer{
				Common:       ir.Common{Scope: c.scope, Origin: c.path, Provenance: ir.Provenance{Tool: id, Path: c.path}},
				Name:         name,
				Command:      raw.Command,
				Args:         raw.Args,
				Env:          raw.Env,
				Cwd:          raw.Cwd,
				URL:          raw.URL,
				Headers:      raw.Headers,
				Enabled:      true,
				SecretsStyle: ir.SecretEnvIndirect,
			}
			if raw.Command != "" {
				s.Transport = ir.TransportStdio
			} else {
				s.Transport = ir.TransportHTTP
				if raw.BearerTokenEnvVar != "" {
					s.Auth = &ir.MCPAuth{Type: "bearer", BearerTokenEnvVar: raw.BearerTokenEnvVar}
				}
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (Codex) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".agents", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".agents", "skills"), ir.ScopeUser},
	}
	for _, r := range roots {
		if r.dir == "" {
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

// ExportCommands: Codex has no command files (deprecated → skills).
func (Codex) ExportCommands(ir.Context) ([]ir.Command, error) { return nil, nil }

func (Codex) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".codex", "agents"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".codex", "agents"), ir.ScopeUser},
	}
	for _, d := range dirs {
		for _, p := range adapter.ListFiles(d.dir, ".toml") {
			b, ok := adapter.ReadIfExists(p)
			if !ok {
				continue
			}
			ag, err := decodeAgentTOML(b)
			if err != nil {
				continue
			}
			name := ag.Name
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(p), ".toml")
			}
			out = append(out, ir.Subagent{
				Common:       ir.Common{ID: ir.Slug(name), Scope: d.scope, Origin: p, Body: ag.DeveloperInstructions, Provenance: ir.Provenance{Tool: id, Path: p}},
				Name:         name,
				Description:  ag.Description,
				SystemPrompt: ag.DeveloperInstructions,
				Model:        ag.Model,
			})
		}
	}
	return out, nil
}

func (Codex) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.HomeDir == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.HomeDir, ".codex", "config.toml"))
	if !ok {
		return ps, nil
	}
	cfg, err := decodeConfigTOML(b)
	if err != nil {
		return ps, nil
	}
	abs, _ := filepath.Abs(ctx.ProjectPath)
	if pj, ok := cfg.Projects[abs]; ok {
		switch pj.TrustLevel {
		case "trusted":
			ps.Trust = "trusted"
		case "":
		default:
			ps.Trust = pj.TrustLevel
		}
	}
	return ps, nil
}

// ---- PlanImport (the transform) ----

func (a Codex) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Instructions -> AGENTS.md (flatten imports; enforce 32 KiB budget).
	// Memory (user scope) -> ~/.codex/AGENTS.md.
	if opts.Wants("instructions") || opts.Wants("memory") {
		var projectBodies, memoryBodies []string
		for _, in := range b.Instructions {
			if len(in.Imports) > 0 {
				in = engine.FlattenInstruction(in)
				plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, in.Origin, ir.ActionInline, "codex has no imports; transclusions flattened inline"))
			}
			if in.IsMemory() {
				memoryBodies = append(memoryBodies, in.Body)
			} else {
				projectBodies = append(projectBodies, in.Body)
			}
		}
		if opts.Wants("instructions") && len(projectBodies) > 0 {
			content := enforceBudget(strings.Join(projectBodies, "\n\n"), from, &plan)
			plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, "AGENTS.md"), []byte(content)))
		}
		if opts.Wants("memory") && len(memoryBodies) > 0 {
			if ctx.HomeDir != "" {
				content := enforceBudget(strings.Join(memoryBodies, "\n\n"), from, &plan)
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.HomeDir, ".codex", "AGENTS.md"), []byte(content)))
			} else {
				plan.Warnings = append(plan.Warnings, engine.Warn("memory", from, id, "~/.codex/AGENTS.md", ir.ActionManual, "personal memory needs a home dir; none resolved — add it to ~/.codex/AGENTS.md manually"))
			}
		}
	}

	// MCP -> .codex/config.toml [mcp_servers.*] with secret env-indirection.
	if opts.Wants("mcp") {
		var project []ir.McpServer
		for _, s := range b.McpServers {
			if s.Transport == ir.TransportSSE {
				plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, s.Name, ir.ActionSkip, "codex does not support SSE transport"))
				continue
			}
			project = append(project, s)
		}
		if len(project) > 0 {
			tomlBytes, refs, err := encodeMCPTOML(project, from)
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, ".codex", "config.toml"), tomlBytes))
				if len(refs) > 0 {
					plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, ".env"), renderEnvStub(refs)))
					for _, r := range refs {
						plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, r.Server, ir.ActionManual, "inline secret externalized to env var "+r.EnvVar+" (.env stub); set it before running"))
					}
				}
				// codex MCP TOML cannot express every attribute — warn, never silently drop.
				for _, s := range project {
					plan.Warnings = append(plan.Warnings, mcpAttrLoss(s, from)...)
				}
			}
		}
	}

	// Skills -> .agents/skills/<name>/SKILL.md
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			plan.Files = append(plan.Files, skillFiles(ctx.ProjectPath, s)...)
		}
	}

	// Commands -> converted to skills (codex commands deprecated).
	if opts.Wants("commands") {
		for _, c := range b.Commands {
			skill := commandToSkill(c)
			plan.Files = append(plan.Files, skillFiles(ctx.ProjectPath, skill)...)
			plan.Warnings = append(plan.Warnings, engine.UnsupportedCommand(from, id, c.Name, "codex commands deprecated → converted to skill"))
		}
	}

	// Subagents -> .codex/agents/<name>.toml (TOML only carries name/desc/instructions/model)
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".codex", "agents", ir.Slug(sub.Name)+".toml"),
				encodeAgentTOML(sub)))
			if len(sub.Tools) > 0 {
				plan.Warnings = append(plan.Warnings, engine.Warn("subagents", from, id, sub.Name, ir.ActionSkip, "codex agent TOML cannot restrict tools; tool list dropped"))
			}
			if len(sub.Extras) > 0 {
				plan.Warnings = append(plan.Warnings, engine.Warn("subagents", from, id, sub.Name, ir.ActionSkip, "codex agent TOML cannot represent extra fields; dropped"))
			}
		}
	}

	// Project-state: codex has no project permissions; only trust (re-grant).
	if opts.Wants("project-state") {
		if len(b.ProjectState.Permissions.Allow) > 0 || len(b.ProjectState.Permissions.Deny) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "permissions", ir.ActionSkip, "codex has no project permission model"))
		}
		if b.ProjectState.Trust != "" {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual, "run codex in this dir and grant trust"))
		}
	}

	return plan
}

// mcpAttrLoss reports MCP attributes codex's [mcp_servers] TOML cannot express,
// so they are warned rather than silently dropped.
func mcpAttrLoss(s ir.McpServer, from string) []ir.Warning {
	var ws []ir.Warning
	add := func(reason string) {
		ws = append(ws, engine.Warn("mcp", from, id, s.Name, ir.ActionSkip, reason))
	}
	if len(s.AutoApprove) > 0 {
		add("codex MCP TOML cannot express autoApprove; dropped")
	}
	if s.Timeout > 0 {
		add("codex MCP TOML cannot express timeout; dropped")
	}
	if !s.Enabled {
		add("codex has no per-server disable; server left enabled")
	}
	if len(s.ToolInclude) > 0 || len(s.ToolExclude) > 0 {
		add("codex MCP TOML cannot express tool filters; dropped")
	}
	// Remote headers other than an externalized Authorization are not representable.
	// Iterate in sorted order so the warning report is deterministic.
	keys := make([]string, 0, len(s.Headers))
	for k := range s.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.EqualFold(k, "Authorization") {
			if !secret.LooksLikeSecret(s.Headers[k]) {
				add("codex represents only secret bearer tokens; non-secret Authorization header dropped")
			}
			continue
		}
		add("codex MCP TOML cannot express header " + k + "; dropped")
	}
	return ws
}

func enforceBudget(content, from string, plan *ir.WritePlan) string {
	if len(content) <= charBudget {
		return content
	}
	plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, "AGENTS.md", ir.ActionManual,
		"instruction body exceeds codex 32 KiB budget; review for truncation"))
	return content[:charBudget] + "\n\n<!-- agensync: truncated at 32 KiB codex budget -->\n"
}

func skillFiles(projectPath string, s ir.Skill) []ir.PlannedFile {
	dir := filepath.Join(projectPath, ".agents", "skills", skillSlug(s))
	var files []ir.PlannedFile
	if content, err := adapter.RenderSkillMarkdown(s); err == nil {
		files = append(files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
	}
	for _, r := range s.Resources {
		files = append(files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
	}
	return files
}

func commandToSkill(c ir.Command) ir.Skill {
	name := c.Name
	if name == "" {
		name = c.ID
	}
	return ir.Skill{
		Common:      ir.Common{ID: ir.Slug(name), Body: c.Body},
		Name:        name,
		Description: c.Description,
	}
}

func skillSlug(s ir.Skill) string {
	if s.Name != "" {
		return ir.Slug(s.Name)
	}
	return ir.Slug(s.ID)
}

func renderEnvStub(refs []secretRef) []byte {
	var b strings.Builder
	b.WriteString("# agensync: set these before running; values came from inline secrets in the source config\n")
	seen := map[string]bool{}
	for _, r := range refs {
		if seen[r.EnvVar] {
			continue
		}
		seen[r.EnvVar] = true
		b.WriteString(r.EnvVar + "=" + r.Value + "\n")
	}
	return []byte(b.String())
}
