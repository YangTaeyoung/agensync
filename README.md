# agensync

Clone or migrate a project's **AI-coding-agent configuration** from one tool to
one or more others — instructions, MCP servers, skills, commands, subagents,
project state, and personal/global **memory** — across both project-local files
and home-directory project-scoped settings.

Set up your conventions once in (say) Claude Code, then carry the same
experience into Codex, Cursor, Gemini CLI, Kiro, Copilot, and more.

## Install

```bash
go install github.com/YangTaeyoung/agensync/cmd/agensync@latest
```

Requires Go 1.26+.

## Usage

Run from your project root.

### Interactive

```bash
agensync
```

Walks you through: **From** tool → **To** tool(s) → categories → plan preview
(with a loss/transformation report) → per-conflict decisions → apply (with
automatic `.bak` backups and post-migration "grant trust" guidance).

### Non-interactive

```bash
# Detect which tools are configured in this project / your home dir
agensync detect

# Plan only (default is dry-run — writes nothing)
agensync migrate --from claude-code --to codex --dry-run

# Migrate selected categories to several tools and apply
agensync migrate --from claude-code --to codex,cursor --only mcp,instructions --apply

# Carry your personal/global memory across (user-scope instruction files)
agensync migrate --from claude-code --to codex --only memory --apply

# Write the structured migration report to a file
agensync migrate --from claude-code --to kiro --report report.txt
```

### Flags

| Flag | Meaning |
|---|---|
| `--from <id>` | source tool |
| `--to <ids>` | comma-separated targets |
| `--only <cats>` / `--skip <cats>` | category filter |
| `--dry-run` | plan only (default) |
| `--apply` / `--yes` | write files |
| `--on-conflict skip\|overwrite\|merge\|suffix` | conflict policy |
| `--no-backup` | don't create `.bak` files |
| `--home <dir>` / `--project <dir>` | override resolved paths |
| `--report <path>` | write the migration report |

**Categories:** `instructions`, `mcp`, `skills`, `commands`, `subagents`,
`project-state`, `memory`.

## Supported tools

| Tool (`id`) | Tier / confidence |
|---|---|
| Claude Code (`claude-code`) | Tier 1 — high |
| Codex CLI (`codex`) | Tier 1 — high |
| Kiro (`kiro`) | Tier 1 — high |
| GitHub Copilot (`copilot`) | Tier 1 — high |
| Cursor (`cursor`) | Tier 1 — high |
| Gemini CLI (`gemini-cli`) | Tier 1 — high |
| Antigravity (`antigravity`) | Tier 2 — medium (fuzzy paths) |
| Windsurf / Devin (`windsurf`) | Tier 2 — medium |
| Cline (`cline`) | Tier 2 — medium |
| Aider (`aider`) | Tier 3 — instructions-only |

Confidence reflects format stability. Tier 2/3 tools use fuzzy path matching and
fallbacks; Aider and Windsurf are instructions-mostly targets.

## Personal memory

Your user/global memory (e.g. `~/.claude/CLAUDE.md`) is migrated as user-scope
instructions to each target's global memory file
(`~/.codex/AGENTS.md`, `~/.gemini/GEMINI.md`, Windsurf `global_rules.md`,
Cline `~/Documents/Cline/Rules/`, …) via the `memory` category. Where memory is
opaque or UI-only (Cursor User Rules, Windsurf auto-memories), agensync **warns
and preserves the content for manual paste-in** rather than silently dropping it.

## Safety

- **Dry-run by default.** Writes require `--apply`/`--yes` or interactive confirmation.
- **Backups.** Every overwritten file is copied to `<file>.bak` (the original is
  preserved even across multi-target runs). Toggle with `--no-backup`.
- **Secrets are never written as plaintext.** Inline tokens in source MCP configs
  are externalized to an env-var reference plus a `.env` stub, with a warning.
- **Trust gating.** Tools that ignore project config until the folder is trusted
  (Codex, Antigravity, …) get an explicit post-migration "grant trust" step.
- **Never silently drop.** Every category a target can't represent produces one
  structured warning in the migration report.

## How it works

Every tool maps to/from a shared canonical IR (`AgentConfigBundle`) via an
adapter, so coverage grows by adding adapters rather than N×N converters. A
capability-driven engine plans the target writes and emits structured loss
warnings; the plan/apply layer renders diffs and writes with backups.

```
[From files] --adapter.export--> IR --capability/gotcha engine--> WritePlan --apply--> [To files]
```

See `docs/specs/2026-06-19-agensync-design.md` for the full design.
