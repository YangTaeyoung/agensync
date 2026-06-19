package all

import (
	"reflect"
	"sort"
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestDefaultRegistryHasAllAdapters(t *testing.T) {
	r := Default()
	want := []string{
		"aider", "antigravity", "claude-code", "cline", "codex",
		"copilot", "cursor", "gemini-cli", "kiro", "windsurf",
	}
	sort.Strings(want)
	got := r.IDs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry IDs\n got=%v\nwant=%v", got, want)
	}
}

// Every adapter must declare a Memory style so the gotcha engine can decide how
// to migrate personal memory — none may leave it unset.
func TestEveryAdapterDeclaresMemoryStyle(t *testing.T) {
	r := Default()
	for _, id := range r.IDs() {
		a, _ := r.Get(id)
		if a.Capabilities().Memory == "" {
			t.Errorf("%s: Memory capability unset", id)
		}
	}
}

// Every adapter must produce a stable Detect result and non-empty DisplayName.
func TestEveryAdapterMetaWellFormed(t *testing.T) {
	r := Default()
	for _, id := range r.IDs() {
		a, _ := r.Get(id)
		m := a.Meta()
		if m.ID == "" || m.DisplayName == "" || m.Confidence == "" {
			t.Errorf("%s: incomplete meta %+v", id, m)
		}
		// Detect on an empty context must not panic and must report absent.
		if a.Detect(ir.Context{}).Present {
			t.Errorf("%s: Detect on empty context should be absent", id)
		}
	}
}
