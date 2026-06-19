# agensync — Design Spec

- **Status:** Approved (design), pending implementation
- **Date:** 2026-06-19
- **Module:** `github.com/YangTaeyoung/agensync`
- **Language:** Go 1.26+, distributed via `go install`

## 1. Purpose

A CLI that **clones/migrates a project's AI-coding-agent configuration from one tool to one or more other tools**, so a developer who set up conventions in (e.g.) Claude Code can carry the same experience into Codex, Antigravity, Kiro, Cursor, etc.

The user runs it **from a project root**, picks a **From** service and one or more **To** services interactively, and agensync replicates the equivalent configuration across two layers:

1. **Project-local files** — `CLAUDE.md`/`AGENTS.md`, MCP config, skills, commands, subagents, ignore/permission files.
2. **Home-directory project-scoped settings** — the per-project entries a tool stores in a HOME file keyed by the project's absolute path (canonical example: `~/.claude.json` `projects["<abs-path>"]`). Only the migratable intent (MCP servers, permissions, trust) is selectively extracted — never the monolithic blob.

Migration is **non-destructive by default**: dry-run preview, conflict resolution, backups, and a structured loss/transformation report.

## 2. Non-Goals (v1)

- Not a continuous two-way sync daemon. It's an explicit, on-demand clone/migrate operation.
- Does not migrate conversation history, telemetry, OAuth credentials, or opaque editor state (Cursor `workspaceStorage`, Gemini `tmp/<hash>`, Windsurf memories).
- Does not write plaintext secrets into target configs (see §8).
- Does not configure cloud-side surfaces that are not file-based (e.g. GitHub Copilot coding-agent MCP, stored server-side).

## 3. Core Architecture — Adapter + Canonical IR

To avoid N×N pairwise converters, every tool maps to/from a shared **canonical intermediate representation (IR)**, `AgentConfigBundle`.

```
[From tool files] --adapter.export--> IR --mapping/gotcha engine--> WritePlan --apply--> [To tool files]
                                        ^                                  ^
                              source+target capabilities()         dry-run / conflict / backup
```

### 3.1 IR — `AgentConfigBundle`

```
AgentConfigBundle
├─ schemaVersion: string
├─ source: { tool, version, exportedAt, projectPath }
├─ instructions: Instruction[]
├─ mcpServers:   McpServer[]
├─ skills:       Skill[]
├─ commands:     Command[]
├─ subagents:    Subagent[]
├─ projectState: ProjectState
└─ unmapped:     RawArtifact[]   # detected but untyped; preserved verbatim
```

**Common record fields:** `id` (stable slug), `scope` (`project|user|enterprise`), `origin` (source abs path), `body`, `frontmatter` (parsed map), `provenance` (source tool + path), `lossyFlags` (fields that could not be represented).

- **Instruction**: `body`, `activation` (`always|glob|model-decision|manual` — normalizes Kiro `inclusion`, Cursor rule types, Windsurf `trigger`, Copilot `applyTo`), `globs[]`, `imports[]` (`{kind: inline-import|file-embed|reference, target, resolved?}` for `@path`, `#[[file:]]`, `@file`), `charBudget?`.
- **McpServer**: `name`, `transport` (`stdio|http|sse`); stdio: `command/args/env/cwd`; remote: `url/headers/auth{type,bearerTokenEnvVar?,oauthScopes?}`; `enabled`, `autoApprove[]|"*"`, `toolFilter{include,exclude}`, `timeout?`, `secretsStyle` (`inline|env-indirect`).
- **Skill**: `name`, `description`, `body`, `resources[]` (scripts/references/assets as FileRef), `allowedTools?`.
- **Command**: `name`, `namespace?`, `description?`, `body`, `argSpec{style: positional|named|all, placeholders[]}`, `shellInjections[]`, `fileInjections[]`, `invocationFormat`.
- **Subagent**: `name`, `description`, `systemPrompt`, `tools[]`, `model?`, `extras{}` (temperature/max_turns/readonly/includeMcpJson — preserved, flagged lossy if unsupported).
- **ProjectState**: `trust`, `approvals{}`, `permissions{allow,deny,ask}`, `hooks[]`, `ignore{patterns[], mode: block|index-only}`.
- **FileRef** `{relPath, bytes|contentRef}`, **RawArtifact** `{category, origPath, content}`.

### 3.2 Adapter interface (one per tool)

```go
type ToolAdapter interface {
    Meta() AdapterMeta // id, displayName, vendor, confidence

    Detect(ctx Context) DetectionResult // {present, scopesFound, evidence[]} — cheap, no parse

    // export: native files -> IR (unsupported categories return empty + capability flag, not error)
    ExportInstructions(ctx Context) ([]Instruction, error)
    ExportMcpServers(ctx Context)   ([]McpServer, error)
    ExportSkills(ctx Context)       ([]Skill, error)
    ExportCommands(ctx Context)     ([]Command, error)
    ExportSubagents(ctx Context)    ([]Subagent, error)
    ExportProjectState(ctx Context) (ProjectState, error)

    Capabilities() Capabilities // single source of truth, drives gotcha engine

    // IR -> tool: pure, returns planned writes + warnings; NO side effects
    PlanImport(bundle AgentConfigBundle, ctx Context, opts ImportOptions) WritePlan

    // the only side-effecting op; honors dryRun / backup / onConflict
    Apply(plan WritePlan, opts ApplyOptions) ApplyResult
}
```

`Context` = `{ ProjectPath, HomeDir }`. `WritePlan` = `{ files: [{path, content, mode}], warnings: Warning[], skipped[] }`.

`Capabilities` declares: instructions `{imports, activationModes[], charBudget?}`, mcp `{projectScope bool, transports[], secretStyle, remoteUrlKey}`, `skills bool`, commands `{argStyles[]}|false`, `subagents bool|"readonly"`, `homeStateKeying path|hash|none`, `permissions bool`, `hooks bool`, `ignore block|index|both|none`.

### 3.3 Mapping / gotcha engine

Consumes **both** adapters' `Capabilities()`. For every IR record the target can't represent, it decides an action and emits a structured `Warning{category, fromTool, toTool, artifact, action: skip|inline|merge|manual, reason}`. **Never silently drop.** Default fallbacks in §7.

## 4. CLI UX

Interactive (Bubble Tea), run from project root:

```
$ agensync
1) Select From service   (auto-detected tools listed first)
2) Select To service(s)  (multi-select)
3) Select categories      (instructions/mcp/skills/commands/subagents/project-state; default = all)
4) Plan preview (diff)    + loss/transformation warning report
5) Per-conflict decision  (skip / overwrite / merge / suffix)
6) Apply (+ auto .bak backups) + post-migration "grant trust" guidance per target
```

Non-interactive:

```
agensync --from claude-code --to codex,cursor --only mcp,instructions --yes
agensync --from claude-code --to kiro --dry-run            # plan only, no writes
agensync detect                                            # list detected tools in cwd/home
```

Flags: `--from`, `--to` (comma list), `--only`/`--skip` (categories), `--dry-run` (default true unless `--yes`/`--apply`), `--yes`/`--apply`, `--on-conflict merge|overwrite|suffix|skip`, `--no-backup`, `--home <dir>`, `--project <dir>`, `--report <path>`.

## 5. Two-Layer Migration

- **Project-local:** instructions (`CLAUDE.md`↔`AGENTS.md`↔`.kiro/steering`↔`.clinerules/`…), MCP (`.mcp.json`↔`.codex/config.toml`↔`.agents/mcp_config.json`…), `.../skills/*/SKILL.md`, `.../commands|workflows/*`, `.../agents/*`, ignore/permission files.
- **Home project-scoped:** read source HOME per-project record (e.g. `~/.claude.json` `projects["<abs>"]`), extract only `mcpServers` + `permissions` + trust intent, write to each target's equivalent (Codex `~/.codex/config.toml [projects."<abs>"]`, Gemini `~/.gemini/trustedFolders.json`, Copilot `~/.copilot/permissions-config.json`, etc.). Telemetry/OAuth/global state excluded.

## 6. Capability Matrix (v1 targets)

Tiers reflect format stability/confidence; an adapter = a tool, so coverage grows by adding adapters.

- **Tier 1 (high confidence, stable):** Claude Code, Codex CLI, Kiro, GitHub Copilot, Cursor, Gemini CLI
- **Tier 2 (medium, version-churn — fuzzy path matching + fallbacks):** Antigravity, Windsurf/Devin, Cline
- **Tier 3 (limited target — instructions only):** Aider

| Tool | Instructions | MCP | Skills | Commands | Subagents | Home project-scoped |
|---|---|---|---|---|---|---|
| Claude Code | `CLAUDE.md` (+`@imports`, dir-merge); `~/.claude/CLAUDE.md` | `.mcp.json` + `~/.claude.json` JSON `mcpServers` (stdio/http/sse) | `.claude/skills/<n>/SKILL.md` | `.claude/commands/<n>.md` (`$ARGUMENTS`/`$1`, `!\``, `@`) | `.claude/agents/<n>.md` (MD+YAML) | `~/.claude.json` `projects[abs]` (mcp/trust/perms) **canonical** |
| Codex CLI | `AGENTS.md` (+`AGENTS.override.md`, concat, no imports, 32KiB); `~/.codex/AGENTS.md` | `~/.codex/config.toml` `[mcp_servers.<n>]` **TOML**, env-indirect | `.agents/skills/<n>/SKILL.md` (note `.agents`) | deprecated → Skills; `~/.codex/prompts/*.md` | `.codex/agents/<n>.toml` **TOML** | `~/.codex/config.toml [projects."abs"].trust_level` (trust only) |
| Antigravity | `AGENTS.md`/`GEMINI.md`/`.agents/rules/*`; `~/.gemini/*` | `.agents/mcp_config.json` + `~/.gemini/config/mcp_config.json` (`serverUrl`, no comments, no timeout) | `.agents/skills/<n>/SKILL.md` | `.agents/workflows/<n>.md` ("Workflows") | plugin `agents/` (sparse) | `~/.gemini/antigravity-cli/settings.json` `trustedWorkspaces` |
| Kiro | `.kiro/steering/*.md` (YAML `inclusion`), `AGENTS.md`; `~/.kiro/steering` | `.kiro/settings/mcp.json` + `~/.kiro/settings/mcp.json` (`${VAR}`, `autoApprove`) | `.kiro/skills/<n>/SKILL.md` | none-as-files (manual steering/hook/subagent) | `.kiro/agents/<n>.md` (MD+YAML) | none (all in repo `.kiro/`) |
| Cursor | `AGENTS.md`; `.cursor/rules/*.mdc`; `.cursorrules` | `.cursor/mcp.json` + `~/.cursor/mcp.json` | `.cursor/skills/`, `.agents/skills/` | `.cursor/commands/<n>.md` | `.cursor/agents/<n>.md` | VS Code `state.vscdb`/`workspaceStorage/<hash>` (opaque) |
| Gemini CLI | `GEMINI.md` (+`@imports`, hierarchical); `~/.gemini/GEMINI.md` | `.gemini/settings.json` + `~/.gemini/settings.json` (`url`/`httpUrl`, no `type`) | none (Extensions) | `.gemini/commands/<n>.toml` **TOML** (`{{args}}`) | `.gemini/agents/<n>.md` | `~/.gemini/trustedFolders.json` (trust) |
| GitHub Copilot | `.github/copilot-instructions.md`, `AGENTS.md`, `.github/instructions/*.instructions.md` (`applyTo`); reads CLAUDE.md/GEMINI.md | CLI: `.mcp.json`/`.github/mcp.json` + `~/.copilot/mcp-config.json` (`mcpServers`); VS Code: `.vscode/mcp.json` (`servers`) | `~/.copilot/skills/<n>/SKILL.md` | IDE `.github/prompts/*.prompt.md`; CLI none | `.github/agents/<n>.agent.md`, `~/.copilot/agents/*.agent.md` | `~/.copilot/permissions-config.json` (by project location) |
| Windsurf | `.windsurf/rules/*.md` / `.devin/rules/*`; `.windsurfrules`; `~/.codeium/windsurf/memories/global_rules.md` | `~/.codeium/windsurf/mcp_config.json` (global-only, `serverUrl`) | none | `.windsurf/workflows/<n>.md` | none | memories (opaque, path-associated) |
| Cline | `.clinerules/*.md` (dir-merge); `AGENTS.md`; `~/Documents/Cline/Rules/` | `cline_mcp_settings.json` (VS Code globalStorage / `~/.cline/...`), global-only | `.cline/skills/`, `.clinerules/skills/`, `.claude/skills/`; `~/.cline/skills/` | `.clinerules/workflows/<n>.md` (`/name.md`) | none (built-in read-only) | profile-global (not path-keyed) |
| Aider | `CONVENTIONS.md` (not auto-loaded; via `read:` in `.aider.conf.yml`) | none (core) | none | none (`/load` scripts only) | none (built-in `/architect`) | none (config by file location) |

## 7. Mapping Gotchas & Default Fallbacks

| Gap | Targets | Fallback (default) |
|---|---|---|
| Instruction imports (`@path`, `#[[file:]]`, recursive) | Codex, Antigravity, Cursor-AGENTS, Windsurf, Copilot, Cline, Aider | **Flatten/inline** into one body; `lossyFlags:[imports-flattened]`; warn |
| Skills → no-skills tools | Windsurf, Gemini CLI, Aider | Emit as instructions (+ optional command); warn loss of auto-invoke |
| Subagents → unsupported | Antigravity, Windsurf, Cline, Aider | **Skip + warn**; optionally append systemPrompt as manual command. Never silent-drop |
| Commands → Kiro/Aider/Codex | Kiro, Aider, Codex | Kiro→`inclusion: manual` steering/hook; Codex→Skill; Aider→`/load` script; warn |
| Command arg syntax | Gemini (TOML `{{args}}`), Cursor, Windsurf | Translate via `argSpec`; inline usage note where unsupported |
| MCP project-scope → global-only | Windsurf, Cline | Merge into global; name-collision → suffix; warn isolation lost |
| MCP remote key dialects (`url`/`serverUrl`/`httpUrl`, `type`) | Antigravity, Gemini, Copilot VS Code (`servers`) | Adapter-local remap from IR transport; drop unsupported keys + warn |
| MCP JSON → TOML | Codex | Structural transform + secret env-indirection |
| Inline secrets → env-indirect tools | Codex, Copilot | Extract → env var name + `.env` stub guidance; **never** write plaintext; warn |
| `~/.claude.json` home blob | all | Extract only mcp/perms/trust; never bulk-copy; warn trust must be re-granted |
| Permissions / hooks model | nearly all | Map allow/deny where present; else `unmapped` RawArtifact + warn |
| Ignore-file two-mode (block vs index) | Cursor, Windsurf, Cline, Aider | Map to nearest; collapse with warn |
| User rules in UI (no file) | Cursor User Rules, IDE Copilot | Emit manual paste-in instructions; warn |

**Default policy:** never silently drop; every lossy decision → one structured warning in the migration report.

## 8. Safety & Security

- **Dry-run by default**; writes require `--yes`/`--apply` or interactive confirmation.
- **Backups**: every overwritten file copied to `<file>.bak` (toggle `--no-backup`).
- **Conflict policy**: per-item `skip|overwrite|merge|suffix`; MCP server lists merge by name (collision → suffix + warn); instruction files merge via marked block append where merge chosen.
- **Secrets**: inline tokens in source MCP configs are never re-serialized as plaintext into TOML/Copilot targets — extracted to an env-var reference with a `.env` stub + guidance. Flag any plaintext secret found.
- **Trust gating**: Codex/Antigravity ignore project-scoped config until the folder is trusted — surface an explicit post-migration "grant trust" step per target.
- **Migration report**: `--report <path>` writes the structured warning list (also printed on completion).

## 9. Project Structure (Go)

```
agensync/
├─ cmd/agensync/main.go            # entrypoint, flag wiring (cobra)
├─ internal/
│  ├─ ir/                          # AgentConfigBundle + record types
│  ├─ adapter/                     # ToolAdapter interface + registry
│  │  ├─ claudecode/  codex/  kiro/  copilot/  cursor/  geminicli/
│  │  ├─ antigravity/  windsurf/  cline/  aider/
│  ├─ engine/                      # capability-driven mapping/gotcha engine
│  ├─ plan/                        # WritePlan, diff, conflict resolution, backup/apply
│  ├─ secret/                      # inline-secret detection + env-indirection
│  └─ tui/                         # Bubble Tea interactive flow
└─ docs/specs/
```

- **Libraries:** `cobra` (commands/flags), `bubbletea`+`bubbles`/`lipgloss` (TUI), `pelletier/go-toml/v2` (Codex/Gemini TOML), `goccy/go-yaml` or stdlib for frontmatter, stdlib `encoding/json`.
- **Testing:** per-adapter **golden-file** tests (sample From tree → expected To tree); engine table tests over capability combinations; round-trip tests (export→IR→import→export idempotence within a tool).

## 10. Risks (flagged)

- **Antigravity / Windsurf / Cline** formats are version-unstable — use fuzzy path matching (`.agents` vs `.agent`, `.windsurf` vs `.devin`) with fallbacks; mark adapter confidence in `Meta()`.
- **Capability honesty** is load-bearing: if an adapter overstates support, the gotcha engine won't warn. Capabilities are the single source of truth; round-trip tests guard it.
- **Copilot is 3 sub-surfaces** (CLI/IDE/cloud); cloud MCP is not file-based — out of scope, documented.
- **Opaque-hash home state** (Cursor/Gemini-tmp/Windsurf) is non-migratable as files — excluded, documented as "re-established on first run."
- **Aider / Windsurf** are near-dead-ends as targets — UX should set expectations (instructions-mostly).

## 11. Open Questions (defer to implementation)

- Exact precedence when multiple instruction files coexist in a target (per-tool).
- Whether to ship a `agensync init` that scaffolds a From baseline.
- Plugin-based adapter loading (external adapters) — post-v1.
