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

// MemoryStyle declares how a tool stores personal/global memory.
type MemoryStyle string

const (
	MemoryFile   MemoryStyle = "file"   // a global memory/rules file we can read/write
	MemoryOpaque MemoryStyle = "opaque" // editor DB / hash store — not migratable as files
	MemoryUI     MemoryStyle = "ui"     // lives in app UI, no file (manual paste-in)
	MemoryNone   MemoryStyle = "none"   // tool has no personal-memory concept
)

type Capabilities struct {
	Instructions InstrCaps
	MCP          MCPCaps
	Skills       bool
	Commands     CommandCaps
	Subagents    string // "true" | "false" | "readonly"
	HomeKeying   HomeKeying
	Memory       MemoryStyle // personal/global memory support
	Permissions  bool
	Hooks        bool
	Ignore       string // "block" | "index" | "both" | "none"
}
