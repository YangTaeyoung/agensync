# agensync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that clones/migrates a project's AI-coding-agent configuration from one tool (From) to one or more others (To), across project-local files and home-dir project-scoped settings, non-destructively.

**Architecture:** Adapter-per-tool plugins export native config into a canonical IR (`AgentConfigBundle`); a capability-driven mapping/gotcha engine plans target writes with structured loss warnings; a plan/apply layer renders diffs and applies with backups and conflict policy. Interactive TUI (Bubble Tea) wraps the flow; non-interactive flags mirror it.

**Tech Stack:** Go 1.26, cobra (CLI), bubbletea/bubbles/lipgloss (TUI), pelletier/go-toml/v2 (TOML), goccy/go-yaml (frontmatter), stdlib encoding/json.

**Spec:** `docs/specs/2026-06-19-agensync-design.md` — read it before starting.

---

## How to read this plan

- **Phases 1–5** (IR, adapter contract, engine, plan/apply, CLI/TUI) are the foundation, fully specified with TDD steps.
- **Phase 6** implements adapters. **Task 6.1 (Claude Code)** and **Task 6.2 (Codex)** are fully worked as the two reference patterns: a "native-Markdown/JSON" adapter and a "format-transform (JSON→TOML)" adapter. Each remaining adapter (6.3–6.10) is specified as a per-adapter contract table (exact paths/formats/quirks + which gotchas it exercises). Implement each by following the 6.1/6.2 pattern against its contract table, TDD with golden files.
- Every adapter task ends with golden-file tests and a commit.

## File Structure

```
agensync/
├─ go.mod                                   # module github.com/YangTaeyoung/agensync (exists)
├─ cmd/agensync/main.go                     # entrypoint; cobra root + subcommands
├─ internal/
│  ├─ ir/
│  │  ├─ bundle.go                          # AgentConfigBundle + record structs
│  │  ├─ capabilities.go                    # Capabilities struct + enums
│  │  └─ warning.go                         # Warning, WritePlan, results, Context
│  ├─ adapter/
│  │  ├─ adapter.go                         # ToolAdapter interface, AdapterMeta, registry
│  │  ├─ registry_test.go
│  │  ├─ claudecode/claudecode.go (+_test)  # Task 6.1
│  │  ├─ codex/codex.go (+_test)            # Task 6.2
│  │  ├─ kiro/  copilot/  cursor/  geminicli/   # 6.3–6.6
│  │  └─ antigravity/  windsurf/  cline/  aider/ # 6.7–6.10
│  ├─ engine/
│  │  ├─ engine.go (+_test)                 # export→IR→plan orchestration + gotcha decisions
│  │  └─ flatten.go (+_test)                # instruction import flattening
│  ├─ secret/secret.go (+_test)             # inline-secret detection + env-indirection
│  ├─ plan/
│  │  ├─ plan.go (+_test)                    # WritePlan diff/render
│  │  └─ apply.go (+_test)                   # backup + conflict policy + write
│  └─ tui/tui.go                            # Bubble Tea flow
└─ docs/{specs,plans}/
```

Test fixtures live beside each adapter: `internal/adapter/<tool>/testdata/{from,want}/`.

---

## Phase 1: IR Core

### Task 1.1: Define the IR bundle and record types

**Files:**
- Create: `internal/ir/bundle.go`
- Test: `internal/ir/bundle_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ir

import "testing"

func TestBundleSlugStability(t *testing.T) {
	s := Slug("My Cool Skill!")
	if s != "my-cool-skill" {
		t.Fatalf("got %q want my-cool-skill", s)
	}
}

func TestNewBundleDefaults(t *testing.T) {
	b := NewBundle(Source{Tool: "claude-code", ProjectPath: "/p"})
	if b.SchemaVersion == "" {
		t.Fatal("schemaVersion must be set")
	}
	if b.Source.Tool != "claude-code" {
		t.Fatalf("source tool not set: %+v", b.Source)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/ -run TestBundle -v`
Expected: FAIL (undefined: Slug, NewBundle, Source).

- [ ] **Step 3: Write minimal implementation**

```go
package ir

import (
	"regexp"
	"strings"
)

const SchemaVersion = "1"

type Scope string

const (
	ScopeProject    Scope = "project"
	ScopeUser       Scope = "user"
	ScopeEnterprise Scope = "enterprise"
)

type Source struct {
	Tool        string
	Version     string
	ExportedAt  string
	ProjectPath string
}

type Provenance struct {
	Tool string
	Path string
}

// Common is embedded in every record.
type Common struct {
	ID          string
	Scope       Scope
	Origin      string
	Body        string
	Frontmatter map[string]any
	Provenance  Provenance
	LossyFlags  []string
}

type Activation string

const (
	ActAlways        Activation = "always"
	ActGlob          Activation = "glob"
	ActModelDecision Activation = "model-decision"
	ActManual        Activation = "manual"
)

type ImportKind string

const (
	ImpInline    ImportKind = "inline-import" // @path
	ImpFileEmbed ImportKind = "file-embed"    // #[[file:]]
	ImpReference ImportKind = "reference"     // @file ref
)

type Import struct {
	Kind     ImportKind
	Target   string
	Resolved string
}

type Instruction struct {
	Common
	Activation Activation
	Globs      []string
	Imports    []Import
	CharBudget int
}

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

type MCPAuth struct {
	Type            string
	BearerTokenEnvVar string
	OAuthScopes     []string
}

type SecretsStyle string

const (
	SecretInline      SecretsStyle = "inline"
	SecretEnvIndirect SecretsStyle = "env-indirect"
)

type McpServer struct {
	Common
	Name        string
	Transport   Transport
	Command     string
	Args        []string
	Env         map[string]string
	Cwd         string
	URL         string
	Headers     map[string]string
	Auth        *MCPAuth
	Enabled     bool
	AutoApprove []string // ["*"] allowed
	ToolInclude []string
	ToolExclude []string
	Timeout     int
	SecretsStyle SecretsStyle
}

type FileRef struct {
	RelPath    string
	Bytes      []byte
	ContentRef string
}

type Skill struct {
	Common
	Name         string
	Description  string
	Resources    []FileRef
	AllowedTools []string
}

type ArgStyle string

const (
	ArgPositional ArgStyle = "positional"
	ArgNamed      ArgStyle = "named"
	ArgAll        ArgStyle = "all"
)

type ArgSpec struct {
	Style        ArgStyle
	Placeholders []string
}

type Command struct {
	Common
	Name             string
	Namespace        string
	Description      string
	ArgSpec          ArgSpec
	ShellInjections  []string
	FileInjections   []string
	InvocationFormat string
}

type Subagent struct {
	Common
	Name        string
	Description string
	SystemPrompt string
	Tools       []string
	Model       string
	Extras      map[string]any
}

type Hook struct {
	Event   string
	Command string
	Raw     map[string]any
}

type IgnoreMode string

const (
	IgnoreBlock IgnoreMode = "block"
	IgnoreIndex IgnoreMode = "index-only"
)

type Permissions struct {
	Allow []string
	Deny  []string
	Ask   []string
}

type ProjectState struct {
	Trust       string // "", "trusted", "untrusted"
	Approvals   map[string]any
	Permissions Permissions
	Hooks       []Hook
	IgnorePatterns []string
	IgnoreMode  IgnoreMode
}

type RawArtifact struct {
	Category string
	OrigPath string
	Content  []byte
}

type AgentConfigBundle struct {
	SchemaVersion string
	Source        Source
	Instructions  []Instruction
	McpServers    []McpServer
	Skills        []Skill
	Commands      []Command
	Subagents     []Subagent
	ProjectState  ProjectState
	Unmapped      []RawArtifact
}

func NewBundle(s Source) AgentConfigBundle {
	return AgentConfigBundle{SchemaVersion: SchemaVersion, Source: s}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/ -run TestBundle -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ir/bundle.go internal/ir/bundle_test.go
git commit -m "feat(ir): canonical AgentConfigBundle and record types"
```

### Task 1.2: Capabilities, Warning, WritePlan, Context types

**Files:**
- Create: `internal/ir/capabilities.go`, `internal/ir/warning.go`
- Test: `internal/ir/warning_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ir

import "testing"

func TestWarningString(t *testing.T) {
	w := Warning{Category: "subagents", FromTool: "claude-code", ToTool: "aider", Action: ActionSkip, Reason: "no subagents"}
	if w.String() == "" || !contains(w.String(), "aider") {
		t.Fatalf("bad warning string: %q", w.String())
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub { return i }
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/ -run TestWarning -v`
Expected: FAIL (undefined: Warning, ActionSkip).

- [ ] **Step 3: Write minimal implementation**

`internal/ir/warning.go`:
```go
package ir

import "fmt"

type Action string

const (
	ActionSkip      Action = "skip"
	ActionInline    Action = "inline"
	ActionMerge     Action = "merge"
	ActionManual    Action = "manual"
	ActionOverwrite Action = "overwrite"
	ActionSuffix    Action = "suffix"
)

type Warning struct {
	Category string
	FromTool string
	ToTool   string
	Artifact string
	Action   Action
	Reason   string
}

func (w Warning) String() string {
	return fmt.Sprintf("[%s] %s→%s %q: %s (%s)", w.Category, w.FromTool, w.ToTool, w.Artifact, w.Action, w.Reason)
}

type FileMode int

const (
	ModeCreate FileMode = iota
	ModeOverwrite
	ModeMerge
	ModeSkip
)

type PlannedFile struct {
	Path     string
	Content  []byte
	Mode     FileMode
	Existing []byte // nil if file does not exist
}

type WritePlan struct {
	Tool     string
	Files    []PlannedFile
	Warnings []Warning
	Skipped  []string
}

type ApplyResult struct {
	Written  []string
	BackedUp []string
	Skipped  []string
	Errors   []error
}

type DetectionResult struct {
	Present     bool
	ScopesFound []Scope
	Evidence    []string
}

// Context carries resolved environment paths for an adapter run.
type Context struct {
	ProjectPath string
	HomeDir     string
}
```

`internal/ir/capabilities.go`:
```go
package ir

type MCPCaps struct {
	ProjectScope bool
	Transports   []Transport
	SecretStyle  SecretsStyle
	RemoteURLKey string // "url" | "serverUrl" | "httpUrl"
	RootKey      string // "mcpServers" | "servers" | "mcp_servers"
	Format       string // "json" | "toml"
}

type InstrCaps struct {
	Imports         bool
	ActivationModes []Activation
	CharBudget      int // 0 = none
}

type CommandCaps struct {
	Supported bool
	ArgStyles []ArgStyle
	Format    string // "md" | "toml" | "none"
}

type HomeKeying string

const (
	HomeKeyPath HomeKeying = "path"
	HomeKeyHash HomeKeying = "hash"
	HomeKeyNone HomeKeying = "none"
)

type Capabilities struct {
	Instructions InstrCaps
	MCP          MCPCaps
	Skills       bool
	Commands     CommandCaps
	Subagents    string // "true" | "false" | "readonly"
	HomeKeying   HomeKeying
	Permissions  bool
	Hooks        bool
	Ignore       string // "block" | "index" | "both" | "none"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ir/capabilities.go internal/ir/warning.go internal/ir/warning_test.go
git commit -m "feat(ir): capabilities, warning, write-plan, context types"
```

---

## Phase 2: Adapter Contract + Registry

### Task 2.1: ToolAdapter interface and registry

**Files:**
- Create: `internal/adapter/adapter.go`
- Test: `internal/adapter/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package adapter

import (
	"testing"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

type fakeAdapter struct{ id string }

func (f fakeAdapter) Meta() AdapterMeta { return AdapterMeta{ID: f.id, DisplayName: f.id, Confidence: "high"} }
func (f fakeAdapter) Detect(ir.Context) ir.DetectionResult { return ir.DetectionResult{Present: true} }
func (f fakeAdapter) ExportInstructions(ir.Context) ([]ir.Instruction, error) { return nil, nil }
func (f fakeAdapter) ExportMcpServers(ir.Context) ([]ir.McpServer, error)     { return nil, nil }
func (f fakeAdapter) ExportSkills(ir.Context) ([]ir.Skill, error)            { return nil, nil }
func (f fakeAdapter) ExportCommands(ir.Context) ([]ir.Command, error)        { return nil, nil }
func (f fakeAdapter) ExportSubagents(ir.Context) ([]ir.Subagent, error)      { return nil, nil }
func (f fakeAdapter) ExportProjectState(ir.Context) (ir.ProjectState, error) { return ir.ProjectState{}, nil }
func (f fakeAdapter) Capabilities() ir.Capabilities                          { return ir.Capabilities{} }
func (f fakeAdapter) PlanImport(ir.AgentConfigBundle, ir.Context, ImportOptions) ir.WritePlan { return ir.WritePlan{} }
func (f fakeAdapter) Apply(ir.WritePlan, ApplyOptions) ir.ApplyResult        { return ir.ApplyResult{} }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{id: "x"})
	got, ok := r.Get("x")
	if !ok || got.Meta().ID != "x" {
		t.Fatalf("get failed: %v %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("expected missing to be absent")
	}
	if len(r.IDs()) != 1 {
		t.Fatalf("IDs len %d", len(r.IDs()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/ -run TestRegistry -v`
Expected: FAIL (undefined: NewRegistry, AdapterMeta, ImportOptions, ApplyOptions).

- [ ] **Step 3: Write minimal implementation**

```go
package adapter

import (
	"sort"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

type AdapterMeta struct {
	ID          string
	DisplayName string
	Vendor      string
	Confidence  string // "high" | "medium" | "low"
}

type ImportOptions struct {
	Categories map[string]bool // empty = all
	OnConflict ir.Action       // merge|overwrite|suffix|skip
}

func (o ImportOptions) Wants(cat string) bool {
	if len(o.Categories) == 0 {
		return true
	}
	return o.Categories[cat]
}

type ApplyOptions struct {
	DryRun     bool
	Backup     bool
	OnConflict ir.Action
}

type ToolAdapter interface {
	Meta() AdapterMeta
	Detect(ctx ir.Context) ir.DetectionResult
	ExportInstructions(ctx ir.Context) ([]ir.Instruction, error)
	ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error)
	ExportSkills(ctx ir.Context) ([]ir.Skill, error)
	ExportCommands(ctx ir.Context) ([]ir.Command, error)
	ExportSubagents(ctx ir.Context) ([]ir.Subagent, error)
	ExportProjectState(ctx ir.Context) (ir.ProjectState, error)
	Capabilities() ir.Capabilities
	PlanImport(bundle ir.AgentConfigBundle, ctx ir.Context, opts ImportOptions) ir.WritePlan
	Apply(plan ir.WritePlan, opts ApplyOptions) ir.ApplyResult
}

type Registry struct{ m map[string]ToolAdapter }

func NewRegistry() *Registry { return &Registry{m: map[string]ToolAdapter{}} }

func (r *Registry) Register(a ToolAdapter) { r.m[a.Meta().ID] = a }
func (r *Registry) Get(id string) (ToolAdapter, bool) { a, ok := r.m[id]; return a, ok }
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.m))
	for id := range r.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/adapter.go internal/adapter/registry_test.go
git commit -m "feat(adapter): ToolAdapter interface and registry"
```

### Task 2.2: Shared adapter helpers (frontmatter, file walking)

**Files:**
- Create: `internal/adapter/fsutil.go`, `internal/adapter/frontmatter.go`
- Test: `internal/adapter/frontmatter_test.go`

- [ ] **Step 1: Write the failing test**

```go
package adapter

import "testing"

func TestParseFrontmatter(t *testing.T) {
	in := "---\nname: foo\ndescription: bar\n---\nbody here\n"
	fm, body, err := ParseFrontmatter([]byte(in))
	if err != nil { t.Fatal(err) }
	if fm["name"] != "foo" || fm["description"] != "bar" {
		t.Fatalf("fm=%v", fm)
	}
	if body != "body here\n" {
		t.Fatalf("body=%q", body)
	}
}

func TestParseFrontmatterNone(t *testing.T) {
	fm, body, err := ParseFrontmatter([]byte("just body"))
	if err != nil { t.Fatal(err) }
	if len(fm) != 0 || body != "just body" {
		t.Fatalf("fm=%v body=%q", fm, body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/ -run Frontmatter -v`
Expected: FAIL (undefined: ParseFrontmatter).

- [ ] **Step 3: Write minimal implementation**

Add dependency: `go get github.com/goccy/go-yaml@latest`

`internal/adapter/frontmatter.go`:
```go
package adapter

import (
	"bytes"
	"github.com/goccy/go-yaml"
)

// ParseFrontmatter splits a YAML-frontmatter document into (map, body).
// Returns empty map + full input as body when no frontmatter delimiter is present.
func ParseFrontmatter(data []byte) (map[string]any, string, error) {
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return map[string]any{}, string(data), nil
	}
	rest := data[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return map[string]any{}, string(data), nil
	}
	head := rest[:end]
	body := rest[end+len("\n---\n"):]
	fm := map[string]any{}
	if err := yaml.Unmarshal(head, &fm); err != nil {
		return nil, "", err
	}
	return fm, string(body), nil
}

// RenderFrontmatter serializes (map, body) back into a frontmatter document.
func RenderFrontmatter(fm map[string]any, body string) ([]byte, error) {
	if len(fm) == 0 {
		return []byte(body), nil
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(head)
	buf.WriteString("---\n")
	buf.WriteString(body)
	return buf.Bytes(), nil
}
```

`internal/adapter/fsutil.go`:
```go
package adapter

import (
	"os"
	"path/filepath"
)

// ReadIfExists returns (content, true) or (nil, false) when the path is absent.
func ReadIfExists(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// FindSkillDirs returns each subdirectory of root containing a SKILL.md.
func FindSkillDirs(root string) []string {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, ok := ReadIfExists(filepath.Join(root, e.Name(), "SKILL.md")); ok {
				out = append(out, filepath.Join(root, e.Name()))
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/ -v`
Expected: PASS. Then `go mod tidy`.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/frontmatter.go internal/adapter/fsutil.go internal/adapter/frontmatter_test.go go.mod go.sum
git commit -m "feat(adapter): frontmatter parsing and fs helpers"
```

---

## Phase 3: Secret Handling

### Task 3.1: Inline-secret detection and env-indirection

**Files:**
- Create: `internal/secret/secret.go`
- Test: `internal/secret/secret_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import "testing"

func TestLooksLikeSecret(t *testing.T) {
	cases := map[string]bool{
		"sk-ant-abc123def456ghi789jkl": true,
		"ghp_0123456789abcdef0123456789abcdef0123": true,
		"hello":          false,
		"${MY_TOKEN}":    false, // already an env ref
		"$BRAVE_API_KEY": false,
	}
	for in, want := range cases {
		if LooksLikeSecret(in) != want {
			t.Errorf("LooksLikeSecret(%q)=%v want %v", in, LooksLikeSecret(in), want)
		}
	}
}

func TestEnvVarName(t *testing.T) {
	if got := EnvVarName("figma", "FIGMA_OAUTH_TOKEN"); got != "FIGMA_OAUTH_TOKEN" {
		t.Fatalf("got %q", got)
	}
	if got := EnvVarName("context7", ""); got != "CONTEXT7_TOKEN" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -v`
Expected: FAIL (undefined: LooksLikeSecret, EnvVarName).

- [ ] **Step 3: Write minimal implementation**

```go
package secret

import (
	"regexp"
	"strings"
)

var (
	envRef     = regexp.MustCompile(`^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?$`)
	secretLike = regexp.MustCompile(`(?i)(sk-[a-z0-9-]{16,}|gh[pousr]_[A-Za-z0-9]{30,}|[A-Za-z0-9_\-]{32,})`)
)

// LooksLikeSecret reports whether a value is a plausible inline credential
// (and is NOT already an env reference like ${X} or $X).
func LooksLikeSecret(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || envRef.MatchString(v) {
		return false
	}
	return secretLike.MatchString(v)
}

// EnvVarName derives a stable env var name for a server's secret.
func EnvVarName(server, hint string) string {
	if hint != "" {
		return hint
	}
	return strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(server)) + "_TOKEN"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/secret/
git commit -m "feat(secret): inline-secret detection and env-var naming"
```

---

## Phase 4: Engine (export → IR → plan) + Flatten

### Task 4.1: Instruction import flattening

**Files:**
- Create: `internal/engine/flatten.go`
- Test: `internal/engine/flatten_test.go`

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"testing"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestFlattenInlinesImports(t *testing.T) {
	ins := ir.Instruction{
		Common:  ir.Common{Body: "intro\n@sub.md\noutro"},
		Imports: []ir.Import{{Kind: ir.ImpInline, Target: "sub.md", Resolved: "SUB CONTENT"}},
	}
	out := FlattenInstruction(ins)
	want := "intro\nSUB CONTENT\noutro"
	if out.Body != want {
		t.Fatalf("got %q want %q", out.Body, want)
	}
	found := false
	for _, f := range out.LossyFlags {
		if f == "imports-flattened" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected imports-flattened lossy flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run Flatten -v`
Expected: FAIL (undefined: FlattenInstruction).

- [ ] **Step 3: Write minimal implementation**

```go
package engine

import (
	"strings"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// FlattenInstruction replaces each resolved import marker in the body with its
// resolved content, for targets that lack a transclusion mechanism.
func FlattenInstruction(in ir.Instruction) ir.Instruction {
	if len(in.Imports) == 0 {
		return in
	}
	body := in.Body
	for _, imp := range in.Imports {
		if imp.Resolved == "" {
			continue
		}
		marker := "@" + imp.Target
		body = strings.ReplaceAll(body, marker, imp.Resolved)
	}
	in.Body = body
	in.Imports = nil
	in.LossyFlags = append(in.LossyFlags, "imports-flattened")
	return in
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run Flatten -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/flatten.go internal/engine/flatten_test.go
git commit -m "feat(engine): instruction import flattening"
```

### Task 4.2: Engine orchestration (Export + Plan)

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"testing"
	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestExportBuildsBundle(t *testing.T) {
	src := stubAdapter{
		id:    "src",
		instr: []ir.Instruction{{Common: ir.Common{ID: "main", Body: "hi"}}},
		mcp:   []ir.McpServer{{Name: "s1", Transport: ir.TransportStdio, Command: "npx"}},
	}
	b, err := Export(src, ir.Context{ProjectPath: "/p"}, adapter.ImportOptions{})
	if err != nil { t.Fatal(err) }
	if len(b.Instructions) != 1 || len(b.McpServers) != 1 {
		t.Fatalf("bundle: %+v", b)
	}
	if b.Source.Tool != "src" {
		t.Fatalf("source tool %q", b.Source.Tool)
	}
}

func TestPlanFiltersByCategory(t *testing.T) {
	src := stubAdapter{id: "src", mcp: []ir.McpServer{{Name: "s1"}}, instr: []ir.Instruction{{Common: ir.Common{ID: "m"}}}}
	dst := stubAdapter{id: "dst"}
	b, _ := Export(src, ir.Context{}, adapter.ImportOptions{})
	plan := Plan(dst, b, ir.Context{}, adapter.ImportOptions{Categories: map[string]bool{"mcp": true}})
	if plan.Tool != "dst" {
		t.Fatalf("plan tool %q", plan.Tool)
	}
}
```

Add a `stubAdapter` in the test file implementing `adapter.ToolAdapter`, returning its configured slices from the matching Export* methods, empty elsewhere, and recording the bundle it receives in `PlanImport` into `ir.WritePlan{Tool: a.id}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run "Export|Plan" -v`
Expected: FAIL (undefined: Export, Plan).

- [ ] **Step 3: Write minimal implementation**

```go
package engine

import (
	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

// Export reads all requested categories from the source adapter into an IR bundle.
func Export(src adapter.ToolAdapter, ctx ir.Context, opts adapter.ImportOptions) (ir.AgentConfigBundle, error) {
	b := ir.NewBundle(ir.Source{Tool: src.Meta().ID, Version: src.Meta().DisplayName, ProjectPath: ctx.ProjectPath})
	var err error
	if opts.Wants("instructions") {
		if b.Instructions, err = src.ExportInstructions(ctx); err != nil { return b, err }
	}
	if opts.Wants("mcp") {
		if b.McpServers, err = src.ExportMcpServers(ctx); err != nil { return b, err }
	}
	if opts.Wants("skills") {
		if b.Skills, err = src.ExportSkills(ctx); err != nil { return b, err }
	}
	if opts.Wants("commands") {
		if b.Commands, err = src.ExportCommands(ctx); err != nil { return b, err }
	}
	if opts.Wants("subagents") {
		if b.Subagents, err = src.ExportSubagents(ctx); err != nil { return b, err }
	}
	if opts.Wants("project-state") {
		if b.ProjectState, err = src.ExportProjectState(ctx); err != nil { return b, err }
	}
	return b, nil
}

// Plan asks the destination adapter to produce a WritePlan for the bundle.
// The adapter's PlanImport is responsible for capability-driven gotcha warnings.
func Plan(dst adapter.ToolAdapter, b ir.AgentConfigBundle, ctx ir.Context, opts adapter.ImportOptions) ir.WritePlan {
	return dst.PlanImport(b, ctx, opts)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat(engine): export-to-IR and plan orchestration"
```

### Task 4.3: Gotcha helper for adapters

**Files:**
- Create: `internal/engine/gotcha.go`
- Test: `internal/engine/gotcha_test.go`

Provide a helper adapters call inside `PlanImport` to record standard losses. Implement `func Warn(cat, from, to, artifact string, action ir.Action, reason string) ir.Warning` (thin constructor) and `func UnsupportedSubagent(from, to, name string) ir.Warning` returning a skip warning. Test that `UnsupportedSubagent("claude-code","aider","x").Action == ir.ActionSkip`. Commit `feat(engine): standard gotcha warning helpers`.

---

## Phase 5: Plan rendering + Apply

### Task 5.1: Diff rendering

**Files:**
- Create: `internal/plan/plan.go`
- Test: `internal/plan/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plan

import (
	"testing"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestRenderDiffMarksNewAndChanged(t *testing.T) {
	p := ir.WritePlan{Tool: "codex", Files: []ir.PlannedFile{
		{Path: "AGENTS.md", Content: []byte("new"), Existing: nil, Mode: ir.ModeCreate},
		{Path: ".mcp", Content: []byte("b"), Existing: []byte("a"), Mode: ir.ModeOverwrite},
	}}
	out := RenderDiff(p)
	if !contains(out, "AGENTS.md") || !contains(out, "new file") {
		t.Fatalf("missing new-file marker:\n%s", out)
	}
	if !contains(out, "overwrite") {
		t.Fatalf("missing overwrite marker:\n%s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub { return true }
	}
	return false
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/plan/ -run RenderDiff -v` → FAIL (undefined RenderDiff).

- [ ] **Step 3: Implement**

```go
package plan

import (
	"fmt"
	"strings"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func RenderDiff(p ir.WritePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan for %s:\n", p.Tool)
	for _, f := range p.Files {
		switch {
		case f.Existing == nil:
			fmt.Fprintf(&b, "  + %s (new file, %d bytes)\n", f.Path, len(f.Content))
		case f.Mode == ir.ModeMerge:
			fmt.Fprintf(&b, "  ~ %s (merge)\n", f.Path)
		default:
			fmt.Fprintf(&b, "  ! %s (overwrite, was %d bytes)\n", f.Path, len(f.Existing))
		}
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "  warn: %s\n", w.String())
	}
	return b.String()
}
```

- [ ] **Step 4: Run to verify pass.** `go test ./internal/plan/ -run RenderDiff -v` → PASS.
- [ ] **Step 5: Commit.** `git add internal/plan/plan.go internal/plan/plan_test.go && git commit -m "feat(plan): diff rendering"`

### Task 5.2: Apply with backup + conflict policy

**Files:**
- Create: `internal/plan/apply.go`
- Test: `internal/plan/apply_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plan

import (
	"os"
	"path/filepath"
	"testing"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestApplyDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("x"), Mode: ir.ModeCreate}}}
	res := Apply(p, ApplyOptions{DryRun: true})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write")
	}
	if len(res.Written) != 1 { // reported as would-write
		t.Fatalf("expected 1 planned write, got %d", len(res.Written))
	}
}

func TestApplyBacksUpExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(target, []byte("old"), 0o644)
	p := ir.WritePlan{Files: []ir.PlannedFile{{Path: target, Content: []byte("new"), Existing: []byte("old"), Mode: ir.ModeOverwrite}}}
	res := Apply(p, ApplyOptions{Backup: true})
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target=%q", got)
	}
	bak, _ := os.ReadFile(target + ".bak")
	if string(bak) != "old" {
		t.Fatalf("backup=%q", bak)
	}
	if len(res.BackedUp) != 1 {
		t.Fatalf("backedup=%v", res.BackedUp)
	}
}
```

- [ ] **Step 2: Run to verify fail** → FAIL (undefined Apply, ApplyOptions).

- [ ] **Step 3: Implement**

```go
package plan

import (
	"os"
	"path/filepath"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

type ApplyOptions struct {
	DryRun     bool
	Backup     bool
	OnConflict ir.Action
}

func Apply(p ir.WritePlan, opts ApplyOptions) ir.ApplyResult {
	var res ir.ApplyResult
	for _, f := range p.Files {
		if f.Mode == ir.ModeSkip {
			res.Skipped = append(res.Skipped, f.Path)
			continue
		}
		if opts.DryRun {
			res.Written = append(res.Written, f.Path) // would-write
			continue
		}
		if f.Existing != nil && opts.Backup {
			if err := os.WriteFile(f.Path+".bak", f.Existing, 0o644); err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			res.BackedUp = append(res.BackedUp, f.Path+".bak")
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if err := os.WriteFile(f.Path, f.Content, 0o644); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Written = append(res.Written, f.Path)
	}
	return res
}
```

- [ ] **Step 4: Run to verify pass.** `go test ./internal/plan/ -v` → PASS.
- [ ] **Step 5: Commit.** `git add internal/plan/apply.go internal/plan/apply_test.go && git commit -m "feat(plan): apply with dry-run, backup, skip"`

---

## Phase 6: Adapters

Each adapter: a package under `internal/adapter/<tool>/` implementing `adapter.ToolAdapter`, with golden-file tests using `testdata/from/` (a sample native tree) and `testdata/want/` (expected target tree). Adapters that share a format (SKILL.md skills, `mcpServers` JSON) reuse the helpers from Phase 2; format-specific encoders live in the adapter package.

### Task 6.1 (REFERENCE — native MD/JSON): Claude Code adapter

This is the canonical source tool. Implement export fully; PlanImport renders Claude Code's own layout.

**Files:** Create `internal/adapter/claudecode/claudecode.go`, `_test.go`, `testdata/from/{CLAUDE.md,.mcp.json,.claude/skills/demo/SKILL.md,.claude/commands/foo.md,.claude/agents/bar.md}`.

**Capabilities:** instructions `{imports:true, modes:[always], charBudget:0}`; mcp `{projectScope:true, transports:[stdio,http,sse], secretStyle:inline, remoteURLKey:"url", rootKey:"mcpServers", format:"json"}`; skills true; commands `{supported:true, argStyles:[positional,all], format:"md"}`; subagents "true"; homeKeying path; permissions true; hooks true; ignore none.

**Export contract:**
- `ExportInstructions`: read `<project>/CLAUDE.md` (scope project) and `<home>/.claude/CLAUDE.md` (scope user). Parse `@path` lines into `Import{Kind:ImpInline, Target:path}`, resolving each relative to the file dir (read file → `Resolved`); cap recursion at 5. `Activation = always`.
- `ExportMcpServers`: merge three sources — `<project>/.mcp.json` (scope project), `~/.claude.json` top-level `mcpServers` (scope user), and `~/.claude.json` `projects["<abs project>"].mcpServers` (scope project, home-origin). JSON shape: object keyed by name, each `{type, command, args, env, url, headers}`. Map `type` → `Transport`; set `SecretsStyle=inline`.
- `ExportSkills`: for each dir under `<project>/.claude/skills/*/SKILL.md` and `<home>/.claude/skills/*`, parse frontmatter (`name`,`description`), body, and bundle sibling files as `Resources`.
- `ExportCommands`: each `<project>/.claude/commands/*.md` and home equivalent. Frontmatter `description`,`argument-hint`; detect `$ARGUMENTS`/`$1` → `ArgSpec`; detect `` !`...` `` → `ShellInjections`; `@file` → `FileInjections`. `InvocationFormat="/name"`.
- `ExportSubagents`: each `<project>/.claude/agents/*.md` + home. Frontmatter `name`,`description`,`tools`(comma),`model`; body → `SystemPrompt`.
- `ExportProjectState`: from `~/.claude.json` `projects["<abs>"]`: `Trust` from `hasTrustDialogAccepted`; `Permissions` from `<project>/.claude/settings.json` + `settings.local.json` `permissions.{allow,deny,ask}`; `Hooks` from settings `hooks`.

**PlanImport:** write `CLAUDE.md` (instructions, scope project; re-emit imports as `@path` since caps.imports=true), `.mcp.json` (project-scope mcpServers as JSON), `.claude/skills|commands/agents` trees, and `.claude/settings.json` permissions/hooks. Home project-scoped mcp/trust → planned write into `~/.claude.json` `projects["<abs>"]` (merge, never overwrite the whole file — read-modify-write the single project key).

- [ ] **Step 1:** Write failing golden test:

```go
package claudecode

import (
	"os"
	"path/filepath"
	"testing"
	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestExportInstructionsResolvesImports(t *testing.T) {
	ctx := ir.Context{ProjectPath: "testdata/from", HomeDir: t.TempDir()}
	a := New()
	ins, err := a.ExportInstructions(ctx)
	if err != nil { t.Fatal(err) }
	if len(ins) == 0 { t.Fatal("no instructions") }
	if ins[0].Activation != ir.ActAlways {
		t.Fatalf("activation=%s", ins[0].Activation)
	}
}

func TestExportMcpFromDotMcpJson(t *testing.T) {
	ctx := ir.Context{ProjectPath: "testdata/from", HomeDir: t.TempDir()}
	servers, err := New().ExportMcpServers(ctx)
	if err != nil { t.Fatal(err) }
	if len(servers) == 0 { t.Fatal("no mcp servers parsed from .mcp.json") }
}

func writeFixture(t *testing.T, dir, rel, content string) {
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(content), 0o644)
}
```

- [ ] **Step 2:** Add `testdata/from/.mcp.json` = `{"mcpServers":{"ctx7":{"type":"stdio","command":"npx","args":["-y","@upstash/context7-mcp"]}}}` and `testdata/from/CLAUDE.md` with one `@sub.md` line + `testdata/from/sub.md`. Run `go test ./internal/adapter/claudecode/ -v` → FAIL (undefined New).
- [ ] **Step 3:** Implement `New()` returning the adapter and all interface methods per the Export contract above. Use `adapter.ParseFrontmatter`, `adapter.FindSkillDirs`, `encoding/json`.
- [ ] **Step 4:** Run `go test ./internal/adapter/claudecode/ -v` → PASS.
- [ ] **Step 5:** Commit `feat(adapter): claude-code export + plan`.
- [ ] **Step 6:** Add round-trip golden test: export `testdata/from` → IR → `PlanImport` into a temp dir → assert files match `testdata/want`. Commit.

### Task 6.2 (REFERENCE — JSON→TOML transform): Codex adapter

Exemplar for format transformation and env-indirect secrets.

**Files:** Create `internal/adapter/codex/codex.go`, `_test.go`, `internal/adapter/codex/toml.go` (MCP + subagent TOML encode/decode), `testdata/from/{AGENTS.md,.codex/config.toml,.codex/agents/x.toml}`, `testdata/want/`.

**Capabilities:** instructions `{imports:false, modes:[always], charBudget:32768}`; mcp `{projectScope:true, transports:[stdio,http], secretStyle:env-indirect, remoteURLKey:"url", rootKey:"mcp_servers", format:"toml"}`; skills true (`.agents/skills`); commands `{supported:false}` (deprecated → skills); subagents "true" (TOML); homeKeying path (trust only); permissions false; hooks true; ignore none.

**Export contract:**
- `ExportInstructions`: read `<project>/AGENTS.md` (+ `AGENTS.override.md` wins) and `<home>/.codex/AGENTS.md`. No imports. `Activation=always`.
- `ExportMcpServers`: parse `~/.codex/config.toml` + trusted `<project>/.codex/config.toml`, `[mcp_servers.<name>]` tables. stdio: `command/args/env`; http: `url`+`bearer_token_env_var`→`Auth.BearerTokenEnvVar`. `SecretsStyle=env-indirect`.
- `ExportSkills`: `<project>/.agents/skills/*/SKILL.md` and `<home>/.agents/skills/*`.
- `ExportCommands`: none (return nil).
- `ExportSubagents`: `.codex/agents/*.toml` + home — `name/description/developer_instructions(→SystemPrompt)/model`.
- `ExportProjectState`: `~/.codex/config.toml` `[projects."<abs>"].trust_level` → `Trust`.

**PlanImport (the transform):**
- instructions → `AGENTS.md`; if any instruction has `Imports`, call `engine.FlattenInstruction` first and add warning. Enforce 32 KiB budget (warn + truncate-note if exceeded).
- mcp → emit TOML `[mcp_servers.<name>]`. For each server with an inline secret in `Env`/`Headers` (use `secret.LooksLikeSecret`): replace with `bearer_token_env_var`/`env_vars` indirection, emit a `.env` stub planned file, and a warning (`action=manual`, reason="inline secret externalized").
- skills → `.agents/skills/*/SKILL.md` (copy).
- commands present in bundle → **convert to skills** (Command body → a SKILL.md), warning `action=inline reason="codex commands deprecated → skill"`.
- subagents → `.codex/agents/<name>.toml`.
- project-state trust → planned merge into `~/.codex/config.toml` `[projects."<abs>"]`; post-apply guidance: "run codex in this dir and grant trust."

- [ ] **Step 1:** Failing test: `TestMcpJSONToTOML` — feed an IR `McpServer{stdio}` through the TOML encoder, assert output contains `[mcp_servers.ctx7]` and `command = "npx"`.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3:** Implement `toml.go` using `github.com/pelletier/go-toml/v2` (`go get`), plus adapter methods.
- [ ] **Step 4:** Failing test `TestPlanInlineSecretExternalized`: IR server with `Headers{"Authorization":"Bearer sk-ant-...."}` → plan has a `.env` file + a `manual` warning, and the TOML contains no `sk-ant`. Implement. Run → PASS.
- [ ] **Step 5:** Golden round-trip test against `testdata/want`. Commit `feat(adapter): codex with JSON→TOML and secret env-indirection`.

### Tasks 6.3–6.10: Remaining adapters (follow 6.1/6.2 pattern)

For each, create `internal/adapter/<id>/<id>.go` + `_test.go` + `testdata/`, implement per the contract row, write golden tests, commit `feat(adapter): <id>`. Use the exact paths/quirks below (from the spec capability matrix). **Every unsupported category MUST emit a structured warning in `PlanImport` (never silent-drop).**

- [ ] **6.3 Kiro** — id `kiro`, confidence high. Instructions: `.kiro/steering/*.md` (+`AGENTS.md`), frontmatter `inclusion` → `Activation` (`always|fileMatch→glob+globs(fileMatchPattern)|manual|auto→model-decision`); `#[[file:path]]` → `Import{ImpFileEmbed}`. MCP: `.kiro/settings/mcp.json` + `~/.kiro/settings/mcp.json`, root `mcpServers`, `${VAR}` env, `autoApprove`, `disabled`→`Enabled`. Skills: `.kiro/skills/<n>/SKILL.md`. Commands: **none-as-files** → on import, convert IR commands to `inclusion: manual` steering files (warn). Subagents: `.kiro/agents/*.md` (frontmatter `name/tools/model/includeMcpJson`). HomeKeying none. Gotcha exercised: commands→manual-steering, global hooks unreliable (skip home hooks + warn).

- [ ] **6.4 GitHub Copilot** — id `copilot`, confidence high. Model as CLI surface (primary). Instructions: read/write `.github/copilot-instructions.md` + `AGENTS.md` + `.github/instructions/*.instructions.md` (`applyTo:` glob → `Activation=glob`,`Globs`). MCP: CLI `.mcp.json`/`.github/mcp.json` + `~/.copilot/mcp-config.json`, root `mcpServers`, `type: local|stdio|http|sse`. Skills: `~/.copilot/skills/<n>/SKILL.md`. Commands: IDE `.github/prompts/*.prompt.md` (frontmatter `mode/model/tools`). Subagents: `.github/agents/*.agent.md` / `~/.copilot/agents/*.agent.md` (NOTE `.agent.md` ext). Home project-scoped: `~/.copilot/permissions-config.json` (by project location → ProjectState.Permissions). Gotcha: VS Code MCP root key is `servers` not `mcpServers` (if writing `.vscode/mcp.json`, remap) — warn; cloud surface out of scope (warn).

- [ ] **6.5 Cursor** — id `cursor`, confidence high. Instructions: `AGENTS.md` + `.cursor/rules/*.mdc` (frontmatter `globs`/`alwaysApply` → Activation) + legacy `.cursorrules`. MCP: `.cursor/mcp.json` + `~/.cursor/mcp.json`, root `mcpServers`. Skills: `.cursor/skills/`,`.agents/skills/`. Commands: `.cursor/commands/*.md`. Subagents: `.cursor/agents/*.md` (frontmatter `name/model/readonly/is_background` → Extras). HomeKeying hash (opaque `workspaceStorage`) → home project-scoped **not migratable**; ProjectState from repo files only. Gotcha: User Rules live in UI (no file) → emit manual paste-in instructions on import (warn).

- [ ] **6.6 Gemini CLI** — id `gemini-cli`, confidence high. Instructions: `GEMINI.md` (+ `@imports`, hierarchical) + `~/.gemini/GEMINI.md`. MCP: `.gemini/settings.json` + `~/.gemini/settings.json`, key `mcpServers`, **no `type`** — distinguish `url`(SSE) vs `httpUrl`(HTTP) vs command(stdio); remoteURLKey `httpUrl`. Skills: **none** → on import, skills become instructions (warn). Commands: `.gemini/commands/*.toml` **TOML** (`prompt`,`description`,`{{args}}`) — encode IR `ArgSpec` placeholders to `{{args}}`; `!{}`/`@{}` injections. Subagents: `.gemini/agents/*.md` (frontmatter `temperature/max_turns/kind`, inline `mcpServers`). Home project-scoped: `~/.gemini/trustedFolders.json` (trust). Gotcha: command MD→TOML transform; skills→instructions fallback.

- [ ] **6.7 Antigravity** — id `antigravity`, confidence medium. Use fuzzy path matching: accept both `.agents/` and `.agent/`. Instructions: `AGENTS.md`/`GEMINI.md`/`.agents/rules/*`; home `~/.gemini/AGENTS.md`. MCP: `.agents/mcp_config.json` + `~/.gemini/config/mcp_config.json`, root `mcpServers`, remoteURLKey **`serverUrl`** (remap from IR `url`), **strip JSON comments, drop `timeout`**. Skills: `.agents/skills/<n>/SKILL.md`. Commands ("Workflows"): `.agents/workflows/*.md` + `~/.gemini/antigravity/global_workflows/*.md`. Subagents: **none clean** → skip+warn. Home project-scoped: `~/.gemini/antigravity-cli/settings.json` `trustedWorkspaces`. Gotcha: `serverUrl` remap, no-comments JSON, subagents skip, GEMINI.md/Gemini-CLI conflict (prefer AGENTS.md, warn).

- [ ] **6.8 Windsurf** — id `windsurf`, confidence medium. Fuzzy: `.windsurf/` or `.devin/`. Instructions: `.windsurf/rules/*.md` (frontmatter `trigger:` → Activation) + legacy `.windsurfrules` + home `~/.codeium/windsurf/memories/global_rules.md`; enforce char caps (~12k/~6k) with warn. MCP: **global-only** `~/.codeium/windsurf/mcp_config.json` (root `mcpServers`, `serverUrl`) → project-scope import merges to global (warn isolation lost). Skills: **none** → instructions fallback (warn). Commands: `.windsurf/workflows/*.md`. Subagents: **none** → skip+warn. HomeKeying hash (memories opaque). Near-dead-end target — set expectations.

- [ ] **6.9 Cline** — id `cline`, confidence medium. Instructions: `.clinerules/*.md` (dir, all merged) + `AGENTS.md` + `~/Documents/Cline/Rules/`. MCP: **global-only** `cline_mcp_settings.json` (VS Code globalStorage path per-OS; CLI `~/.cline/...`), root `mcpServers`, `disabled`/`autoApprove`/`timeout` → merge to global (warn). Skills: `.cline/skills/`,`.clinerules/skills/`,`.claude/skills/`; home `~/.cline/skills/`. Commands ("Workflows"): `.clinerules/workflows/*.md`, invocation `/name.md` (set `InvocationFormat`). Subagents: **none** (built-in read-only) → skip+warn. HomeKeying none (profile-global). Gotcha: global-only MCP, invocation-format `.md` suffix.

- [ ] **6.10 Aider** — id `aider`, confidence high, **instructions-only target**. Instructions: `CONVENTIONS.md` (not auto-loaded) — on import, also plan a `.aider.conf.yml` with `read: [CONVENTIONS.md]` wiring (merge if exists). MCP/skills/commands/subagents: **all unsupported** → each emits a skip warning on import. Export: read `CONVENTIONS.md` if referenced by `.aider.conf.yml` `read:`. Ignore: `.aiderignore`. Gotcha: heavy loss report; UX must warn this is instructions-mostly.

After 6.10: register all adapters in a constructor. Create `internal/adapter/all/all.go` with `func Default() *adapter.Registry` registering every adapter; test that `Default().IDs()` has 10 entries. Commit `feat(adapter): default registry with all 10 adapters`.

---

## Phase 7: CLI + TUI

### Task 7.1: cobra root, `detect`, and non-interactive `migrate`

**Files:** Create `cmd/agensync/main.go`, `internal/cli/cli.go` (+`_test.go`).

- [ ] **Step 1:** Failing test `TestParseCategories` — `ParseCategories("mcp,instructions")` → `map[string]bool{"mcp":true,"instructions":true}`; invalid category → error.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3:** Implement `ParseCategories`, `ParseTools` (validate against registry IDs), and a `Run(args)` that wires cobra commands:
  - `agensync detect` → for each registry adapter, run `Detect(ctx)`, print present ones.
  - `agensync migrate --from <id> --to <ids> [--only/--skip] [--dry-run|--apply|--yes] [--on-conflict] [--no-backup] [--home] [--project] [--report]` → `engine.Export(src)` → for each target `engine.Plan` → `plan.RenderDiff` → if `--apply`/`--yes` then `target.Apply`; write report.
  - bare `agensync` with no subcommand → launch TUI (Task 7.2).
- [ ] **Step 4:** Run → PASS. Add an integration test: temp project with a `CLAUDE.md` + `.mcp.json`, run `migrate --from claude-code --to codex --dry-run`, assert stdout shows a planned `AGENTS.md` and `[mcp_servers` TOML and writes nothing.
- [ ] **Step 5:** Commit `feat(cli): detect and migrate commands`.

### Task 7.2: Bubble Tea interactive flow

**Files:** Create `internal/tui/tui.go`.

- [ ] **Step 1:** `go get github.com/charmbracelet/bubbletea github.com/charmbracelet/bubbles github.com/charmbracelet/lipgloss`.
- [ ] **Step 2:** Implement a model with states: `selectFrom` (list of detected+all adapters) → `selectTo` (multi-select) → `selectCategories` (multi-select, default all) → `preview` (scrollable RenderDiff + warnings) → `resolveConflicts` (per-file skip/overwrite/merge/suffix) → `confirmApply`. On confirm, call engine/plan/apply. Keep business logic in engine/plan; TUI only orchestrates + collects choices into `ImportOptions`/`ApplyOptions`.
- [ ] **Step 3:** Manual smoke test: `go run ./cmd/agensync` in a temp project; verify the flow renders and a dry-run preview shows. (TUI logic is thin; unit-test the non-UI helpers it calls.)
- [ ] **Step 4:** Commit `feat(tui): interactive from/to/category/preview/apply flow`.

### Task 7.3: README + end-to-end smoke

**Files:** Create `README.md`.

- [ ] **Step 1:** Write README: install (`go install github.com/YangTaeyoung/agensync/cmd/agensync@latest`), usage (interactive + flags), supported tools + confidence tiers, safety notes (dry-run/backup/secrets/trust).
- [ ] **Step 2:** Add a top-level `TestEndToEndClaudeToCodexAndCursor` in `internal/cli/` building a temp project and asserting both targets' files are produced on `--apply` with backups.
- [ ] **Step 3:** Run full suite `go test ./...` → PASS; `go vet ./...` clean.
- [ ] **Step 4:** Commit `docs: README and end-to-end test`.

---

## Self-Review Notes (coverage check vs spec)

- Spec §3 IR → Phase 1. §3.2 adapter interface → Phase 2. §3.3 gotcha engine → Phase 4 + per-adapter `PlanImport`. §4 CLI/UX → Phase 7. §5 two-layer (home project-scoped) → covered in 6.1 (`~/.claude.json` projects key) and 6.2/6.6/6.7 (trust files). §6 matrix → Phase 6 tasks 6.1–6.10. §7 gotchas → each adapter task names the gotcha it exercises; flatten in 4.1, secret env-indirection in 3.1+6.2. §8 safety → Phase 5 (dry-run/backup/conflict) + secret module + trust guidance. §9 structure → File Structure section. §10 risks → confidence flags in `Meta()` + fuzzy paths in 6.7/6.8/6.9.
- No placeholders: every foundation task has full code; adapters reference exact paths/formats and the two fully-worked reference patterns (6.1/6.2). Type names (`AgentConfigBundle`, `ToolAdapter`, `WritePlan`, `ImportOptions`, `ApplyOptions`, `Capabilities`, `FlattenInstruction`, `Apply`, `RenderDiff`) are consistent across phases.
- Known intentional simplification: TUI is smoke-tested, not unit-tested (logic pushed into testable engine/plan packages).
