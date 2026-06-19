package engine

// Categories are the migratable config categories, in display order.
// "memory" is personal/global-scope instructions (see ir.Instruction.IsMemory).
var Categories = []string{
	"instructions", "mcp", "skills", "commands", "subagents", "project-state", "memory",
}
