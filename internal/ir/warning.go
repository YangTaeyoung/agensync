package ir

import "fmt"

type Action string

const (
	ActionSkip      Action = "skip"
	ActionInline    Action = "inline"
	ActionMerge     Action = "merge"
	ActionManual    Action = "manual"
	ActionOverwrite Action = "overwrite"
	ActionSuffix    Action = "suffix"
)

type Warning struct {
	Category string
	FromTool string
	ToTool   string
	Artifact string
	Action   Action
	Reason   string
}

func (w Warning) String() string {
	return fmt.Sprintf("[%s] %s→%s %q: %s (%s)", w.Category, w.FromTool, w.ToTool, w.Artifact, w.Action, w.Reason)
}

type FileMode int

const (
	ModeCreate FileMode = iota
	ModeOverwrite
	ModeMerge
	ModeSkip
)

type PlannedFile struct {
	Path     string
	Content  []byte
	Mode     FileMode
	Existing []byte // nil if file does not exist
}

type WritePlan struct {
	Tool     string
	Files    []PlannedFile
	Warnings []Warning
	Skipped  []string
}

type ApplyResult struct {
	Written  []string
	BackedUp []string
	Skipped  []string
	Errors   []error
}

type DetectionResult struct {
	Present     bool
	ScopesFound []Scope
	Evidence    []string
}

// Context carries resolved environment paths for an adapter run.
type Context struct {
	ProjectPath string
	HomeDir     string
}
