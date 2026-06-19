package adapter

import (
	"bytes"

	"github.com/goccy/go-yaml"
)

// ParseFrontmatter splits a YAML-frontmatter document into (map, body).
// Returns empty map + full input as body when no frontmatter delimiter is present.
func ParseFrontmatter(data []byte) (map[string]any, string, error) {
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return map[string]any{}, string(data), nil
	}
	rest := data[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return map[string]any{}, string(data), nil
	}
	head := rest[:end]
	body := rest[end+len("\n---\n"):]
	fm := map[string]any{}
	if err := yaml.Unmarshal(head, &fm); err != nil {
		return nil, "", err
	}
	return fm, string(body), nil
}

// RenderFrontmatter serializes (map, body) back into a frontmatter document.
func RenderFrontmatter(fm map[string]any, body string) ([]byte, error) {
	if len(fm) == 0 {
		return []byte(body), nil
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(head)
	buf.WriteString("---\n")
	buf.WriteString(body)
	return buf.Bytes(), nil
}
