package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCategories(t *testing.T) {
	got, err := ParseCategories("mcp,instructions")
	if err != nil {
		t.Fatal(err)
	}
	if !got["mcp"] || !got["instructions"] || len(got) != 2 {
		t.Fatalf("got=%v", got)
	}
	if _, err := ParseCategories("bogus"); err == nil {
		t.Fatal("expected error for invalid category")
	}
	if _, err := ParseCategories("memory"); err != nil {
		t.Fatalf("memory must be a valid category: %v", err)
	}
}

func TestParseTools(t *testing.T) {
	ids, err := ParseTools("claude-code,codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "claude-code" {
		t.Fatalf("ids=%v", ids)
	}
	if _, err := ParseTools("claude-code,nope"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDetectCommand(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# hi\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	var out bytes.Buffer
	err := Run([]string{"detect", "--project", dir, "--home", t.TempDir()}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "claude-code") {
		t.Fatalf("detect did not find claude-code:\n%s", out.String())
	}
}

// Non-interactive migrate dry-run: shows planned AGENTS.md + TOML, writes nothing.
func TestMigrateDryRunClaudeToCodex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Guidelines\nWrite tests.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"ctx7":{"type":"stdio","command":"npx","args":["-y","pkg"]}}}`), 0o644)
	var out bytes.Buffer
	err := Run([]string{
		"migrate", "--from", "claude-code", "--to", "codex",
		"--project", dir, "--home", t.TempDir(), "--dry-run",
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "AGENTS.md") {
		t.Fatalf("expected planned AGENTS.md:\n%s", s)
	}
	if !strings.Contains(s, "mcp_servers") && !strings.Contains(s, "config.toml") {
		t.Fatalf("expected codex TOML plan:\n%s", s)
	}
	// dry-run writes nothing
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write AGENTS.md")
	}
}

func TestMigrateApplyWritesAndBacks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Guidelines\n"), 0o644)
	var out bytes.Buffer
	err := Run([]string{
		"migrate", "--from", "claude-code", "--to", "codex",
		"--project", dir, "--home", t.TempDir(), "--apply", "--only", "instructions",
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("apply should write AGENTS.md: %v", err)
	}
}

func TestReportFlagWritesFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Guidelines\n"), 0o644)
	report := filepath.Join(dir, "report.txt")
	var out bytes.Buffer
	err := Run([]string{
		"migrate", "--from", "claude-code", "--to", "aider",
		"--project", dir, "--home", t.TempDir(), "--dry-run", "--report", report,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("report not written: %v", err)
	}
}
