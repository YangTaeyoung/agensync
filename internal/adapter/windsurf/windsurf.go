// Package windsurf implements the Windsurf (Codeium) adapter — a Tier 2,
// fuzzy-path, near-dead-end target. Windsurf stores project rules under a fuzzy
// directory (.windsurf/ or .devin/), keeps MCP config globally only, and has no
// skills or subagents, so importing into it is lossy: skills are degraded to
// rules, subagents are skipped, and project MCP isolation is lost.
package windsurf

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "windsurf"

// charBudget is Windsurf's approximate per-rules-file character cap (~12k).
const charBudget = 12000

// defaultDir is the directory used for writes when no fuzzy dir is detected.
const defaultDir = ".windsurf"

// ignoreFile is Windsurf/Codeium's single project ignore file.
const ignoreFile = ".codeiumignore"

// fuzzyDirs are the candidate project rule/workflow roots, in detection order.
var fuzzyDirs = []string{".windsurf", ".devin"}

// Windsurf is the Windsurf (Codeium) adapter.
type Windsurf struct{ adapter.Base }

// New returns the Windsurf adapter.
func New() adapter.ToolAdapter { return Windsurf{} }

func (Windsurf) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Windsurf", Vendor: "Codeium", Confidence: "medium"}
}

func (Windsurf) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{
			Imports:         false,
			ActivationModes: []ir.Activation{ir.ActAlways, ir.ActGlob, ir.ActModelDecision, ir.ActManual},
			CharBudget:      charBudget,
		},
		MCP: ir.MCPCaps{
			ProjectScope: false,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "serverUrl",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      false,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgAll}, Format: "md"},
		Subagents:   "false",
		HomeKeying:  ir.HomeKeyHash,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       false,
		Ignore:      "both",
	}
}

// ---- fuzzy path helpers ----

// resolveDir picks the fuzzy project dir that already holds rules/workflows,
// defaulting to .windsurf/ for fresh writes.
func resolveDir(projectPath string) string {
	if projectPath == "" {
		return defaultDir
	}
	for _, d := range fuzzyDirs {
		if dirHasContent(filepath.Join(projectPath, d)) {
			return d
		}
	}
	return defaultDir
}

// dirHasContent reports whether a fuzzy dir has any rules/ or workflows/ files.
// ListFiles returns nil for missing dirs, so this also distinguishes absent dirs.
func dirHasContent(path string) bool {
	return len(adapter.ListFiles(filepath.Join(path, "rules"), ".md")) > 0 ||
		len(adapter.ListFiles(filepath.Join(path, "workflows"), ".md")) > 0
}

// memoriesDir is the global personal-memory dir (~/.codeium/windsurf/memories).
func memoriesDir(homeDir string) string {
	return filepath.Join(homeDir, ".codeium", "windsurf", "memories")
}

func globalRulesPath(homeDir string) string {
	return filepath.Join(memoriesDir(homeDir), "global_rules.md")
}

func globalMCPPath(homeDir string) string {
	return filepath.Join(homeDir, ".codeium", "windsurf", "mcp_config.json")
}

// ---- Detect ----

func (Windsurf) Detect(ctx ir.Context) ir.DetectionResult {
	var res ir.DetectionResult
	mark := func(path string, scope ir.Scope) {
		res.Present = true
		res.ScopesFound = append(res.ScopesFound, scope)
		res.Evidence = append(res.Evidence, path)
	}
	if ctx.ProjectPath != "" {
		if b, ok := adapter.ReadIfExists(filepath.Join(ctx.ProjectPath, ".windsurfrules")); ok && len(b) > 0 {
			mark(filepath.Join(ctx.ProjectPath, ".windsurfrules"), ir.ScopeProject)
		}
		for _, d := range fuzzyDirs {
			rulesDir := filepath.Join(ctx.ProjectPath, d, "rules")
			if len(adapter.ListFiles(rulesDir, ".md")) > 0 {
				mark(rulesDir, ir.ScopeProject)
			}
			wfDir := filepath.Join(ctx.ProjectPath, d, "workflows")
			if len(adapter.ListFiles(wfDir, ".md")) > 0 {
				mark(wfDir, ir.ScopeProject)
			}
		}
	}
	if ctx.HomeDir != "" {
		if _, ok := adapter.ReadIfExists(globalRulesPath(ctx.HomeDir)); ok {
			mark(globalRulesPath(ctx.HomeDir), ir.ScopeUser)
		}
		if _, ok := adapter.ReadIfExists(globalMCPPath(ctx.HomeDir)); ok {
			mark(globalMCPPath(ctx.HomeDir), ir.ScopeUser)
		}
	}
	return res
}

// ---- Export ----

// triggerToActivation maps a Windsurf rules frontmatter "trigger" to IR.
func triggerToActivation(trigger string) ir.Activation {
	switch strings.TrimSpace(strings.ToLower(trigger)) {
	case "glob":
		return ir.ActGlob
	case "model_decision":
		return ir.ActModelDecision
	case "manual":
		return ir.ActManual
	default: // "always_on" and anything unknown
		return ir.ActAlways
	}
}

func fmGlobs(fm map[string]any) []string {
	raw, ok := fm["globs"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if t := strings.TrimSpace(v); t != "" {
			return []string{t}
		}
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

func (Windsurf) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		for _, d := range fuzzyDirs {
			rulesDir := filepath.Join(ctx.ProjectPath, d, "rules")
			for _, p := range adapter.ListFiles(rulesDir, ".md") {
				b, ok := adapter.ReadIfExists(p)
				if !ok {
					continue
				}
				fm, body, err := adapter.ParseFrontmatter(b)
				if err != nil {
					continue
				}
				trigger, _ := fm["trigger"].(string)
				name := strings.TrimSuffix(filepath.Base(p), ".md")
				out = append(out, ir.Instruction{
					Common: ir.Common{
						ID:          ir.Slug("project-" + name),
						Scope:       ir.ScopeProject,
						Origin:      p,
						Body:        body,
						Frontmatter: fm,
						Provenance:  ir.Provenance{Tool: id, Path: p},
					},
					Activation: triggerToActivation(trigger),
					Globs:      fmGlobs(fm),
					CharBudget: charBudget,
				})
			}
		}
		// legacy .windsurfrules -> project, always-on
		p := filepath.Join(ctx.ProjectPath, ".windsurfrules")
		if b, ok := adapter.ReadIfExists(p); ok && len(strings.TrimSpace(string(b))) > 0 {
			out = append(out, ir.Instruction{
				Common: ir.Common{
					ID:         "project-windsurfrules",
					Scope:      ir.ScopeProject,
					Origin:     p,
					Body:       string(b),
					Provenance: ir.Provenance{Tool: id, Path: p},
				},
				Activation: ir.ActAlways,
				CharBudget: charBudget,
			})
		}
	}
	// global memory (~/.codeium/windsurf/memories/global_rules.md) -> user scope
	if ctx.HomeDir != "" {
		p := globalRulesPath(ctx.HomeDir)
		if b, ok := adapter.ReadIfExists(p); ok && len(strings.TrimSpace(string(b))) > 0 {
			out = append(out, ir.Instruction{
				Common: ir.Common{
					ID:         "user-global-rules",
					Scope:      ir.ScopeUser,
					Origin:     p,
					Body:       string(b),
					Provenance: ir.Provenance{Tool: id, Path: p},
				},
				Activation: ir.ActAlways,
				CharBudget: charBudget,
			})
		}
	}
	return out, nil
}

func (Windsurf) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	if ctx.HomeDir == "" {
		return nil, nil
	}
	p := globalMCPPath(ctx.HomeDir)
	b, ok := adapter.ReadIfExists(p)
	if !ok {
		return nil, nil
	}
	servers, err := adapter.ParseMCPServersJSON(b, adapter.MCPJSONOptions{RootKey: "mcpServers"})
	if err != nil {
		return nil, err
	}
	for i := range servers {
		servers[i].Scope = ir.ScopeUser // windsurf MCP is global-only
		servers[i].Origin = p
		servers[i].Provenance = ir.Provenance{Tool: id, Path: p}
	}
	return servers, nil
}

// ExportSkills: Windsurf has no skill concept.
func (Windsurf) ExportSkills(ir.Context) ([]ir.Skill, error) { return nil, nil }

func (Windsurf) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	if ctx.ProjectPath == "" {
		return out, nil
	}
	for _, d := range fuzzyDirs {
		wfDir := filepath.Join(ctx.ProjectPath, d, "workflows")
		for _, p := range adapter.ListFiles(wfDir, ".md") {
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
	}
	return out, nil
}

// ExportSubagents: Windsurf has no subagent concept.
func (Windsurf) ExportSubagents(ir.Context) ([]ir.Subagent, error) { return nil, nil }

// ExportProjectState: Windsurf memories are opaque/path-associated beyond the
// global_rules.md file, so there is no migratable project state.
func (Windsurf) ExportProjectState(ir.Context) (ir.ProjectState, error) {
	return ir.ProjectState{}, nil
}

// ---- PlanImport ----

// activationToTrigger maps an IR Activation back to a Windsurf rules trigger.
func activationToTrigger(a ir.Activation) string {
	switch a {
	case ir.ActGlob:
		return "glob"
	case ir.ActModelDecision:
		return "model_decision"
	case ir.ActManual:
		return "manual"
	default:
		return "always_on"
	}
}

func (a Windsurf) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool
	dir := resolveDir(ctx.ProjectPath)

	// Instructions (project) -> <dir>/rules/<slug>.md
	// Memory (user/enterprise) -> ~/.codeium/windsurf/memories/global_rules.md
	if opts.Wants("instructions") || opts.Wants("memory") {
		var memoryBodies []string
		for _, in := range b.Instructions {
			if in.IsMemory() {
				memoryBodies = append(memoryBodies, in.Body)
				continue
			}
			if !opts.Wants("instructions") {
				continue
			}
			plan.Files = append(plan.Files, a.ruleFile(dir, ctx.ProjectPath, ruleSlug(in), in.Activation, in.Globs, in.Body, from, &plan))
		}
		if opts.Wants("memory") && len(memoryBodies) > 0 && ctx.HomeDir != "" {
			plan.Files = append(plan.Files, adapter.PlanFile(
				globalRulesPath(ctx.HomeDir),
				[]byte(strings.Join(memoryBodies, "\n\n")),
			))
		}
	}

	// MCP -> global-only ~/.codeium/windsurf/mcp_config.json
	if opts.Wants("mcp") && len(b.McpServers) > 0 {
		for _, s := range b.McpServers {
			if s.Scope == ir.ScopeProject {
				plan.Warnings = append(plan.Warnings, engine.Warn("mcp", from, id, s.Name, ir.ActionMerge,
					"windsurf MCP is global-only; project isolation lost"))
			}
		}
		if ctx.HomeDir != "" {
			content, err := adapter.RenderMCPServersJSON(b.McpServers, adapter.MCPJSONOptions{
				RootKey:      "mcpServers",
				RemoteURLKey: "serverUrl",
				EmitType:     false,
			})
			if err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(globalMCPPath(ctx.HomeDir), content))
			}
		}
	}

	// Skills: UNSUPPORTED -> warn AND preserve body as a rules file.
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			name := skillName(s)
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSkill(from, id, name))
			body := skillBody(s)
			plan.Files = append(plan.Files, a.ruleFile(dir, ctx.ProjectPath, "skill-"+ir.Slug(name), ir.ActManual, nil, body, from, &plan))
		}
	}

	// Commands -> <dir>/workflows/<slug>.md
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

	// Subagents: UNSUPPORTED -> skip with warning.
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSubagent(from, id, sub.Name))
		}
	}

	// Project state: Windsurf honors a project ignore file (.codeiumignore) but
	// has no project permission/hook model and no scriptable trust grant. Never
	// silently drop any of these — write the ignore file (collapsing block/index
	// modes to the single supported file) and warn on every lossy decision.
	if opts.Wants("project-state") {
		ps := b.ProjectState
		if len(ps.IgnorePatterns) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ignoreFile), renderIgnore(ps.IgnorePatterns)))
			if ps.IgnoreMode == ir.IgnoreIndex {
				plan.Warnings = append(plan.Warnings, engine.Warn("ignore", from, id, ignoreFile, ir.ActionMerge,
					"windsurf has a single .codeiumignore; index-only patterns collapsed to a block ignore"))
			}
		}
		if len(ps.Permissions.Allow) > 0 || len(ps.Permissions.Deny) > 0 || len(ps.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "permissions",
				"windsurf has no project permission model"))
		}
		if len(ps.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "hooks",
				"windsurf has no hooks model"))
		}
		if ps.Trust != "" {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual,
				"windsurf trust is not scriptable; re-grant in the editor"))
		}
	}

	return plan
}

// renderIgnore serializes ignore patterns into a deterministic .codeiumignore body.
func renderIgnore(patterns []string) []byte {
	sorted := append([]string(nil), patterns...)
	sort.Strings(sorted)
	return []byte(strings.Join(sorted, "\n") + "\n")
}

// ruleFile renders a single rule with a trigger frontmatter, enforcing the
// ~12k char cap (truncate + warn).
func (Windsurf) ruleFile(dir, projectPath, slug string, act ir.Activation, globs []string, body, from string, plan *ir.WritePlan) ir.PlannedFile {
	if len(body) > charBudget {
		plan.Warnings = append(plan.Warnings, engine.Warn("instructions", from, id, slug+".md", ir.ActionManual,
			"exceeds windsurf ~12k char cap; truncated"))
		body = body[:charBudget] + "\n\n<!-- agensync: truncated at windsurf ~12k char cap -->\n"
	}
	fm := map[string]any{"trigger": activationToTrigger(act)}
	if act == ir.ActGlob && len(globs) > 0 {
		fm["globs"] = globs
	}
	content, err := adapter.RenderFrontmatter(fm, body)
	if err != nil {
		content = []byte(body)
	}
	return adapter.PlanFile(filepath.Join(projectPath, dir, "rules", slug+".md"), content)
}

func ruleSlug(in ir.Instruction) string {
	if in.ID != "" {
		return ir.Slug(in.ID)
	}
	return ir.Slug(filepath.Base(in.Origin))
}

func cmdSlug(c ir.Command) string {
	if c.Name != "" {
		return ir.Slug(c.Name)
	}
	return ir.Slug(c.ID)
}

func skillName(s ir.Skill) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

// skillBody composes a self-describing rule body from a skill (auto-invoke lost).
func skillBody(s ir.Skill) string {
	var b strings.Builder
	if s.Description != "" {
		b.WriteString("# " + skillName(s) + "\n\n")
		b.WriteString(s.Description + "\n\n")
	}
	b.WriteString(s.Body)
	return b.String()
}
