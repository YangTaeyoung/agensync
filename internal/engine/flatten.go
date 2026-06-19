// Package engine orchestrates export→IR→plan and provides the capability-driven
// mapping/gotcha helpers shared by adapters.
package engine

import (
	"strings"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

// FlattenInstruction replaces each resolved import marker in the body with its
// resolved content, for targets that lack a transclusion mechanism. Handles
// inline (@path), file-embed (#[[file:path]]) and reference markers.
func FlattenInstruction(in ir.Instruction) ir.Instruction {
	if len(in.Imports) == 0 {
		return in
	}
	body := in.Body
	for _, imp := range in.Imports {
		if imp.Resolved == "" {
			continue
		}
		for _, marker := range importMarkers(imp) {
			body = strings.ReplaceAll(body, marker, imp.Resolved)
		}
	}
	in.Body = body
	in.Imports = nil
	in.LossyFlags = append(in.LossyFlags, "imports-flattened")
	return in
}

// importMarkers returns the literal text(s) a given import may appear as in a body.
func importMarkers(imp ir.Import) []string {
	switch imp.Kind {
	case ir.ImpFileEmbed:
		return []string{"#[[file:" + imp.Target + "]]"}
	default: // ImpInline, ImpReference
		return []string{"@" + imp.Target}
	}
}
