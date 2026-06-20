package ir

import (
	"strings"
	"testing"
)

func TestWarningString(t *testing.T) {
	w := Warning{Category: "subagents", FromTool: "claude-code", ToTool: "aider", Action: ActionSkip, Reason: "no subagents"}
	if w.String() == "" || !strings.Contains(w.String(), "aider") {
		t.Fatalf("bad warning string: %q", w.String())
	}
}
