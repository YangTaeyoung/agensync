// Package adapter defines the ToolAdapter contract every AI-coding tool plugin
// implements, plus a registry and shared parsing helpers.
package adapter

import (
	"sort"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

type AdapterMeta struct {
	ID          string
	DisplayName string
	Vendor      string
	Confidence  string // "high" | "medium" | "low"
}

type ImportOptions struct {
	Categories map[string]bool // empty = all
	OnConflict ir.Action       // merge|overwrite|suffix|skip
}

func (o ImportOptions) Wants(cat string) bool {
	if len(o.Categories) == 0 {
		return true
	}
	return o.Categories[cat]
}

type ApplyOptions struct {
	DryRun     bool
	Backup     bool
	OnConflict ir.Action
}

type ToolAdapter interface {
	Meta() AdapterMeta
	Detect(ctx ir.Context) ir.DetectionResult
	ExportInstructions(ctx ir.Context) ([]ir.Instruction, error)
	ExportMcpServers(ctx ir.Context) ([]ir.McpServer, error)
	ExportSkills(ctx ir.Context) ([]ir.Skill, error)
	ExportCommands(ctx ir.Context) ([]ir.Command, error)
	ExportSubagents(ctx ir.Context) ([]ir.Subagent, error)
	ExportProjectState(ctx ir.Context) (ir.ProjectState, error)
	Capabilities() ir.Capabilities
	PlanImport(bundle ir.AgentConfigBundle, ctx ir.Context, opts ImportOptions) ir.WritePlan
	Apply(plan ir.WritePlan, opts ApplyOptions) ir.ApplyResult
}

type Registry struct{ m map[string]ToolAdapter }

func NewRegistry() *Registry { return &Registry{m: map[string]ToolAdapter{}} }

func (r *Registry) Register(a ToolAdapter) { r.m[a.Meta().ID] = a }

func (r *Registry) Get(id string) (ToolAdapter, bool) { a, ok := r.m[id]; return a, ok }

func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.m))
	for id := range r.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
