// Package claudecode implements the Claude Code adapter — the canonical
// native-Markdown/JSON tool and the reference pattern for export + PlanImport.
package claudecode

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "claude-code"

// CC is the Claude Code adapter.
type CC struct{ adapter.Base }

// New returns the Claude Code adapter.
func New() adapter.ToolAdapter { return CC{} }

func (CC) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Claude Code", Vendor: "Anthropic", Confidence: "high"}
}

func (CC) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: true, ActivationModes: []ir.Activation{ir.ActAlways}},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "url",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      true,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgPositional, ir.ArgAll}, Format: "md"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyPath,
		Memory:      ir.MemoryFile,
		Permissions: true,
		Hooks:       true,
		Ignore:      "none",
	}
}

func (CC) Detect(ctx ir.Context) ir.DetectionResult {
	var res ir.DetectionResult
	check := func(rel string, scope ir.Scope, base string) {
		if base == "" {
			return
		}
		if _, ok := adapter.ReadIfExists(filepath.Join(base, rel)); ok {
			res.Present = true
			res.ScopesFound = append(res.ScopesFound, scope)
			res.Evidence = append(res.Evidence, filepath.Join(base, rel))
		}
	}
	check("CLAUDE.md", ir.ScopeProject, ctx.ProjectPath)
	check(".mcp.json", ir.ScopeProject, ctx.ProjectPath)
	check(filepath.Join(".claude", "settings.json"), ir.ScopeProject, ctx.ProjectPath)
	check(filepath.Join(".claude", "CLAUDE.md"), ir.ScopeUser, ctx.HomeDir)
	check(".claude.json", ir.ScopeUser, ctx.HomeDir)
	return res
}

// ---- Export ----

var reImport = regexp.MustCompile(`@([^\s]+)`)

func resolveImports(body, dir string, depth int) []ir.Import {
	if depth >= 5 {
		return nil
	}
	var out []ir.Import
	seen := map[string]bool{}
	for _, m := range reImport.FindAllStringSubmatch(body, -1) {
		target := m[1]
		if seen[target] {
			continue
		}
		content, ok := adapter.ReadIfExists(filepath.Join(dir, target))
		if !ok {
			continue
		}
		seen[target] = true
		out = append(out, ir.Import{Kind: ir.ImpInline, Target: target, Resolved: string(content)})
	}
	return out
}

func (CC) buildInstruction(path, body string, scope ir.Scope) ir.Instruction {
	return ir.Instruction{
		Common: ir.Common{
			ID:         ir.Slug(scope2id(scope) + "-claude-md"),
			Scope:      scope,
			Origin:     path,
			Body:       body,
			Provenance: ir.Provenance{Tool: id, Path: path},
		},
		Activation: ir.ActAlways,
		Imports:    resolveImports(body, filepath.Dir(path), 0),
	}
}

func scope2id(s ir.Scope) string {
	if s == ir.ScopeUser {
		return "user"
	}
	return "project"
}

func (a CC) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if p := filepath.Join(ctx.ProjectPath, "CLAUDE.md"); ctx.ProjectPath != "" {
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, a.buildInstruction(p, string(b), ir.ScopeProject))
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".claude", "CLAUDE.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, a.buildInstruction(p, string(b), ir.ScopeUser))
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

func (CC) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	if ctx.ProjectPath != "" {
		p := filepath.Join(ctx.ProjectPath, ".mcp.json")
		if b, ok := adapter.ReadIfExists(p); ok {
			servers, err := adapter.ParseMCPServersJSON(b, adapter.MCPJSONOptions{RootKey: "mcpServers"})
			if err != nil {
				return nil, err
			}
			out = append(out, tagScope(servers, ir.ScopeProject, p)...)
		}
	}
	out = append(out, homeMCP(ctx)...)
	return out, nil
}

// homeMCP extracts personal (~/.claude.json top-level) and this project's
// home-stored (projects["<abs>"].mcpServers) servers.
func homeMCP(ctx ir.Context) []ir.McpServer {
	if ctx.HomeDir == "" {
		return nil
	}
	p := filepath.Join(ctx.HomeDir, ".claude.json")
	b, ok := adapter.ReadIfExists(p)
	if !ok {
		return nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	var out []ir.McpServer
	if raw, ok := root["mcpServers"]; ok {
		if servers := parseWrapped(raw); servers != nil {
			out = append(out, tagScope(servers, ir.ScopeUser, p)...)
		}
	}
	if raw, ok := root["projects"]; ok {
		var projects map[string]map[string]json.RawMessage
		if json.Unmarshal(raw, &projects) == nil {
			abs, _ := filepath.Abs(ctx.ProjectPath)
			if pj, ok := projects[abs]; ok {
				if mraw, ok := pj["mcpServers"]; ok {
					if servers := parseWrapped(mraw); servers != nil {
						out = append(out, tagScope(servers, ir.ScopeProject, p)...)
					}
				}
			}
		}
	}
	return out
}

func parseWrapped(raw json.RawMessage) []ir.McpServer {
	wrapped := append(append([]byte(`{"mcpServers":`), raw...), '}')
	servers, err := adapter.ParseMCPServersJSON(wrapped, adapter.MCPJSONOptions{RootKey: "mcpServers"})
	if err != nil {
		return nil
	}
	return servers
}

func (CC) ExportSkills(ctx ir.Context) ([]ir.Skill, error) {
	var out []ir.Skill
	roots := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".claude", "skills"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".claude", "skills"), ir.ScopeUser},
	}
	for _, r := range roots {
		if r.dir == "" {
			continue
		}
		for _, d := range adapter.FindSkillDirs(r.dir) {
			s, err := adapter.ExportSkillDir(d, r.scope, id)
			if err != nil {
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (CC) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".claude", "commands"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".claude", "commands"), ir.ScopeUser},
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

func (CC) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".claude", "agents"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".claude", "agents"), ir.ScopeUser},
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

func (CC) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	for _, name := range []string{"settings.json", "settings.local.json"} {
		p := filepath.Join(ctx.ProjectPath, ".claude", name)
		b, ok := adapter.ReadIfExists(p)
		if !ok {
			continue
		}
		var s struct {
			Permissions struct {
				Allow []string `json:"allow"`
				Deny  []string `json:"deny"`
				Ask   []string `json:"ask"`
			} `json:"permissions"`
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		ps.Permissions.Allow = append(ps.Permissions.Allow, s.Permissions.Allow...)
		ps.Permissions.Deny = append(ps.Permissions.Deny, s.Permissions.Deny...)
		ps.Permissions.Ask = append(ps.Permissions.Ask, s.Permissions.Ask...)
		for event, raw := range s.Hooks {
			ps.Hooks = append(ps.Hooks, ir.Hook{Event: event, Raw: map[string]any{"config": string(raw)}})
		}
	}
	// trust from ~/.claude.json projects[abs].hasTrustDialogAccepted
	if ctx.HomeDir != "" {
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.HomeDir, ".claude.json")); ok {
			var root struct {
				Projects map[string]struct {
					HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
				} `json:"projects"`
			}
			if json.Unmarshal(b, &root) == nil {
				abs, _ := filepath.Abs(ctx.ProjectPath)
				if pj, ok := root.Projects[abs]; ok && pj.HasTrustDialogAccepted {
					ps.Trust = "trusted"
				}
			}
		}
	}
	return ps, nil
}

// ---- PlanImport ----

func (a CC) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Instructions (project) -> CLAUDE.md ; memory (user) -> ~/.claude/CLAUDE.md
	if opts.Wants("instructions") || opts.Wants("memory") {
		var projectBodies []string
		var memoryBodies []string
		for _, in := range b.Instructions {
			if in.IsMemory() {
				memoryBodies = append(memoryBodies, in.Body)
			} else {
				projectBodies = append(projectBodies, in.Body)
			}
		}
		if opts.Wants("instructions") && len(projectBodies) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, "CLAUDE.md"),
				[]byte(joinBodies(projectBodies)),
			))
		}
		if opts.Wants("memory") && len(memoryBodies) > 0 && ctx.HomeDir != "" {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.HomeDir, ".claude", "CLAUDE.md"),
				[]byte(joinBodies(memoryBodies)),
			))
		}
	}

	// MCP: project-scope -> .mcp.json ; user/personal -> ~/.claude.json mcpServers.
	var userServers []ir.McpServer
	if opts.Wants("mcp") {
		var project []ir.McpServer
		for _, s := range b.McpServers {
			if s.Scope == ir.ScopeUser {
				userServers = append(userServers, s)
				continue
			}
			project = append(project, s)
		}
		if len(project) > 0 {
			content, err := adapter.RenderMCPServersJSON(project, adapter.MCPJSONOptions{RootKey: "mcpServers", RemoteURLKey: "url", EmitType: true})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(ctx.ProjectPath, ".mcp.json"), content))
			}
		}
		if len(userServers) > 0 && ctx.HomeDir == "" {
			for _, s := range userServers {
				plan.Warnings = append(plan.Warnings, engine.Skip("mcp", from, id, s.Name, "personal MCP server needs ~/.claude.json but no home dir resolved"))
			}
			userServers = nil
		}
	}

	// Skills -> .claude/skills/<name>/SKILL.md (+ resources)
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			dir := filepath.Join(ctx.ProjectPath, ".claude", "skills", skillSlug(s))
			if content, err := adapter.RenderSkillMarkdown(s); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, "SKILL.md"), content))
			}
			for _, r := range s.Resources {
				plan.Files = append(plan.Files, adapter.PlanFile(filepath.Join(dir, r.RelPath), r.Bytes))
			}
		}
	}

	// Commands -> .claude/commands/<name>.md
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
				filepath.Join(ctx.ProjectPath, ".claude", "commands", cmdSlug(c)+".md"), content))
		}
	}

	// Subagents -> .claude/agents/<name>.md
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
			content, err := adapter.RenderFrontmatter(fm, sub.SystemPrompt+"\n")
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".claude", "agents", ir.Slug(sub.Name)+".md"), content))
		}
	}

	// Project state -> .claude/settings.json (permissions + hooks)
	var trust string
	if opts.Wants("project-state") {
		if content, ok := renderSettings(b.ProjectState); ok {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".claude", "settings.json"), content))
		}
		if b.ProjectState.Trust != "" {
			if ctx.HomeDir == "" {
				plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual, "re-grant trust by accepting the trust dialog on first run"))
			} else {
				trust = b.ProjectState.Trust
				plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionMerge, "trust written to ~/.claude.json; Claude Code may re-confirm on first run"))
			}
		}
	}

	// Home project-scoped layer (§5): user MCP + trust -> ~/.claude.json,
	// merged into the single project key — never overwriting the whole blob.
	if f, ok := planHomeClaudeJSON(ctx, userServers, trust); ok {
		plan.Files = append(plan.Files, f)
	}

	return plan
}

// planHomeClaudeJSON read-modify-writes ~/.claude.json: personal mcpServers
// (top-level, merged by name) and this project's trust flag under
// projects["<abs>"], preserving every other key in the file.
func planHomeClaudeJSON(ctx ir.Context, userServers []ir.McpServer, trust string) (ir.PlannedFile, bool) {
	if ctx.HomeDir == "" || (len(userServers) == 0 && trust == "") {
		return ir.PlannedFile{}, false
	}
	path := filepath.Join(ctx.HomeDir, ".claude.json")
	root := map[string]json.RawMessage{}
	if b, ok := adapter.ReadIfExists(path); ok {
		_ = json.Unmarshal(b, &root)
	}
	if len(userServers) > 0 {
		servers := map[string]json.RawMessage{}
		if raw, ok := root["mcpServers"]; ok {
			_ = json.Unmarshal(raw, &servers)
		}
		rendered, err := adapter.RenderMCPServersJSON(userServers, adapter.MCPJSONOptions{RootKey: "mcpServers", RemoteURLKey: "url", EmitType: true})
		if err == nil {
			var wrap struct {
				McpServers map[string]json.RawMessage `json:"mcpServers"`
			}
			if json.Unmarshal(rendered, &wrap) == nil {
				for name, v := range wrap.McpServers {
					servers[name] = v
				}
			}
		}
		if b, err := json.Marshal(servers); err == nil {
			root["mcpServers"] = b
		}
	}
	if trust != "" {
		abs, _ := filepath.Abs(ctx.ProjectPath)
		projects := map[string]map[string]json.RawMessage{}
		if raw, ok := root["projects"]; ok {
			_ = json.Unmarshal(raw, &projects)
		}
		pj := projects[abs]
		if pj == nil {
			pj = map[string]json.RawMessage{}
		}
		pj["hasTrustDialogAccepted"] = json.RawMessage(boolJSON(trust == "trusted"))
		projects[abs] = pj
		if b, err := json.Marshal(projects); err == nil {
			root["projects"] = b
		}
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return ir.PlannedFile{}, false
	}
	return adapter.PlanFile(path, append(out, '\n')), true
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func joinBodies(bodies []string) string {
	return strings.Join(bodies, "\n\n")
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

func renderSettings(ps ir.ProjectState) ([]byte, bool) {
	out := map[string]any{}
	perms := map[string]any{}
	if len(ps.Permissions.Allow) > 0 {
		perms["allow"] = ps.Permissions.Allow
	}
	if len(ps.Permissions.Deny) > 0 {
		perms["deny"] = ps.Permissions.Deny
	}
	if len(ps.Permissions.Ask) > 0 {
		perms["ask"] = ps.Permissions.Ask
	}
	if len(perms) > 0 {
		out["permissions"] = perms
	}
	if hooks := renderHooks(ps.Hooks); len(hooks) > 0 {
		out["hooks"] = hooks
	}
	if len(out) == 0 {
		return nil, false
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, false
	}
	return append(b, '\n'), true
}

// renderHooks reconstructs the settings.json "hooks" object from exported
// Hook records (each carries its original event config as raw JSON).
func renderHooks(hooks []ir.Hook) map[string]json.RawMessage {
	if len(hooks) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for _, h := range hooks {
		cfg, _ := h.Raw["config"].(string)
		if cfg == "" {
			continue
		}
		out[h.Event] = json.RawMessage(cfg)
	}
	return out
}
