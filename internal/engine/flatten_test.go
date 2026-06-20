package engine

import (
	"testing"

	"github.com/YangTaeyoung/agensync/internal/ir"
)

func TestFlattenInlinesImports(t *testing.T) {
	ins := ir.Instruction{
		Common:  ir.Common{Body: "intro\n@sub.md\noutro"},
		Imports: []ir.Import{{Kind: ir.ImpInline, Target: "sub.md", Resolved: "SUB CONTENT"}},
	}
	out := FlattenInstruction(ins)
	want := "intro\nSUB CONTENT\noutro"
	if out.Body != want {
		t.Fatalf("got %q want %q", out.Body, want)
	}
	found := false
	for _, f := range out.LossyFlags {
		if f == "imports-flattened" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected imports-flattened lossy flag")
	}
}

func TestFlattenFileEmbed(t *testing.T) {
	ins := ir.Instruction{
		Common:  ir.Common{Body: "before #[[file:ref.md]] after"},
		Imports: []ir.Import{{Kind: ir.ImpFileEmbed, Target: "ref.md", Resolved: "EMBED"}},
	}
	out := FlattenInstruction(ins)
	if out.Body != "before EMBED after" {
		t.Fatalf("got %q", out.Body)
	}
}

func TestFlattenNoImports(t *testing.T) {
	ins := ir.Instruction{Common: ir.Common{Body: "plain"}}
	out := FlattenInstruction(ins)
	if out.Body != "plain" || len(out.LossyFlags) != 0 {
		t.Fatalf("unexpected change: %+v", out)
	}
}
