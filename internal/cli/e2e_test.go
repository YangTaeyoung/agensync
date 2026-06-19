package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Full migration from a real Claude Code project to Codex + Cursor with --apply,
// asserting both targets' files are produced and an existing file is backed up.
func TestEndToEndClaudeToCodexAndCursor(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(proj, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("CLAUDE.md", "# Guidelines\nWrite tests first.\n")
	write(".mcp.json", `{"mcpServers":{"ctx7":{"type":"stdio","command":"npx","args":["-y","pkg"]}}}`)
	write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: demo skill\n---\nDo the thing.\n")
	// pre-existing AGENTS.md so we can assert a .bak backup is created
	write("AGENTS.md", "OLD AGENTS\n")

	var out bytes.Buffer
	err := Run([]string{
		"migrate", "--from", "claude-code", "--to", "codex,cursor",
		"--project", proj, "--home", home, "--apply",
	}, &out)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}

	mustExist := []string{
		"AGENTS.md",
		"AGENTS.md.bak", // existing AGENTS.md backed up before overwrite
		filepath.Join(".codex", "config.toml"),
		filepath.Join(".agents", "skills", "demo", "SKILL.md"),
		filepath.Join(".cursor", "mcp.json"),
	}
	for _, rel := range mustExist {
		if _, err := os.Stat(filepath.Join(proj, rel)); err != nil {
			t.Errorf("expected %s: %v", rel, err)
		}
	}

	bak, _ := os.ReadFile(filepath.Join(proj, "AGENTS.md.bak"))
	if string(bak) != "OLD AGENTS\n" {
		t.Fatalf("backup content = %q", bak)
	}
	toml, _ := os.ReadFile(filepath.Join(proj, ".codex", "config.toml"))
	if !strings.Contains(string(toml), "mcp_servers.ctx7") {
		t.Fatalf("codex TOML missing server:\n%s", toml)
	}
	if !strings.Contains(out.String(), "applied to codex") {
		t.Fatalf("missing apply summary:\n%s", out.String())
	}
}

// Personal-memory migration end-to-end: a user-scope ~/.claude/CLAUDE.md is
// carried into Codex's global ~/.codex/AGENTS.md via the memory category.
func TestEndToEndMemoryMigration(t *testing.T) {
	proj := t.TempDir()
	srcHome := t.TempDir()
	os.MkdirAll(filepath.Join(srcHome, ".claude"), 0o755)
	os.WriteFile(filepath.Join(srcHome, ".claude", "CLAUDE.md"), []byte("Remember: I prefer Go.\n"), 0o644)

	var out bytes.Buffer
	err := Run([]string{
		"migrate", "--from", "claude-code", "--to", "codex",
		"--project", proj, "--home", srcHome, "--only", "memory", "--apply",
	}, &out)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	mem, err := os.ReadFile(filepath.Join(srcHome, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatalf("memory not migrated to ~/.codex/AGENTS.md: %v", err)
	}
	if !strings.Contains(string(mem), "prefer Go") {
		t.Fatalf("memory content = %q", mem)
	}
}
