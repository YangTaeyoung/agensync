package engine

import (
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestUnsupportedSubagentSkips(t *testing.T) {
	w := UnsupportedSubagent("claude-code", "aider", "x")
	if w.Action != ir.ActionSkip {
		t.Fatalf("action=%s", w.Action)
	}
	if w.Category != "subagents" || w.ToTool != "aider" {
		t.Fatalf("warning=%+v", w)
	}
}

func TestWarnConstructor(t *testing.T) {
	w := Warn("mcp", "a", "b", "srv", ir.ActionMerge, "global only")
	if w.Action != ir.ActionMerge || w.Artifact != "srv" {
		t.Fatalf("warning=%+v", w)
	}
}

func TestMemoryUnsupportedWarns(t *testing.T) {
	w := MemoryUnsupported("claude-code", "cursor", ir.MemoryUI, "global memory")
	if w.Category != "memory" || w.Action != ir.ActionManual {
		t.Fatalf("ui memory should be manual: %+v", w)
	}
	w2 := MemoryUnsupported("claude-code", "aider", ir.MemoryNone, "global memory")
	if w2.Action != ir.ActionSkip {
		t.Fatalf("none memory should be skip: %+v", w2)
	}
}
