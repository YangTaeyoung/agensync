package adapter

import "testing"

func TestParseFrontmatter(t *testing.T) {
	in := "---\nname: foo\ndescription: bar\n---\nbody here\n"
	fm, body, err := ParseFrontmatter([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if fm["name"] != "foo" || fm["description"] != "bar" {
		t.Fatalf("fm=%v", fm)
	}
	if body != "body here\n" {
		t.Fatalf("body=%q", body)
	}
}

func TestParseFrontmatterNone(t *testing.T) {
	fm, body, err := ParseFrontmatter([]byte("just body"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fm) != 0 || body != "just body" {
		t.Fatalf("fm=%v body=%q", fm, body)
	}
}

func TestRenderFrontmatterRoundTrip(t *testing.T) {
	fm := map[string]any{"name": "foo"}
	out, err := RenderFrontmatter(fm, "hello\n")
	if err != nil {
		t.Fatal(err)
	}
	got, body, err := ParseFrontmatter(out)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "foo" || body != "hello\n" {
		t.Fatalf("roundtrip fm=%v body=%q", got, body)
	}
}

func TestRenderFrontmatterEmpty(t *testing.T) {
	out, err := RenderFrontmatter(map[string]any{}, "body")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "body" {
		t.Fatalf("got %q", out)
	}
}
