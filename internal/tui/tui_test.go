package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func press(m model, s string) model {
	var k tea.KeyMsg
	switch s {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		k = tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	}
	nm, _ := m.Update(k)
	return nm.(model)
}

func indexOf(ss []string, v string) int {
	for i, s := range ss {
		if s == v {
			return i
		}
	}
	return -1
}

// selectTarget moves the cursor to id and toggles it (used on stepTo).
func selectTarget(m model, id string) model {
	want := indexOf(m.fromID, id)
	for m.cursor < want {
		m = press(m, "down")
	}
	return press(m, "space")
}

func TestInteractiveFlowAppliesMigration(t *testing.T) {
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("# rules\n"), 0o644)
	m := newModel(ir.Context{ProjectPath: proj, HomeDir: t.TempDir()}, io.Discard)

	if m.fromID[0] != "claude-code" {
		t.Fatalf("detected claude-code should be first: %v", m.fromID)
	}
	_ = m.View() // first screen renders without panic
	m = press(m, "enter")
	if m.step != stepTo {
		t.Fatalf("step=%d", m.step)
	}
	m = selectTarget(m, "codex")
	m = press(m, "enter") // -> cats
	m = press(m, "enter") // cats default all -> options
	if m.step != stepOptions {
		t.Fatalf("expected options step, got %d", m.step)
	}
	m = press(m, "enter") // options -> preview
	if m.step != stepPreview || m.nFiles == 0 {
		t.Fatalf("preview not computed: step=%d files=%d", m.step, m.nFiles)
	}
	_ = m.View()
	m = press(m, "enter") // apply
	if m.step != stepApplied {
		t.Fatalf("step=%d", m.step)
	}
	if !strings.Contains(m.applied, "codex") {
		t.Fatalf("apply log:\n%s", m.applied)
	}
	if _, err := os.Stat(filepath.Join(proj, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}
}

// Toggling Overwrite OFF must leave an existing target file untouched.
func TestInteractiveOverwriteOffSkips(t *testing.T) {
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("# rules\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("KEEP ME\n"), 0o644)
	m := newModel(ir.Context{ProjectPath: proj, HomeDir: t.TempDir()}, io.Discard)

	m = press(m, "enter")        // from = claude-code
	m = selectTarget(m, "codex") // to = codex
	m = press(m, "enter")        // -> cats
	// only instructions to keep it simple: deselect all then enable instructions
	m = press(m, "enter") // cats (all) -> options
	m = press(m, "space") // toggle Overwrite OFF (cursor 0)
	if m.overwrite {
		t.Fatal("overwrite should be off after toggle")
	}
	m = press(m, "enter") // -> preview
	m = press(m, "enter") // apply

	got, _ := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if string(got) != "KEEP ME\n" {
		t.Fatalf("overwrite-off must keep existing AGENTS.md, got %q", got)
	}
	if !strings.Contains(m.applied, "skipped") {
		t.Fatalf("expected a skip in the log:\n%s", m.applied)
	}
}

// Toggling Recurse ON migrates nested projects in place.
func TestInteractiveRecursiveToggle(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# root\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "svc", "a"), 0o755)
	os.WriteFile(filepath.Join(root, "svc", "a", "CLAUDE.md"), []byte("# a\n"), 0o644)

	m := newModel(ir.Context{ProjectPath: root, HomeDir: t.TempDir()}, io.Discard)
	m = press(m, "enter")        // from = claude-code
	m = selectTarget(m, "codex") // to = codex
	m = press(m, "enter")        // -> cats
	m = press(m, "enter")        // -> options
	m = press(m, "down")         // move to Recurse toggle
	m = press(m, "space")        // Recurse ON
	if !m.recursive {
		t.Fatal("recursive should be on")
	}
	m = press(m, "enter") // -> preview
	m = press(m, "enter") // apply

	for _, rel := range []string{"AGENTS.md", filepath.Join("svc", "a", "AGENTS.md")} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("recursive interactive should write %s: %v", rel, err)
		}
	}
}

// esc returns to the previous step without losing prior selections.
func TestInteractiveBackNavigation(t *testing.T) {
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("# rules\n"), 0o644)
	m := newModel(ir.Context{ProjectPath: proj, HomeDir: t.TempDir()}, io.Discard)
	m = press(m, "enter") // -> stepTo
	m = press(m, "esc")   // back to stepFrom
	if m.step != stepFrom {
		t.Fatalf("esc should return to from, step=%d", m.step)
	}
}
