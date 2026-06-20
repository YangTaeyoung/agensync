// Package all wires every built-in tool adapter into a ready-to-use registry.
package all

import (
	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/adapter/aider"
	"github.com/YangTaeyoung/agensync/internal/adapter/antigravity"
	"github.com/YangTaeyoung/agensync/internal/adapter/claudecode"
	"github.com/YangTaeyoung/agensync/internal/adapter/cline"
	"github.com/YangTaeyoung/agensync/internal/adapter/codex"
	"github.com/YangTaeyoung/agensync/internal/adapter/copilot"
	"github.com/YangTaeyoung/agensync/internal/adapter/cursor"
	"github.com/YangTaeyoung/agensync/internal/adapter/geminicli"
	"github.com/YangTaeyoung/agensync/internal/adapter/kiro"
	"github.com/YangTaeyoung/agensync/internal/adapter/windsurf"
)

// Default returns a registry populated with all built-in adapters.
func Default() *adapter.Registry {
	r := adapter.NewRegistry()
	for _, a := range []adapter.ToolAdapter{
		claudecode.New(),
		codex.New(),
		kiro.New(),
		copilot.New(),
		cursor.New(),
		geminicli.New(),
		antigravity.New(),
		windsurf.New(),
		cline.New(),
		aider.New(),
	} {
		r.Register(a)
	}
	return r
}
