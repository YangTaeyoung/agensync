// Package cursor implements the Cursor (Anysphere) adapter. Cursor stores
// project instructions in AGENTS.md, legacy .cursorrules, and per-rule .mdc
// files under .cursor/rules; MCP in .cursor/mcp.json; skills, commands and
// subagents under .cursor/. Personal "User Rules" live only in the app UI
// (MemoryUI), so personal memory is preserved to a helper file on import.
package cursor

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

const id = "cursor"

// Cursor is the Cursor adapter.
type Cursor struct{ adapter.Base }

// New returns the Cursor adapter.
func New() adapter.ToolAdapter { return Cursor{} }

func (Cursor) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Cursor", Vendor: "Anysphere", Confidence: "high"}
}

func (Cursor) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{
			Imports:         false,
			ActivationModes: []ir.Activation{ir.ActAlways, ir.ActGlob, ir.ActModelDecision, ir.ActManual},
			CharBudget:      0,
		},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "url",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgAll}, Format: "md"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyHash,
		Memory:      ir.MemoryUI,
		Permissions: false,
		Hooks:       false,
		Ignore:      "both",
	}
}

func (Cursor) Detect(ctx ir.Context) ir.DetectionResult {
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
		check(filepath.Join(ctx.ProjectPath, ".cursorrules"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".cursor", "mcp.json"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".cursorignore"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".cursorindexingignore"), ir.ScopeProject)
		if dirHasFiles(filepath.Join(ctx.ProjectPath, ".cursor", "rules"), ".mdc") {
			res.Present = true
			res.ScopesFound = append(res.ScopesFound, ir.ScopeProject)
			res.Evidence = append(res.Evidence, filepath.Join(ctx.ProjectPath, ".cursor", "rules"))
		}
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".cursor", "mcp.json"), ir.ScopeUser)
	}
	return res
}

func dirHasFiles(dir, suffix string) bool {
	return len(adapter.ListFiles(dir, suffix)) > 0
}

// ---- Export ----

func (Cursor) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath == "" {
		return out, nil
	}
	// AGENTS.md (always-on project instruction).
	if p := filepath.Join(ctx.ProjectPath, "AGENTS.md"); true {
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, ir.Instruction{
				Common:     ir.Common{ID: "project-agents-md", Scope: ir.ScopeProject, Origin: p, Body: string(b), Provenance: ir.Provenance{Tool: id, Path: p}},
				Activation: ir.ActAlways,
			})
		}
	}
	// Legacy .cursorrules (always-on project instruction).
	if p := filepath.Join(ctx.ProjectPath, ".cursorrules"); true {
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, ir.Instruction{
				Common:     ir.Common{ID: "project-cursorrules", Scope: ir.ScopeProject, Origin: p, Body: string(b), Provenance: ir.Provenance{Tool: id, Path: p}},
				Activation: ir.ActAlways,
			})
		}
	}
	// .cursor/rules/*.mdc with frontmatter-driven activation.
	rulesDir := filepath.Join(ctx.ProjectPath, ".cursor", "rules")
	for _, p := range adapter.ListFiles(rulesDir, ".mdc") {
		b, ok := adapter.ReadIfExists(p)
		if !ok {
			continue
		}
		fm, body, err := adapter.ParseFrontmatter(b)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".mdc")
		in := ir.Instruction{
			Common:     ir.Common{ID: ir.Slug(name), Scope: ir.ScopeProject, Origin: p, Body: body, Frontmatter: fm, Provenance: ir.Provenance{Tool: id, Path: p}},
			Activation: activationFromFrontmatter(fm),
			Globs:      globsFromFrontmatter(fm),
			CharBudget: 0,
		}
		out = append(out, in)
	}
	return out, nil
}

// activationFromFrontmatter maps a .mdc rule's frontmatter to an Activation:
// alwaysApply:true -> always; globs set -> glob; description only -> model-decision;
// else -> manual.
func activationFromFrontmatter(fm map[string]any) ir.Activation {
	if always, ok := fm["alwaysApply"].(bool); ok && always {
		return ir.ActAlways
	}
	if len(globsFromFrontmatter(fm)) > 0 {
		return ir.ActGlob
	}
	if desc, _ := fm["description"].(string); strings.TrimSpace(desc) != "" {
		return ir.ActModelDecision
	}
	return ir.ActManual
}

func globsFromFrontmatter(fm map[string]any) []string {
	raw, ok := fm["globs"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case string:
		return splitCSV(v)
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (Cursor) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	configs := []struct {
		path  string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".cursor", "mcp.json"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".cursor", "mcp.json"), ir.ScopeUser},
	}
	for _, c := range configs {
		if c.path == "" || (c.scope == ir.ScopeProject && ctx.ProjectPath == "") || (c.scope == ir.ScopeUser && ctx.HomeDir == "") {
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

func (Cursor) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".cursor", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.ProjectPath, ".agents", "skills"), ir.ScopeProject},
	}
	for _, r := range roots {
		if r.dir == "" || ctx.ProjectPath == "" {
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

func (Cursor) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	if ctx.ProjectPath == "" {
		return out, nil
	}
	dir := filepath.Join(ctx.ProjectPath, ".cursor", "commands")
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
			InvocationFormat: "/" + name,
		})
	}
	return out, nil
}

func (Cursor) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	if ctx.ProjectPath == "" {
		return out, nil
	}
	dir := filepath.Join(ctx.ProjectPath, ".cursor", "agents")
	for _, p := range adapter.ListFiles(dir, ".md") {
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
		extras := map[string]any{}
		if v, ok := fm["readonly"]; ok {
			extras["readonly"] = v
		}
		if v, ok := fm["is_background"]; ok {
			extras["is_background"] = v
		}
		if len(extras) == 0 {
			extras = nil
		}
		out = append(out, ir.Subagent{
			Common: ir.Common{
				ID:          ir.Slug(name),
				Scope:       ir.ScopeProject,
				Origin:      p,
				Body:        body,
				Frontmatter: fm,
				Provenance:  ir.Provenance{Tool: id, Path: p},
			},
			Name:         name,
			Description:  desc,
			SystemPrompt: strings.TrimRight(body, "\n"),
			Model:        model,
			Extras:       extras,
		})
	}
	return out, nil
}

func (Cursor) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.ProjectPath == "" {
		return ps, nil
	}
	// Home/global state is hash-keyed (editor DB) and not migratable; do not read it.
	// .cursorignore blocks files from the model (block mode); when absent we fall
	// back to .cursorindexingignore which excludes files from indexing only.
	if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ".cursorignore")); ok {
		ps.IgnorePatterns = parseIgnore(b)
		ps.IgnoreMode = ir.IgnoreBlock
		return ps, nil
	}
	if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ".cursorindexingignore")); ok {
		ps.IgnorePatterns = parseIgnore(b)
		ps.IgnoreMode = ir.IgnoreIndex
		return ps, nil
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

// ---- PlanImport ----

func (Cursor) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	planInstructions(&plan, b, ctx, opts, from)

	// MCP. Cursor supports stdio/http/sse, so no transport is dropped. Servers
	// are keyed by scope: project-scope -> .cursor/mcp.json, user/enterprise-scope
	// -> ~/.cursor/mcp.json. When no HomeDir is resolved, user-scope servers are
	// merged into the project file with a per-server isolation-loss warning so the
	// personal/project boundary is never silently collapsed.
	if opts.Wants("mcp") && len(b.McpServers) > 0 {
		var projectServers, userServers []ir.McpServer
		for _, s := range b.McpServers {
			if s.Scope == ir.ScopeUser || s.Scope == ir.ScopeEnterprise {
				userServers = append(userServers, s)
			} else {
				projectServers = append(projectServers, s)
			}
		}
		if len(userServers) > 0 && ctx.HomeDir != "" {
			content, err := adapter.RenderMCPServersJSON(userServers, adapter.MCPJSONOptions{
				RootKey:      "mcpServers",
				RemoteURLKey: "url",
				EmitType:     true,
			})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.HomeDir, ".cursor", "mcp.json"), content))
			}
		} else if len(userServers) > 0 {
			// No home dir to isolate into: merge into the project file and warn that
			// the user/personal scope boundary was lost.
			projectServers = append(projectServers, userServers...)
			for _, s := range userServers {
				plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, s.Name, ir.ActionMerge, "user-scope MCP server merged into project .cursor/mcp.json; personal/project isolation lost"))
			}
		}
		if len(projectServers) > 0 {
			content, err := adapter.RenderMCPServersJSON(projectServers, adapter.MCPJSONOptions{
				RootKey:      "mcpServers",
				RemoteURLKey: "url",
				EmitType:     true,
			})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, ".cursor", "mcp.json"), content))
			}
		}
	}

	// Skills -> .cursor/skills/<slug>/SKILL.md (+ resources).
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			dir := filepath.Join(ctx.ProjectPath, ".cursor", "skills", skillSlug(s))
			if content, err := adapter.RenderSkillMarkdown(s); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
			}
			for _, r := range s.Resources {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
			}
		}
	}

	// Commands -> .cursor/commands/<slug>.md.
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
				filepath.Join(ctx.ProjectPath, ".cursor", "commands", cmdSlug(c)+".md"), content))
		}
	}

	// Subagents -> .cursor/agents/<slug>.md (re-emit name/model/readonly/is_background).
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			fm := map[string]any{"name": sub.Name}
			if sub.Model != "" {
				fm["model"] = sub.Model
			}
			if v, ok := sub.Extras["readonly"]; ok {
				fm["readonly"] = v
			}
			if v, ok := sub.Extras["is_background"]; ok {
				fm["is_background"] = v
			}
			if sub.Description != "" {
				fm["description"] = sub.Description
			}
			content, err := adapter.RenderFrontmatter(fm, sub.SystemPrompt+"\n")
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".cursor", "agents", ir.Slug(sub.Name)+".md"), content))
		}
	}

	// Project state: cursor has two ignore files but no permission/hook model.
	// .cursorignore blocks files from the model (block mode); .cursorindexingignore
	// excludes them from indexing only (index-only mode). Honoring both modes means
	// nothing is silently collapsed (Capabilities.Ignore=="both").
	if opts.Wants("project-state") {
		if len(b.ProjectState.IgnorePatterns) > 0 {
			ignoreFile := ".cursorignore"
			if b.ProjectState.IgnoreMode == ir.IgnoreIndex {
				ignoreFile = ".cursorindexingignore"
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ignoreFile), renderIgnore(b.ProjectState.IgnorePatterns)))
		}
		if len(b.ProjectState.Permissions.Allow) > 0 || len(b.ProjectState.Permissions.Deny) > 0 || len(b.ProjectState.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "permissions", "cursor has no project permission model"))
		}
		if len(b.ProjectState.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "hooks", "cursor has no hooks"))
		}
	}

	return plan
}

// planInstructions splits instructions by scope: project-scope always-rules are
// concatenated into AGENTS.md, project-scope non-always rules become .mdc files,
// and user/enterprise-scope "memory" is preserved to a helper file with a warning
// (Cursor User Rules are UI-only).
func planInstructions(plan *ir.WritePlan, b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, from string) {
	if !opts.Wants("instructions") && !opts.Wants("memory") {
		return
	}
	var alwaysBodies []string
	var memoryBodies []string
	type nonAlways struct {
		in   ir.Instruction
		slug string
	}
	var rules []nonAlways

	for _, in := range b.Instructions {
		// Cursor instruction files (AGENTS.md, .cursorrules, .mdc) have no import
		// mechanism (Capabilities.Instructions.Imports==false). Flatten any
		// transclusions inline so they are not silently dropped.
		if len(in.Imports) > 0 {
			in = engine.FlattenInstruction(in)
			plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, in.Origin, ir.ActionInline, "cursor has no imports; transclusions flattened inline"))
		}
		if in.IsMemory() {
			memoryBodies = append(memoryBodies, in.Body)
			continue
		}
		if in.Activation == ir.ActAlways || in.Activation == "" {
			alwaysBodies = append(alwaysBodies, in.Body)
		} else {
			rules = append(rules, nonAlways{in: in, slug: ruleSlug(in)})
		}
	}

	if opts.Wants("instructions") {
		if len(alwaysBodies) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, "AGENTS.md"),
				[]byte(strings.Join(alwaysBodies, "\n\n")),
			))
		}
		for _, r := range rules {
			content := renderMdc(r.in)
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".cursor", "rules", r.slug+".mdc"), content))
		}
	}

	// Personal memory (user/enterprise scope): Cursor User Rules are UI-only.
	if opts.Wants("memory") && len(memoryBodies) > 0 {
		plan.Warnings = append(plan.Warnings, engine.MemoryUnsupported(from, id, ir.MemoryUI, "User Rules"))
		plan.Files = append(plan.Files, adapter.PlanFile(
			filepath.Join(ctx.ProjectPath, ".cursor", "agensync-user-rules.md"),
			[]byte(userRulesHeader+strings.Join(memoryBodies, "\n\n")+"\n")))
	}
}

const userRulesHeader = "<!-- agensync: Cursor User Rules live in the app UI (Settings > Rules). " +
	"Paste the content below into User Rules manually; it cannot be written as a file. -->\n\n"

// renderMdc renders a non-always instruction as a .cursor/rules/*.mdc document.
func renderMdc(in ir.Instruction) []byte {
	fm := map[string]any{"alwaysApply": false}
	switch in.Activation {
	case ir.ActGlob:
		if len(in.Globs) > 0 {
			fm["globs"] = in.Globs
		}
	case ir.ActModelDecision:
		if desc := descriptionOf(in); desc != "" {
			fm["description"] = desc
		}
	}
	content, err := adapter.RenderFrontmatter(fm, in.Body)
	if err != nil {
		return []byte(in.Body)
	}
	return content
}

func descriptionOf(in ir.Instruction) string {
	if in.Frontmatter != nil {
		if d, _ := in.Frontmatter["description"].(string); strings.TrimSpace(d) != "" {
			return d
		}
	}
	return ""
}

func ruleSlug(in ir.Instruction) string {
	// For model-decision rules, prefer a slug derived from the description so the
	// filename is human-meaningful; otherwise fall back to the record ID.
	if in.Activation == ir.ActModelDecision {
		if d := descriptionOf(in); d != "" {
			if s := ir.Slug(d); s != "" {
				return s
			}
		}
	}
	if in.ID != "" {
		return ir.Slug(in.ID)
	}
	return "rule"
}

func renderIgnore(patterns []string) []byte {
	sorted := append([]string(nil), patterns...)
	sort.Strings(sorted)
	return []byte(strings.Join(sorted, "\n") + "\n")
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
