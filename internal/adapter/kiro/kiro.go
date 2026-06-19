// Package kiro implements the Kiro (AWS) adapter. Kiro keeps everything in the
// repo under .kiro/ (steering rules, MCP, skills, agents) plus a top-level
// AGENTS.md, and stores personal/global memory as ~/.kiro/steering/*.md files.
package kiro

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "kiro"

// Kiro is the Kiro adapter.
type Kiro struct{ adapter.Base }

// New returns the Kiro adapter.
func New() adapter.ToolAdapter { return Kiro{} }

func (Kiro) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Kiro", Vendor: "AWS/Kiro", Confidence: "high"}
}

func (Kiro) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{
			Imports:         true,
			ActivationModes: []ir.Activation{ir.ActAlways, ir.ActGlob, ir.ActModelDecision, ir.ActManual},
			CharBudget:      0,
		},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			// Kiro's mcp.json represents every remote server with a single "url"
			// key and no "type" discriminator, so it cannot distinguish http from
			// sse. We advertise only the transports we can faithfully represent
			// (stdio + a single remote url that round-trips as sse) and warn per
			// server when an incoming http server is coerced (see PlanImport).
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "url",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: false, Format: "none"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyNone,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       true,
		Ignore:      "none",
	}
}

func (Kiro) Detect(ctx ir.Context) ir.DetectionResult {
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
	dirHas := func(dir string, scope ir.Scope) {
		if dir == "" {
			return
		}
		if files := adapter.ListFiles(dir, ".md"); len(files) > 0 {
			res.Present = true
			res.ScopesFound = append(res.ScopesFound, scope)
			res.Evidence = append(res.Evidence, dir)
		}
	}
	if ctx.ProjectPath != "" {
		dirHas(filepath.Join(ctx.ProjectPath, ".kiro", "steering"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, "AGENTS.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".kiro", "settings", "mcp.json"), ir.ScopeProject)
	}
	if ctx.HomeDir != "" {
		dirHas(filepath.Join(ctx.HomeDir, ".kiro", "steering"), ir.ScopeUser)
		check(filepath.Join(ctx.HomeDir, ".kiro", "settings", "mcp.json"), ir.ScopeUser)
	}
	return res
}

// ---- Export ----

var reFileEmbed = regexp.MustCompile(`#\[\[file:([^\]]+)\]\]`)

func (Kiro) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	steer := func(dir string, scope ir.Scope) {
		if dir == "" {
			return
		}
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
			act, globs := activationFromFM(fm)
			out = append(out, ir.Instruction{
				Common: ir.Common{
					ID:          ir.Slug(name),
					Scope:       scope,
					Origin:      p,
					Body:        body,
					Frontmatter: fm,
					Provenance:  ir.Provenance{Tool: id, Path: p},
				},
				Activation: act,
				Globs:      globs,
				Imports:    parseFileEmbeds(body, dir),
			})
		}
	}
	if ctx.ProjectPath != "" {
		steer(filepath.Join(ctx.ProjectPath, ".kiro", "steering"), ir.ScopeProject)
		// Top-level AGENTS.md (project scope, always-on).
		p := filepath.Join(ctx.ProjectPath, "AGENTS.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, ir.Instruction{
				Common: ir.Common{
					ID:         ir.Slug("project-agents-md"),
					Scope:      ir.ScopeProject,
					Origin:     p,
					Body:       string(b),
					Provenance: ir.Provenance{Tool: id, Path: p},
				},
				Activation: ir.ActAlways,
			})
		}
	}
	if ctx.HomeDir != "" {
		steer(filepath.Join(ctx.HomeDir, ".kiro", "steering"), ir.ScopeUser)
	}
	return out, nil
}

// activationFromFM maps Kiro's 'inclusion' frontmatter to an IR Activation.
//
//	always       -> ActAlways
//	fileMatch    -> ActGlob (globs from fileMatchPattern, string or list)
//	manual       -> ActManual
//	auto         -> ActModelDecision
//	(default)    -> ActAlways
func activationFromFM(fm map[string]any) (ir.Activation, []string) {
	incl, _ := fm["inclusion"].(string)
	switch strings.TrimSpace(incl) {
	case "fileMatch":
		return ir.ActGlob, stringList(fm["fileMatchPattern"])
	case "manual":
		return ir.ActManual, nil
	case "auto":
		return ir.ActModelDecision, nil
	case "always", "":
		return ir.ActAlways, nil
	default:
		return ir.ActAlways, nil
	}
}

// parseFileEmbeds finds #[[file:PATH]] markers and resolves each against dir.
func parseFileEmbeds(body, dir string) []ir.Import {
	var out []ir.Import
	seen := map[string]bool{}
	for _, m := range reFileEmbed.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		imp := ir.Import{Kind: ir.ImpFileEmbed, Target: target}
		if c, ok := adapter.ReadIfExists(filepath.Join(dir, target)); ok {
			imp.Resolved = string(c)
		}
		out = append(out, imp)
	}
	return out
}

func (Kiro) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	configs := []struct {
		path  string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".kiro", "settings", "mcp.json"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".kiro", "settings", "mcp.json"), ir.ScopeUser},
	}
	for _, c := range configs {
		if c.path == "" {
			continue
		}
		b, ok := adapter.ReadIfExists(c.path)
		if !ok {
			continue
		}
		servers, err := adapter.ParseMCPServersJSON(b, adapter.MCPJSONOptions{RootKey: "mcpServers"})
		if err != nil {
			return nil, err
		}
		for i := range servers {
			servers[i].Scope = c.scope
			servers[i].Origin = c.path
			servers[i].Provenance = ir.Provenance{Tool: id, Path: c.path}
		}
		out = append(out, servers...)
	}
	return out, nil
}

func (Kiro) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".kiro", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".kiro", "skills"), ir.ScopeUser},
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

// ExportCommands: Kiro has no command files.
func (Kiro) ExportCommands(ir.Context) ([]ir.Command, error) { return nil, nil }

func (Kiro) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".kiro", "agents"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".kiro", "agents"), ir.ScopeUser},
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
			name, _ := fm["name"].(string)
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(p), ".md")
			}
			desc, _ := fm["description"].(string)
			model, _ := fm["model"].(string)
			sub := ir.Subagent{
				Common: ir.Common{
					ID:          ir.Slug(name),
					Scope:       d.scope,
					Origin:      p,
					Body:        body,
					Frontmatter: fm,
					Provenance:  ir.Provenance{Tool: id, Path: p},
				},
				Name:         name,
				Description:  desc,
				SystemPrompt: strings.TrimRight(body, "\n"),
				Tools:        splitList(fm["tools"]),
				Model:        model,
			}
			if v, ok := fm["includeMcpJson"]; ok {
				sub.Extras = map[string]any{"includeMcpJson": v}
			}
			out = append(out, sub)
		}
	}
	return out, nil
}

// ExportProjectState: Kiro keeps everything in the repo, so there is no
// out-of-band project state to extract.
func (Kiro) ExportProjectState(ir.Context) (ir.ProjectState, error) {
	return ir.ProjectState{}, nil
}

// ---- PlanImport ----

func (Kiro) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Instructions (project) -> .kiro/steering/<slug>.md ;
	// memory (user/enterprise) -> ~/.kiro/steering/<slug>.md.
	if opts.Wants("instructions") || opts.Wants("memory") {
		for _, in := range b.Instructions {
			if in.IsMemory() {
				if !opts.Wants("memory") || ctx.HomeDir == "" {
					continue
				}
				content := renderSteering(in)
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.HomeDir, ".kiro", "steering", instrSlug(in)+".md"), content))
			} else {
				if !opts.Wants("instructions") {
					continue
				}
				content := renderSteering(in)
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.ProjectPath, ".kiro", "steering", instrSlug(in)+".md"), content))
			}
		}
	}

	// MCP -> .kiro/settings/mcp.json (project). Pass through autoApprove/disabled.
	// Kiro encodes remote servers with a single "url" key and no transport
	// discriminator, so http and sse cannot be distinguished on disk: an http
	// server round-trips as sse. Warn per affected server (NEVER silently change
	// transport) before rendering.
	if opts.Wants("mcp") && len(b.McpServers) > 0 {
		for _, s := range b.McpServers {
			if s.Transport == ir.TransportHTTP {
				plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, s.Name, ir.ActionManual,
					"kiro remote MCP uses a single url key; http/sse transport distinction lost (server stored as a plain url)"))
			}
		}
		content, err := adapter.RenderMCPServersJSON(b.McpServers, adapter.MCPJSONOptions{
			RootKey: "mcpServers", RemoteURLKey: "url", EmitType: false,
		})
		if err == nil {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".kiro", "settings", "mcp.json"), content))
		}
	}

	// Skills -> .kiro/skills/<slug>/SKILL.md (+ resources).
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			dir := filepath.Join(ctx.ProjectPath, ".kiro", "skills", skillSlug(s))
			if content, err := adapter.RenderSkillMarkdown(s); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
			}
			for _, r := range s.Resources {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
			}
		}
	}

	// Commands: Kiro has no command files. Convert each to a manual steering file
	// and warn (NEVER drop).
	if opts.Wants("commands") {
		for _, c := range b.Commands {
			name := commandName(c)
			fm := map[string]any{"inclusion": "manual"}
			content, err := adapter.RenderFrontmatter(fm, c.Body)
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".kiro", "steering", ir.Slug(name)+".md"), content))
			plan.Warnings = append(plan.Warnings,
				engine.UnsupportedCommand(from, id, name, "kiro has no commands -> manual steering"))
		}
	}

	// Subagents -> .kiro/agents/<slug>.md.
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			fm := map[string]any{"name": sub.Name}
			if sub.Description != "" {
				fm["description"] = sub.Description
			}
			if len(sub.Tools) > 0 {
				fm["tools"] = strings.Join(sub.Tools, ", ")
			}
			if sub.Model != "" {
				fm["model"] = sub.Model
			}
			// Re-emit known Kiro subagent extras so they round-trip.
			if v, ok := sub.Extras["includeMcpJson"]; ok {
				fm["includeMcpJson"] = v
			}
			content, err := adapter.RenderFrontmatter(fm, sub.SystemPrompt+"\n")
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".kiro", "agents", subSlug(sub)+".md"), content))
		}
	}

	// Project-state: Kiro has no permission model; global hooks are unreliable.
	if opts.Wants("project-state") {
		ps := b.ProjectState
		if len(ps.Permissions.Allow) > 0 || len(ps.Permissions.Deny) > 0 || len(ps.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings,
				engine.Skip("project-state", from, id, "permissions", "kiro has no project permission model"))
		}
		if len(ps.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "hooks", ir.ActionManual,
				"kiro global hooks are unreliable; configure manually"))
		}
	}

	return plan
}

// renderSteering serializes an instruction back into a Kiro steering document,
// deriving the 'inclusion' frontmatter from the Activation and re-emitting any
// #[[file:]] embeds in the body verbatim (imports are natively supported).
func renderSteering(in ir.Instruction) []byte {
	fm := map[string]any{}
	switch in.Activation {
	case ir.ActAlways:
		fm["inclusion"] = "always"
	case ir.ActGlob:
		fm["inclusion"] = "fileMatch"
		if len(in.Globs) == 1 {
			fm["fileMatchPattern"] = in.Globs[0]
		} else if len(in.Globs) > 1 {
			fm["fileMatchPattern"] = in.Globs
		}
	case ir.ActManual:
		fm["inclusion"] = "manual"
	case ir.ActModelDecision:
		fm["inclusion"] = "auto"
	default:
		fm["inclusion"] = "always"
	}
	content, err := adapter.RenderFrontmatter(fm, in.Body)
	if err != nil {
		return []byte(in.Body)
	}
	return content
}

// ---- helpers ----

func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
		return nil
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

func splitList(v any) []string {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func instrSlug(in ir.Instruction) string {
	if in.ID != "" {
		return ir.Slug(in.ID)
	}
	return ir.Slug(filepath.Base(in.Origin))
}

func skillSlug(s ir.Skill) string {
	if s.Name != "" {
		return ir.Slug(s.Name)
	}
	return ir.Slug(s.ID)
}

func subSlug(s ir.Subagent) string {
	if s.Name != "" {
		return ir.Slug(s.Name)
	}
	return ir.Slug(s.ID)
}

func commandName(c ir.Command) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}
