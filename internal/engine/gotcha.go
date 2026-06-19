package engine

import "github.com/YangTaeyoung/agensync/internal/ir"

// Warn is a thin constructor for a structured loss/transformation warning.
// Adapters call these helpers inside PlanImport so that no IR record the target
// cannot represent is ever silently dropped.
func Warn(cat, from, to, artifact string, action ir.Action, reason string) ir.Warning {
	return ir.Warning{Category: cat, FromTool: from, ToTool: to, Artifact: artifact, Action: action, Reason: reason}
}

// Skip records a category/artifact the target cannot represent at all.
func Skip(cat, from, to, artifact, reason string) ir.Warning {
	return Warn(cat, from, to, artifact, ir.ActionSkip, reason)
}

// UnsupportedSubagent is the standard skip warning for tools without subagents.
func UnsupportedSubagent(from, to, name string) ir.Warning {
	return Skip("subagents", from, to, name, "target has no subagent concept")
}

// UnsupportedSkill is the standard warning for skills→no-skills tools; the
// caller is expected to also emit the skill body as instructions.
func UnsupportedSkill(from, to, name string) ir.Warning {
	return Warn("skills", from, to, name, ir.ActionInline, "no skills support; emitted as instructions (auto-invoke lost)")
}

// UnsupportedCommand is the standard warning for commands→no-commands tools.
func UnsupportedCommand(from, to, name, reason string) ir.Warning {
	return Warn("commands", from, to, name, ir.ActionInline, reason)
}

// MemoryUnsupported maps a target's MemoryStyle to the right loss action for a
// personal/global-memory record that cannot be written as a file.
func MemoryUnsupported(from, to string, style ir.MemoryStyle, artifact string) ir.Warning {
	switch style {
	case ir.MemoryUI:
		return Warn("memory", from, to, artifact, ir.ActionManual, "personal memory lives in the app UI; paste in manually")
	case ir.MemoryOpaque:
		return Warn("memory", from, to, artifact, ir.ActionManual, "personal memory is an opaque store; re-established on first run")
	default: // MemoryNone
		return Skip("memory", from, to, artifact, "target has no personal-memory concept")
	}
}
