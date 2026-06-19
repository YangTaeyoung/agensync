// Package copilot implements the GitHub Copilot adapter, modeling the Copilot
// CLI surface (~/.copilot + .github/*) as primary. It is a native MD/JSON
// adapter: instructions, MCP, skills, prompt-commands and ".agent.md" subagents.
package copilot

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "copilot"

// Copilot is the GitHub Copilot adapter.
type Copilot struct{ adapter.Base }

// New returns the GitHub Copilot adapter.
func New() adapter.ToolAdapter { return Copilot{} }

func (Copilot) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "GitHub Copilot", Vendor: "GitHub", Confidence: "high"}
}

func (Copilot) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: false, ActivationModes: []ir.Activation{ir.ActAlways, ir.ActGlob}, CharBudget: 0},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			// Inline secrets are externalized to env-var refs + a .env stub on
			// import; copilot configs never carry plaintext credentials (§8).
			SecretStyle:  ir.SecretEnvIndirect,
			RemoteURLKey: "url",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgAll}, Format: "md"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyPath,
		Memory:      ir.MemoryFile,
		Permissions: true,
		Hooks:       false,
		Ignore:      "none",
	}
}

func (Copilot) Detect(ctx ir.Context) ir.DetectionResult {
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
		check(filepath.Join(ctx.ProjectPath, ".github", "copilot-instructions.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, "AGENTS.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".github", "mcp.json"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".mcp.json"), ir.ScopeProject)
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".copilot", "copilot-instructions.md"), ir.ScopeUser)
		check(filepath.Join(ctx.HomeDir, ".copilot", "mcp-config.json"), ir.ScopeUser)
	}
	return res
}

// ---- Export ----

func mkInstruction(idSuffix, path, body string, scope ir.Scope, act ir.Activation, globs []string) ir.Instruction {
	return ir.Instruction{
		Common: ir.Common{
			ID:         ir.Slug(idSuffix),
			Scope:      scope,
			Origin:     path,
			Body:       body,
			Provenance: ir.Provenance{Tool: id, Path: path},
		},
		Activation: act,
		Globs:      globs,
	}
}

func (Copilot) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ".github", "copilot-instructions.md")); ok {
			p := filepath.Join(ctx.ProjectPath, ".github", "copilot-instructions.md")
			out = append(out, mkInstruction("project-copilot-instructions", p, string(b), ir.ScopeProject, ir.ActAlways, nil))
		}
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, "AGENTS.md")); ok {
			p := filepath.Join(ctx.ProjectPath, "AGENTS.md")
			out = append(out, mkInstruction("project-agents-md", p, string(b), ir.ScopeProject, ir.ActAlways, nil))
		}
		// .github/instructions/*.instructions.md -> glob-activated, applyTo -> Globs
		dir := filepath.Join(ctx.ProjectPath, ".github", "instructions")
		for _, p := range adapter.ListFiles(dir, ".instructions.md") {
			b, ok := adapter.ReadIfExists(p)
			if !ok {
				continue
			}
			fm, body, err := adapter.ParseFrontmatter(b)
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(p), ".instructions.md")
			globs := splitGlobs(fm["applyTo"])
			act := ir.ActGlob
			if len(globs) == 0 {
				act = ir.ActAlways
			}
			in := mkInstruction(name, p, body, ir.ScopeProject, act, globs)
			in.Frontmatter = fm
			out = append(out, in)
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".copilot", "copilot-instructions.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, mkInstruction("user-copilot-instructions", p, string(b), ir.ScopeUser, ir.ActAlways, nil))
		}
	}
	return out, nil
}

// splitGlobs parses an applyTo frontmatter value (comma-separated string or a
// YAML list) into a glob slice.
func splitGlobs(v any) []string {
	switch t := v.(type) {
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				if p := strings.TrimSpace(s); p != "" {
					out = append(out, p)
				}
			}
		}
		return out
	}
	return nil
}

func tagScope(servers []ir.McpServer, scope ir.Scope, origin string) []ir.McpServer {
	for i := range servers {
		servers[i].Scope = scope
		servers[i].Origin = origin
		servers[i].Provenance = ir.Provenance{Tool: id, Path: origin}
	}
	return servers
}

func (Copilot) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	opts := adapter.MCPJSONOptions{RootKey: "mcpServers"}
	if ctx.ProjectPath != "" {
		// .github/mcp.json preferred; fall back to .mcp.json.
		for _, rel := range []string{
			filepath.Join(".github", "mcp.json"),
			".mcp.json",
		} {
			p := filepath.Join(ctx.ProjectPath, rel)
			b, ok := adapter.ReadIfExists(p)
			if !ok {
				continue
			}
			servers, err := adapter.ParseMCPServersJSON(b, opts)
			if err != nil {
				return nil, err
			}
			out = append(out, tagScope(servers, ir.ScopeProject, p)...)
			break
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".copilot", "mcp-config.json")
		if b, ok := adapter.ReadIfExists(p); ok {
			servers, err := adapter.ParseMCPServersJSON(b, opts)
			if err != nil {
				return nil, err
			}
			out = append(out, tagScope(servers, ir.ScopeUser, p)...)
		}
	}
	return out, nil
}

func (Copilot) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".github", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".copilot", "skills"), ir.ScopeUser},
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

func (Copilot) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	if ctx.ProjectPath == "" {
		return out, nil
	}
	dir := filepath.Join(ctx.ProjectPath, ".github", "prompts")
	for _, p := range adapter.ListFiles(dir, ".prompt.md") {
		b, ok := adapter.ReadIfExists(p)
		if !ok {
			continue
		}
		fm, body, err := adapter.ParseFrontmatter(b)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".prompt.md")
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

func (Copilot) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".github", "agents"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".copilot", "agents"), ir.ScopeUser},
	}
	for _, d := range dirs {
		if d.dir == "" {
			continue
		}
		// NOTE the ".agent.md" extension (not plain ".md").
		for _, p := range adapter.ListFiles(d.dir, ".agent.md") {
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
				name = strings.TrimSuffix(filepath.Base(p), ".agent.md")
			}
			desc, _ := fm["description"].(string)
			model, _ := fm["model"].(string)
			out = append(out, ir.Subagent{
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
			})
		}
	}
	return out, nil
}

func splitList(v any) []string {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				if v := strings.TrimSpace(s); v != "" {
					out = append(out, v)
				}
			}
		}
		return out
	}
	return nil
}

func (Copilot) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.HomeDir == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.HomeDir, ".copilot", "permissions-config.json"))
	if !ok {
		return ps, nil
	}
	var raw struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
		Ask   []string `json:"ask"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return ps, nil
	}
	ps.Permissions.Allow = raw.Allow
	ps.Permissions.Deny = raw.Deny
	ps.Permissions.Ask = raw.Ask
	return ps, nil
}

// ---- PlanImport ----

func (a Copilot) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	a.planInstructions(b, ctx, opts, &plan, from)
	a.planMCP(b, ctx, opts, &plan, from)
	a.planSkills(b, ctx, opts, &plan, from)
	a.planCommands(b, ctx, opts, &plan, from)
	a.planSubagents(b, ctx, opts, &plan, from)
	a.planProjectState(b, ctx, opts, &plan, from)

	return plan
}

// planInstructions splits by scope: project-scope -> .github (always/none ->
// copilot-instructions.md, glob -> .github/instructions/<slug>.instructions.md
// with applyTo frontmatter); user/enterprise (memory) ->
// ~/.copilot/copilot-instructions.md.
func (Copilot) planInstructions(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("instructions") && !opts.Wants("memory") {
		return
	}
	var alwaysBodies []string
	var memoryBodies []ir.Instruction

	for _, in := range b.Instructions {
		// Copilot has no transclusion/import mechanism (Capabilities.Imports ==
		// false): flatten resolved imports inline and warn so nothing is lost.
		if len(in.Imports) > 0 {
			in = engine.FlattenInstruction(in)
			plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, in.Origin, ir.ActionInline,
				"copilot has no imports; transclusions flattened inline"))
		}
		if in.IsMemory() {
			memoryBodies = append(memoryBodies, in)
			continue
		}
		if !opts.Wants("instructions") {
			continue
		}
		if in.Activation == ir.ActGlob && len(in.Globs) > 0 {
			fm := map[string]any{"applyTo": strings.Join(in.Globs, ",")}
			content, err := adapter.RenderFrontmatter(fm, in.Body)
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".github", "instructions", instrSlug(in)+".instructions.md"),
				content))
		} else {
			alwaysBodies = append(alwaysBodies, in.Body)
		}
	}

	if opts.Wants("instructions") && len(alwaysBodies) > 0 {
		plan.Files = append(plan.Files, adapter.PlanFile(
			filepath.Join(ctx.ProjectPath, ".github", "copilot-instructions.md"),
			[]byte(strings.Join(alwaysBodies, "\n\n")+"\n")))
	}

	// Memory -> ~/.copilot/copilot-instructions.md (MemoryFile).
	if opts.Wants("memory") && len(memoryBodies) > 0 {
		if ctx.HomeDir == "" {
			for _, in := range memoryBodies {
				plan.Warnings = append(plan.Warnings, engine.Warn("memory", from, id, in.ID, ir.ActionManual,
					"no HomeDir resolved; cannot write ~/.copilot/copilot-instructions.md"))
			}
			return
		}
		bodies := make([]string, 0, len(memoryBodies))
		for _, in := range memoryBodies {
			bodies = append(bodies, in.Body)
		}
		plan.Files = append(plan.Files, adapter.PlanFile(
			filepath.Join(ctx.HomeDir, ".copilot", "copilot-instructions.md"),
			[]byte(strings.Join(bodies, "\n\n")+"\n")))
	}
}

// planMCP renders .github/mcp.json and ALWAYS appends the two structured
// gotchas: the VS Code root-key remap warning and the cloud out-of-scope skip.
func (Copilot) planMCP(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("mcp") {
		return
	}
	if len(b.McpServers) > 0 {
		// Externalize any inline secret to an env-var reference + .env stub so
		// the rendered JSON never contains plaintext credentials (§8).
		servers, refs := externalizeSecrets(b.McpServers)
		content, err := adapter.RenderMCPServersJSON(servers, adapter.MCPJSONOptions{
			RootKey:      "mcpServers",
			RemoteURLKey: "url",
			EmitType:     true,
		})
		if err == nil {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".github", "mcp.json"), content))
			if len(refs) > 0 {
				plan.Files = append(plan.Files, adapter.PlanFile(
					filepath.Join(ctx.ProjectPath, ".env"), renderEnvStub(refs)))
				for _, r := range refs {
					plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, r.Server, ir.ActionManual,
						"inline secret externalized to env var "+r.EnvVar+" (.env stub); set it before running"))
				}
			}
		}
	}
	// Always warn about the two known divergences (independent of server count).
	plan.Warnings = append(plan.Warnings,
		engine.Warn("mcp", from, id, "vscode", ir.ActionManual,
			"VS Code uses .vscode/mcp.json with root key 'servers' (not mcpServers); remap if targeting the IDE"),
		engine.Skip("mcp", from, id, "cloud",
			"Copilot coding-agent cloud MCP is server-side, out of scope"))
}

func (Copilot) planSkills(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("skills") {
		return
	}
	for _, s := range b.Skills {
		if ctx.HomeDir == "" {
			plan.Warnings = append(plan.Warnings, engine.Warn("skills", from, id, skillSlug(s), ir.ActionManual,
				"no HomeDir resolved; cannot write ~/.copilot/skills/<name>/SKILL.md"))
			continue
		}
		dir := filepath.Join(ctx.HomeDir, ".copilot", "skills", skillSlug(s))
		if content, err := adapter.RenderSkillMarkdown(s); err == nil {
			plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
		}
		for _, r := range s.Resources {
			plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
		}
	}
}

func (Copilot) planCommands(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("commands") {
		return
	}
	for _, c := range b.Commands {
		fm := map[string]any{}
		if c.Description != "" {
			fm["description"] = c.Description
		}
		if mode, ok := c.Frontmatter["mode"].(string); ok && mode != "" {
			fm["mode"] = mode
		}
		if model, ok := c.Frontmatter["model"].(string); ok && model != "" {
			fm["model"] = model
		}
		if tools := c.Frontmatter["tools"]; tools != nil {
			fm["tools"] = tools
		}
		content, err := adapter.RenderFrontmatter(fm, c.Body)
		if err != nil {
			continue
		}
		plan.Files = append(plan.Files, adapter.PlanFile(
			filepath.Join(ctx.ProjectPath, ".github", "prompts", cmdSlug(c)+".prompt.md"), content))
	}
}

func (Copilot) planSubagents(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("subagents") {
		return
	}
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
		content, err := adapter.RenderFrontmatter(fm, sub.SystemPrompt+"\n")
		if err != nil {
			continue
		}
		// NOTE the ".agent.md" extension.
		plan.Files = append(plan.Files, adapter.PlanFile(
			filepath.Join(ctx.ProjectPath, ".github", "agents", ir.Slug(sub.Name)+".agent.md"), content))
	}
}

func (Copilot) planProjectState(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions, plan *ir.WritePlan, from string) {
	if !opts.Wants("project-state") && !opts.Wants("permissions") {
		return
	}
	// Copilot has no hooks model (Capabilities.Hooks == false) and no
	// file-based trust flag: never silently drop these — emit one structured
	// warning each instead (§7).
	if len(b.ProjectState.Hooks) > 0 {
		plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "hooks", ir.ActionManual,
			"copilot has no hooks model; configure manually"))
	}
	if b.ProjectState.Trust != "" {
		plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual,
			"copilot has no file-based trust flag; grant trust on first run"))
	}
	perms := b.ProjectState.Permissions
	if len(perms.Allow) == 0 && len(perms.Deny) == 0 && len(perms.Ask) == 0 {
		return
	}
	if ctx.HomeDir == "" {
		plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "permissions", ir.ActionManual,
			"no HomeDir resolved; cannot write ~/.copilot/permissions-config.json"))
		return
	}
	out := map[string]any{}
	if len(perms.Allow) > 0 {
		out["allow"] = perms.Allow
	}
	if len(perms.Deny) > 0 {
		out["deny"] = perms.Deny
	}
	if len(perms.Ask) > 0 {
		out["ask"] = perms.Ask
	}
	content, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "permissions", ir.ActionManual,
			"could not render permissions-config.json; set permissions manually"))
		return
	}
	content = append(content, '\n')
	plan.Files = append(plan.Files, adapter.PlanFile(
		filepath.Join(ctx.HomeDir, ".copilot", "permissions-config.json"), content))
}

// instrSlug derives a stable file slug for a glob instruction. It prefers the
// source filename (so a copilot round-trip keeps "go.instructions.md"), then
// the IR ID, then a glob-derived fallback.
func instrSlug(in ir.Instruction) string {
	if in.Origin != "" {
		base := filepath.Base(in.Origin)
		base = strings.TrimSuffix(base, ".instructions.md")
		base = strings.TrimSuffix(base, ".md")
		if s := ir.Slug(base); s != "" {
			return s
		}
	}
	if in.ID != "" {
		if s := ir.Slug(in.ID); s != "" {
			return s
		}
	}
	for _, g := range in.Globs {
		if s := ir.Slug(g); s != "" {
			return s
		}
	}
	return "instruction"
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
