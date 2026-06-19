// Package ir defines agensync's canonical intermediate representation: the
// AgentConfigBundle that every tool adapter exports into and imports from.
package ir

import (
	"regexp"
	"strings"
)

const SchemaVersion = "1"

type Scope string

const (
	ScopeProject    Scope = "project"
	ScopeUser       Scope = "user" // user/global scope == personal "memory"
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

// IsMemory reports whether this instruction is personal/global memory
// (user or enterprise scope) rather than project-local config.
func (i Instruction) IsMemory() bool {
	return i.Scope == ScopeUser || i.Scope == ScopeEnterprise
}

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

type MCPAuth struct {
	Type              string
	BearerTokenEnvVar string
	OAuthScopes       []string
}

type SecretsStyle string

const (
	SecretInline      SecretsStyle = "inline"
	SecretEnvIndirect SecretsStyle = "env-indirect"
)

type McpServer struct {
	Common
	Name         string
	Transport    Transport
	Command      string
	Args         []string
	Env          map[string]string
	Cwd          string
	URL          string
	Headers      map[string]string
	Auth         *MCPAuth
	Enabled      bool
	AutoApprove  []string // ["*"] allowed
	ToolInclude  []string
	ToolExclude  []string
	Timeout      int
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
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	Model        string
	Extras       map[string]any
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
	Trust          string // "", "trusted", "untrusted"
	Approvals      map[string]any
	Permissions    Permissions
	Hooks          []Hook
	IgnorePatterns []string
	IgnoreMode     IgnoreMode
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
