package plan

import (
	"strings"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestRenderDiffMarksNewAndChanged(t *testing.T) {
	p := ir.WritePlan{Tool: "codex", Files: []ir.PlannedFile{
		{Path: "AGENTS.md", Content: []byte("new"), Existing: nil, Mode: ir.ModeCreate},
		{Path: ".mcp", Content: []byte("b"), Existing: []byte("a"), Mode: ir.ModeOverwrite},
	}}
	out := RenderDiff(p)
	if !strings.Contains(out, "AGENTS.md") || !strings.Contains(out, "new file") {
		t.Fatalf("missing new-file marker:\n%s", out)
	}
	if !strings.Contains(out, "overwrite") {
		t.Fatalf("missing overwrite marker:\n%s", out)
	}
}

func TestRenderDiffShowsWarnings(t *testing.T) {
	p := ir.WritePlan{Tool: "aider", Warnings: []ir.Warning{
		{Category: "subagents", FromTool: "claude-code", ToTool: "aider", Action: ir.ActionSkip, Reason: "none"},
	}}
	out := RenderDiff(p)
	if !strings.Contains(out, "warn") || !strings.Contains(out, "subagents") {
		t.Fatalf("warnings not rendered:\n%s", out)
	}
}
