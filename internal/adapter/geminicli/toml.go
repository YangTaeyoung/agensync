package geminicli

import (
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// geminiCommand is the on-disk shape of a .gemini/commands/*.toml command.
type geminiCommand struct {
	Prompt      string `toml:"prompt"`
	Description string `toml:"description"`
}

func decodeCommandTOML(b []byte) (geminiCommand, error) {
	var c geminiCommand
	err := toml.Unmarshal(b, &c)
	return c, err
}

// encodeCommandTOML renders a command as deterministic {description, prompt} TOML.
// The prompt is emitted as a multi-line basic string when it contains newlines.
func encodeCommandTOML(description, prompt string) []byte {
	var b strings.Builder
	if description != "" {
		b.WriteString("description = " + tomlString(description) + "\n")
	}
	b.WriteString("prompt = " + tomlMultiline(prompt) + "\n")
	return []byte(b.String())
}

func tomlString(s string) string { return strconv.Quote(s) }

// tomlMultiline emits a TOML multi-line basic string ("""...""") when the value
// spans multiple lines, otherwise a normal quoted string. The closing delimiter
// is placed on its own line so re-parsing is stable.
func tomlMultiline(s string) string {
	if !strings.Contains(s, "\n") {
		return tomlString(s)
	}
	// Escape backslashes and triple-quote sequences that would break the literal.
	esc := strings.ReplaceAll(s, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"""`, `\"\"\"`)
	return "\"\"\"\n" + esc + "\n\"\"\""
}
