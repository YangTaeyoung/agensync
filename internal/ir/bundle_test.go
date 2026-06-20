package ir

import "testing"

func TestBundleSlugStability(t *testing.T) {
	s := Slug("My Cool Skill!")
	if s != "my-cool-skill" {
		t.Fatalf("got %q want my-cool-skill", s)
	}
}

func TestNewBundleDefaults(t *testing.T) {
	b := NewBundle(Source{Tool: "claude-code", ProjectPath: "/p"})
	if b.SchemaVersion == "" {
		t.Fatal("schemaVersion must be set")
	}
	if b.Source.Tool != "claude-code" {
		t.Fatalf("source tool not set: %+v", b.Source)
	}
}
