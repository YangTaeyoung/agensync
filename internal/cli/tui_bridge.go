package cli

import (
	"io"

	"github.com/YangTaeyoung/agensync/internal/tui"
)

// runTUI launches the interactive Bubble Tea flow. It resolves the project and
// home from the current environment.
func runTUI(out io.Writer) error {
	ctx, err := resolveContext("", "")
	if err != nil {
		return err
	}
	return tui.Run(ctx, out)
}
