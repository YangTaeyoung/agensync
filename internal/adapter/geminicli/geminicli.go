// Package geminicli implements the Gemini CLI adapter (Google). It is a native
// Markdown/JSON tool whose commands use TOML and whose MCP config lives inside a
// larger settings.json. Personal memory is a global GEMINI.md file.
package geminicli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

const id = "gemini-cli"

// Gemini is the Gemini CLI adapter.
type Gemini struct{ adapter.Base }

// New returns the Gemini CLI adapter.
func New() adapter.ToolAdapter { return Gemini{} }

func (Gemini) Meta() adapter.AdapterMeta {
	return adapter.AdapterMeta{ID: id, DisplayName: "Gemini CLI", Vendor: "Google", Confidence: "high"}
}

func (Gemini) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.InstrCaps{Imports: true, ActivationModes: []ir.Activation{ir.ActAlways}, CharBudget: 0},
		MCP: ir.MCPCaps{
			ProjectScope: true,
			Transports:   []ir.Transport{ir.TransportStdio, ir.TransportHTTP, ir.TransportSSE},
			SecretStyle:  ir.SecretInline,
			RemoteURLKey: "httpUrl",
			RootKey:      "mcpServers",
			Format:       "json",
		},
		Skills:      false,
		Commands:    ir.CommandCaps{Supported: true, ArgStyles: []ir.ArgStyle{ir.ArgPositional, ir.ArgNamed, ir.ArgAll}, Format: "toml"},
		Subagents:   "true",
		HomeKeying:  ir.HomeKeyPath,
		Memory:      ir.MemoryFile,
		Permissions: false,
		Hooks:       false,
		Ignore:      "none",
	}
}

func (Gemini) Detect(ctx ir.Context) ir.DetectionResult {
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
		check(filepath.Join(ctx.ProjectPath, "GEMINI.md"), ir.ScopeProject)
		check(filepath.Join(ctx.ProjectPath, ".gemini", "settings.json"), ir.ScopeProject)
	}
	if ctx.HomeDir != "" {
		check(filepath.Join(ctx.HomeDir, ".gemini", "GEMINI.md"), ir.ScopeUser)
		check(filepath.Join(ctx.HomeDir, ".gemini", "settings.json"), ir.ScopeUser)
	}
	return res
}

// ---- Export ----

var reImport = regexp.MustCompile(`@([^\s]+)`)

// resolveImports parses @path inline imports like claude code does, resolving
// each path relative to the instruction file's directory.
func resolveImports(body, dir string) []ir.Import {
	var out []ir.Import
	seen := map[string]bool{}
	for _, m := range reImport.FindAllStringSubmatch(body, -1) {
		target := m[1]
		if seen[target] {
			continue
		}
		seen[target] = true
		imp := ir.Import{Kind: ir.ImpInline, Target: target}
		if content, ok := adapter.ReadIfExists(filepath.Join(dir, target)); ok {
			imp.Resolved = string(content)
		}
		out = append(out, imp)
	}
	return out
}

func (Gemini) buildInstruction(path, body string, scope ir.Scope) ir.Instruction {
	return ir.Instruction{
		Common: ir.Common{
			ID:         ir.Slug(scope2id(scope) + "-gemini-md"),
			Scope:      scope,
			Origin:     path,
			Body:       body,
			Provenance: ir.Provenance{Tool: id, Path: path},
		},
		Activation: ir.ActAlways,
		Imports:    resolveImports(body, filepath.Dir(path)),
	}
}

func scope2id(s ir.Scope) string {
	if s == ir.ScopeUser {
		return "user"
	}
	return "project"
}

func (a Gemini) ExportInstructions(ctx ir.Context) ([]ir.Instruction, error) {
	var out []ir.Instruction
	if ctx.ProjectPath != "" {
		p := filepath.Join(ctx.ProjectPath, "GEMINI.md")
		if b, ok := adapter.ReadIfExists(p); ok {
			out = append(out, a.buildInstruction(p, string(b), ir.ScopeProject))
		}
	}
	if ctx.HomeDir != "" {
		p := filepath.Join(ctx.HomeDir, ".gemini", "GEMINI.md")
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

func (Gemini) ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error) {
	var out []ir.McpServer
	configs := []struct {
		path  string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".gemini", "settings.json"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".gemini", "settings.json"), ir.ScopeUser},
	}
	for _, c := range configs {
		if c.path == "" {
			continue
		}
		b, ok := adapter.ReadIfExists(c.path)
		if !ok {
			continue
		}
		// settings.json has many top-level keys of varying shapes, so decode the
		// outer object loosely and re-wrap just the "mcpServers" value before
		// handing it to the shared parser (url=>SSE, httpUrl=>HTTP inferred).
		var root map[string]json.RawMessage
		if json.Unmarshal(b, &root) != nil {
			continue
		}
		raw, ok := root["mcpServers"]
		if !ok {
			continue
		}
		wrapped := append(append([]byte(`{"mcpServers":`), raw...), '}')
		servers, err := adapter.ParseMCPServersJSON(wrapped, adapter.MCPJSONOptions{RootKey: "mcpServers"})
		if err != nil {
			return nil, err
		}
		out = append(out, tagScope(servers, c.scope, c.path)...)
	}
	return out, nil
}

// ExportSkills: Gemini CLI has no skills concept.
func (Gemini) ExportSkills(ir.Context) ([]ir.Skill, error) { return nil, nil }

func (Gemini) ExportCommands(ctx ir.Context) ([]ir.Command, error) {
	var out []ir.Command
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".gemini", "commands"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".gemini", "commands"), ir.ScopeUser},
	}
	for _, d := range dirs {
		for _, p := range adapter.ListFiles(d.dir, ".toml") {
			b, ok := adapter.ReadIfExists(p)
			if !ok {
				continue
			}
			cmd, err := decodeCommandTOML(b)
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(p), ".toml")
			c := ir.Command{
				Common: ir.Common{
					ID:         ir.Slug(name),
					Scope:      d.scope,
					Origin:     p,
					Body:       cmd.Prompt,
					Provenance: ir.Provenance{Tool: id, Path: p},
				},
				Name:             name,
				Description:      cmd.Description,
				InvocationFormat: "/" + name,
			}
			if strings.Contains(cmd.Prompt, "{{args}}") {
				c.ArgSpec = ir.ArgSpec{Style: ir.ArgAll, Placeholders: []string{"{{args}}"}}
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func (Gemini) ExportSubagents(ctx ir.Context) ([]ir.Subagent, error) {
	var out []ir.Subagent
	dirs := []struct {
		dir   string
		scope ir.Scope
	}{
		{filepath.Join(ctx.ProjectPath, ".gemini", "agents"), ir.ScopeProject},
		{filepath.Join(ctx.HomeDir, ".gemini", "agents"), ir.ScopeUser},
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
			extras := map[string]any{}
			for _, k := range []string{"temperature", "max_turns", "kind"} {
				if v, ok := fm[k]; ok {
					extras[k] = v
				}
			}
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
				Extras:       extras,
			})
		}
	}
	return out, nil
}

func (Gemini) ExportProjectState(ctx ir.Context) (ir.ProjectState, error) {
	var ps ir.ProjectState
	if ctx.HomeDir == "" {
		return ps, nil
	}
	b, ok := adapter.ReadIfExists(filepath.Join(ctx.HomeDir, ".gemini", "trustedFolders.json"))
	if !ok {
		return ps, nil
	}
	abs, _ := filepath.Abs(ctx.ProjectPath)
	if isTrusted(b, abs) {
		ps.Trust = "trusted"
	}
	return ps, nil
}

// isTrusted reports whether abs appears as a trusted folder. The file is a JSON
// object mapping folder path -> trust level (string) or bool; any non-empty
// string other than an explicit untrusted marker, or true, counts as trusted.
func isTrusted(b []byte, abs string) bool {
	var asAny map[string]any
	if json.Unmarshal(b, &asAny) != nil {
		return false
	}
	v, ok := asAny[abs]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToUpper(strings.TrimSpace(t))
		return s != "" && s != "TRUST_NONE" && s != "DO_NOT_TRUST" && s != "UNTRUSTED"
	default:
		return true
	}
}

// ---- PlanImport ----

func (a Gemini) PlanImport(b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	plan := ir.WritePlan{Tool: id}
	from := b.Source.Tool

	// Instructions (project) -> GEMINI.md ; memory (user) -> ~/.gemini/GEMINI.md.
	// Skills have no native home in gemini, so their bodies are appended to the
	// project GEMINI.md as instructions (auto-invoke lost) when instructions are
	// requested. Split by scope: engine exports instructions for either flag.
	var projectBodies, memoryBodies []string
	for _, in := range b.Instructions {
		if in.IsMemory() {
			memoryBodies = append(memoryBodies, in.Body)
		} else {
			projectBodies = append(projectBodies, in.Body)
		}
	}

	// Skills -> instruction fallback (always warn; append body to the project
	// instruction file). Gemini has no skills concept, so the only way to carry
	// a skill across is by inlining its body into GEMINI.md.
	var skillBodies []string
	if opts.Wants("skills") {
		for _, s := range b.Skills {
			plan.Warnings = append(plan.Warnings, engine.UnsupportedSkill(from, id, skillName(s)))
			skillBodies = append(skillBodies, renderSkillBody(s))
		}
	}

	// Write the project GEMINI.md when either real instructions are requested or
	// the skills fallback produced bodies. Without this, skills selected via
	// --only skills would be warned as "emitted as instructions" yet never
	// written (silent drop). The two categories share the single project file.
	wantInstr := opts.Wants("instructions")
	wantSkills := opts.Wants("skills")
	if (wantInstr && len(projectBodies) > 0) || (wantSkills && len(skillBodies) > 0) {
		var bodies []string
		if wantInstr {
			bodies = append(bodies, projectBodies...)
		}
		if wantSkills {
			bodies = append(bodies, skillBodies...)
		}
		if len(bodies) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, "GEMINI.md"),
				[]byte(joinBodies(bodies)),
			))
		}
	}
	if opts.Wants("memory") && ctx.HomeDir != "" {
		if len(memoryBodies) > 0 {
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.HomeDir, ".gemini", "GEMINI.md"),
				[]byte(joinBodies(memoryBodies)),
			))
		}
	}

	// MCP -> .gemini/settings.json (merge into existing top-level keys).
	if opts.Wants("mcp") {
		var servers []ir.McpServer
		servers = append(servers, b.McpServers...)
		if len(servers) > 0 {
			path := filepath.Join(ctx.ProjectPath, ".gemini", "settings.json")
			if content, err := renderSettingsWithMCP(path, servers); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(path, content))
			}
		}
	}

	// Commands -> .gemini/commands/<slug>.toml (md prompt -> TOML).
	if opts.Wants("commands") {
		for _, c := range b.Commands {
			prompt := translateArgs(c)
			content := encodeCommandTOML(c.Description, prompt)
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".gemini", "commands", cmdSlug(c)+".toml"), content))
			plan.Warnings = append(plan.Warnings, engine.Warn("commands", from, id, c.Name, ir.ActionInline, "command markdown converted to gemini TOML"))
		}
	}

	// Subagents -> .gemini/agents/<slug>.md.
	if opts.Wants("subagents") {
		for _, sub := range b.Subagents {
			content, err := renderSubagent(sub)
			if err != nil {
				continue
			}
			plan.Files = append(plan.Files, adapter.PlanFile(
				filepath.Join(ctx.ProjectPath, ".gemini", "agents", ir.Slug(sub.Name)+".md"), content))
		}
	}

	// Project-state: only trust is migratable (no permissions/hooks model).
	if opts.Wants("project-state") {
		if len(b.ProjectState.Permissions.Allow) > 0 || len(b.ProjectState.Permissions.Deny) > 0 || len(b.ProjectState.Permissions.Ask) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "permissions", ir.ActionSkip, "gemini cli has no project permission model"))
		}
		if len(b.ProjectState.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, engine.Skip("project-state", from, id, "hooks", "gemini cli has no hooks"))
		}
		if b.ProjectState.Trust != "" && ctx.HomeDir != "" {
			path := filepath.Join(ctx.HomeDir, ".gemini", "trustedFolders.json")
			abs, _ := filepath.Abs(ctx.ProjectPath)
			if content, err := mergeTrust(path, abs); err == nil {
				plan.Files = append(plan.Files, adapter.PlanFile(path, content))
			}
			plan.Warnings = append(plan.Warnings, engine.Warn("project-state", from, id, "trust", ir.ActionManual, "project added to trustedFolders.json; confirm trust on first run"))
		}
	}

	return plan
}

// renderSettingsWithMCP reads any existing settings.json, sets/replaces its
// "mcpServers" key with the rendered servers (HTTP=>httpUrl, SSE=>url, no type),
// and re-marshals preserving all other top-level keys.
func renderSettingsWithMCP(path string, servers []ir.McpServer) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if existing, ok := adapter.ReadIfExists(path); ok {
		_ = json.Unmarshal(existing, &doc)
	}
	mcpObj, err := buildMCPObject(servers)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(mcpObj)
	if err != nil {
		return nil, err
	}
	doc["mcpServers"] = raw
	return marshalIndentSorted(doc)
}

// buildMCPObject builds the value of the "mcpServers" key: each server object
// uses httpUrl for HTTP, url for SSE/stdio-less, command for stdio, and never a
// "type" field.
func buildMCPObject(servers []ir.McpServer) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, s := range servers {
		js := map[string]any{}
		switch {
		case s.Transport == ir.TransportStdio || s.Command != "":
			js["command"] = s.Command
			if len(s.Args) > 0 {
				js["args"] = s.Args
			}
			if len(s.Env) > 0 {
				js["env"] = s.Env
			}
			if s.Cwd != "" {
				js["cwd"] = s.Cwd
			}
		case s.Transport == ir.TransportHTTP:
			js["httpUrl"] = s.URL
			if len(s.Headers) > 0 {
				js["headers"] = s.Headers
			}
		default: // SSE (and any remote without an explicit transport)
			js["url"] = s.URL
			if len(s.Headers) > 0 {
				js["headers"] = s.Headers
			}
		}
		if !s.Enabled {
			js["disabled"] = true
		}
		if len(s.AutoApprove) > 0 {
			js["autoApprove"] = s.AutoApprove
		}
		if s.Timeout > 0 {
			js["timeout"] = s.Timeout
		}
		out[s.Name] = js
	}
	return out, nil
}

// marshalIndentSorted marshals a map[string]json.RawMessage with sorted top-level
// keys and 2-space indentation for deterministic output. It assembles a compact
// document by hand (to control key order) then runs json.Indent over the whole.
func marshalIndentSorted(doc map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var compact bytes.Buffer
	compact.WriteByte('{')
	for i, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		// Re-compact each value so its internal object keys are stable.
		var v any
		if err := json.Unmarshal(doc[k], &v); err != nil {
			return nil, err
		}
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if i > 0 {
			compact.WriteByte(',')
		}
		compact.Write(kb)
		compact.WriteByte(':')
		compact.Write(vb)
	}
	compact.WriteByte('}')
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), nil
}

// mergeTrust reads an existing trustedFolders.json (object mapping path->level),
// adds abs as TRUST_FOLDER, and re-marshals with sorted keys.
func mergeTrust(path, abs string) ([]byte, error) {
	doc := map[string]string{}
	if existing, ok := adapter.ReadIfExists(path); ok {
		_ = json.Unmarshal(existing, &doc)
	}
	doc[abs] = "TRUST_FOLDER"
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	buf.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(doc[k])
		buf.WriteString("  ")
		buf.Write(kb)
		buf.WriteString(": ")
		buf.Write(vb)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return []byte(buf.String()), nil
}

// translateArgs ensures the prompt references {{args}} when the source command
// takes positional/all-style arguments, replacing $ARGUMENTS / $N placeholders.
func translateArgs(c ir.Command) string {
	prompt := c.Body
	switch c.ArgSpec.Style {
	case ir.ArgAll, ir.ArgPositional, ir.ArgNamed:
		prompt = reArguments.ReplaceAllString(prompt, "{{args}}")
		prompt = rePositional.ReplaceAllString(prompt, "{{args}}")
		if !strings.Contains(prompt, "{{args}}") {
			prompt = strings.TrimRight(prompt, "\n") + "\n\n{{args}}"
		}
	}
	return prompt
}

var (
	reArguments  = regexp.MustCompile(`\$ARGUMENTS\b`)
	rePositional = regexp.MustCompile(`\$[0-9]+`)
)

func renderSubagent(sub ir.Subagent) ([]byte, error) {
	fm := map[string]any{"name": sub.Name}
	if sub.Description != "" {
		fm["description"] = sub.Description
	}
	for _, k := range []string{"temperature", "max_turns", "kind"} {
		if v, ok := sub.Extras[k]; ok {
			fm[k] = v
		}
	}
	if sub.Model != "" {
		fm["model"] = sub.Model
	}
	return adapter.RenderFrontmatter(fm, strings.TrimRight(sub.SystemPrompt, "\n")+"\n")
}

func renderSkillBody(s ir.Skill) string {
	var b strings.Builder
	name := skillName(s)
	if name != "" {
		b.WriteString("## Skill: " + name + "\n\n")
	}
	if s.Description != "" {
		b.WriteString(s.Description + "\n\n")
	}
	b.WriteString(strings.TrimRight(s.Body, "\n"))
	return b.String()
}

func joinBodies(bodies []string) string {
	return strings.Join(bodies, "\n\n")
}

func skillName(s ir.Skill) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

func cmdSlug(c ir.Command) string {
	if c.Name != "" {
		return ir.Slug(c.Name)
	}
	return ir.Slug(c.ID)
}
