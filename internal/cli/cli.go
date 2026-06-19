// Package cli wires the cobra command tree for agensync: detect, migrate, and
// (with no subcommand) the interactive TUI.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/adapter/all"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/plan"
)

// KnownCategories are the migratable config categories.
var KnownCategories = engine.Categories

func isKnownCategory(c string) bool {
	for _, k := range KnownCategories {
		if k == c {
			return true
		}
	}
	return false
}

// ParseCategories parses a comma-separated category list, validating each name.
func ParseCategories(s string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, raw := range strings.Split(s, ",") {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		if !isKnownCategory(c) {
			return nil, fmt.Errorf("unknown category %q (known: %s)", c, strings.Join(KnownCategories, ", "))
		}
		out[c] = true
	}
	return out, nil
}

// ParseTools parses a comma-separated tool-id list, validating against the registry.
func ParseTools(s string) ([]string, error) {
	reg := all.Default()
	var out []string
	for _, raw := range strings.Split(s, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := reg.Get(id); !ok {
			return nil, fmt.Errorf("unknown tool %q (known: %s)", id, strings.Join(reg.IDs(), ", "))
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tools specified")
	}
	return out, nil
}

func parseAction(s string) ir.Action {
	switch s {
	case "overwrite":
		return ir.ActionOverwrite
	case "merge":
		return ir.ActionMerge
	case "suffix":
		return ir.ActionSuffix
	default:
		return ir.ActionSkip
	}
}

// Run executes agensync with the given args, writing user-facing output to out.
func Run(args []string, out io.Writer) error {
	root := &cobra.Command{
		Use:           "agensync",
		Short:         "Clone/migrate AI-coding-agent configuration between tools",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(out)
		},
	}
	root.SetOut(out)
	root.SetErr(out)
	root.AddCommand(detectCmd(out), migrateCmd(out))
	root.SetArgs(args)
	return root.Execute()
}

func resolveContext(project, home string) (ir.Context, error) {
	ctx := ir.Context{ProjectPath: project, HomeDir: home}
	if ctx.ProjectPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ctx, err
		}
		ctx.ProjectPath = wd
	}
	if ctx.HomeDir == "" {
		h, err := os.UserHomeDir()
		if err == nil {
			ctx.HomeDir = h
		}
	}
	return ctx, nil
}

func detectCmd(out io.Writer) *cobra.Command {
	var project, home string
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "List AI-coding tools detected in the project and home",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, err := resolveContext(project, home)
			if err != nil {
				return err
			}
			reg := all.Default()
			found := 0
			for _, id := range reg.IDs() {
				a, _ := reg.Get(id)
				res := a.Detect(ctx)
				if res.Present {
					found++
					fmt.Fprintf(out, "%-12s %-14s [%s]\n", id, a.Meta().DisplayName, strings.Join(evidenceScopes(res), ","))
				}
			}
			if found == 0 {
				fmt.Fprintln(out, "No AI-coding tools detected in this project or home directory.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project root (default: cwd)")
	cmd.Flags().StringVar(&home, "home", "", "home dir (default: $HOME)")
	return cmd
}

func evidenceScopes(res ir.DetectionResult) []string {
	var s []string
	for _, sc := range res.ScopesFound {
		s = append(s, string(sc))
	}
	if len(s) == 0 {
		s = append(s, "present")
	}
	return s
}

func migrateCmd(out io.Writer) *cobra.Command {
	var (
		from, to, only, skip, onConflict string
		project, home, report            string
		dryRun, yes, apply, noBackup     bool
	)
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate config from one tool to one or more others",
		RunE: func(_ *cobra.Command, _ []string) error {
			if from == "" || to == "" {
				return fmt.Errorf("--from and --to are required")
			}
			ctx, err := resolveContext(project, home)
			if err != nil {
				return err
			}
			reg := all.Default()
			src, ok := reg.Get(from)
			if !ok {
				return fmt.Errorf("unknown --from tool %q", from)
			}
			targets, err := ParseTools(to)
			if err != nil {
				return err
			}
			cats, err := resolveCategories(only, skip)
			if err != nil {
				return err
			}
			opts := adapter.ImportOptions{Categories: cats, OnConflict: parseAction(onConflict)}

			bundle, err := engine.Export(src, ctx, opts)
			if err != nil {
				return fmt.Errorf("export from %s: %w", from, err)
			}

			doApply := apply || yes
			_ = dryRun // dry-run is the default; --apply/--yes opts into writes

			var rep strings.Builder
			for _, id := range targets {
				dst, _ := reg.Get(id)
				p := engine.Plan(dst, bundle, ctx, opts)
				section := plan.RenderDiff(p)
				fmt.Fprint(out, section)
				rep.WriteString(section)

				if doApply {
					res := dst.Apply(p, adapter.ApplyOptions{DryRun: false, Backup: !noBackup, OnConflict: parseAction(onConflict)})
					summary := renderApply(id, res)
					fmt.Fprint(out, summary)
					rep.WriteString(summary)
					emitTrustGuidance(out, dst, p)
				}
			}
			if !doApply {
				fmt.Fprintln(out, "\n(dry-run — no files written; pass --apply to write, with .bak backups)")
			}
			if report != "" {
				if err := os.WriteFile(report, []byte(rep.String()), 0o644); err != nil {
					return fmt.Errorf("write report: %w", err)
				}
				fmt.Fprintf(out, "report written to %s\n", report)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&from, "from", "", "source tool id")
	f.StringVar(&to, "to", "", "comma-separated target tool ids")
	f.StringVar(&only, "only", "", "only these categories (comma-separated)")
	f.StringVar(&skip, "skip", "", "skip these categories (comma-separated)")
	f.StringVar(&onConflict, "on-conflict", "skip", "conflict policy: skip|overwrite|merge|suffix")
	f.StringVar(&project, "project", "", "project root (default: cwd)")
	f.StringVar(&home, "home", "", "home dir (default: $HOME)")
	f.StringVar(&report, "report", "", "write the migration report to this path")
	f.BoolVar(&dryRun, "dry-run", true, "plan only, do not write (default)")
	f.BoolVar(&yes, "yes", false, "apply without confirmation")
	f.BoolVar(&apply, "apply", false, "apply the migration (write files)")
	f.BoolVar(&noBackup, "no-backup", false, "do not create .bak backups")
	return cmd
}

func resolveCategories(only, skip string) (map[string]bool, error) {
	if only != "" {
		return ParseCategories(only)
	}
	if skip != "" {
		skipped, err := ParseCategories(skip)
		if err != nil {
			return nil, err
		}
		out := map[string]bool{}
		for _, c := range KnownCategories {
			if !skipped[c] {
				out[c] = true
			}
		}
		return out, nil
	}
	return nil, nil // empty == all
}

func renderApply(id string, res ir.ApplyResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "applied to %s: %d written, %d backed up, %d skipped\n", id, len(res.Written), len(res.BackedUp), len(res.Skipped))
	for _, e := range res.Errors {
		fmt.Fprintf(&b, "  error: %v\n", e)
	}
	return b.String()
}

func emitTrustGuidance(out io.Writer, dst adapter.ToolAdapter, p ir.WritePlan) {
	for _, w := range p.Warnings {
		if w.Category == "project-state" && strings.Contains(strings.ToLower(w.Reason), "trust") {
			fmt.Fprintf(out, "  → %s: grant trust for this folder before first run\n", dst.Meta().DisplayName)
			return
		}
	}
}
