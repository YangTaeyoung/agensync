// Package tui implements the interactive Bubble Tea flow that wraps the same
// engine/plan pipeline the non-interactive CLI uses. All business logic stays
// in engine/plan; this package only collects choices and renders results.
package tui

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/YangTaeyoung/agensync/internal/adapter"
	"github.com/YangTaeyoung/agensync/internal/adapter/all"
	"github.com/YangTaeyoung/agensync/internal/engine"
	"github.com/YangTaeyoung/agensync/internal/ir"
	"github.com/YangTaeyoung/agensync/internal/plan"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	okMark        = selectedStyle.Render("✓")
)

type step int

const (
	stepFrom step = iota
	stepTo
	stepCats
	stepOptions
	stepPreview
	stepApplied
)

// Run launches the interactive flow for the given context.
func Run(ctx ir.Context, out io.Writer) error {
	m := newModel(ctx, out)
	p := tea.NewProgram(m, tea.WithOutput(out))
	_, err := p.Run()
	return err
}

type migUnit struct {
	ctx   ir.Context
	label string // relative dir label in recursive mode; "" otherwise
	plans map[string]ir.WritePlan
}

type model struct {
	reg *adapter.Registry
	ctx ir.Context
	out io.Writer

	step   step
	fromID []string
	cursor int
	from   string

	toSel  map[string]bool
	catSel map[string]bool

	overwrite bool
	recursive bool

	root    string
	units   []migUnit
	preview string
	nFiles  int
	nWarn   int
	applied string
	quit    bool
}

func newModel(ctx ir.Context, out io.Writer) model {
	reg := all.Default()
	return model{
		reg:       reg,
		ctx:       ctx,
		out:       out,
		fromID:    detectedFirst(reg, ctx),
		toSel:     map[string]bool{},
		catSel:    allCats(),
		overwrite: true, // sensible default; .bak backups are always kept
	}
}

func detectedFirst(reg *adapter.Registry, ctx ir.Context) []string {
	var detected, rest []string
	for _, id := range reg.IDs() {
		a, _ := reg.Get(id)
		if a.Detect(ctx).Present {
			detected = append(detected, id)
		} else {
			rest = append(rest, id)
		}
	}
	return append(detected, rest...)
}

func (m model) detectedIDs() []string {
	var out []string
	for _, id := range m.fromID {
		a, _ := m.reg.Get(id)
		if a.Detect(m.ctx).Present {
			out = append(out, id)
		}
	}
	return out
}

func allCats() map[string]bool {
	m := map[string]bool{}
	for _, c := range engine.Categories {
		m[c] = true
	}
	return m
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit
	case "esc", "left", "h":
		m.back()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.maxCursor() {
			m.cursor++
		}
	case " ":
		m.toggle()
	case "enter", "right", "l":
		return m.advance()
	}
	return m, nil
}

func (m model) maxCursor() int {
	switch m.step {
	case stepFrom, stepTo:
		return len(m.fromID) - 1
	case stepCats:
		return len(engine.Categories) - 1
	case stepOptions:
		return 1
	default:
		return 0
	}
}

func (m *model) toggle() {
	switch m.step {
	case stepTo:
		id := m.fromID[m.cursor]
		if id == m.from {
			return // can't target the source
		}
		m.toSel[id] = !m.toSel[id]
	case stepCats:
		c := engine.Categories[m.cursor]
		m.catSel[c] = !m.catSel[c]
	case stepOptions:
		if m.cursor == 0 {
			m.overwrite = !m.overwrite
		} else {
			m.recursive = !m.recursive
		}
	}
}

func (m *model) back() {
	switch m.step {
	case stepTo:
		m.step = stepFrom
	case stepCats:
		m.step = stepTo
	case stepOptions:
		m.step = stepCats
	case stepPreview:
		m.step = stepOptions
	default:
		return
	}
	m.cursor = 0
}

func (m model) advance() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepFrom:
		m.from = m.fromID[m.cursor]
		m.step = stepTo
		m.cursor = 0
	case stepTo:
		if len(m.selectedTargets()) == 0 {
			return m, nil
		}
		m.step = stepCats
		m.cursor = 0
	case stepCats:
		m.step = stepOptions
		m.cursor = 0
	case stepOptions:
		m.computePreview()
		m.step = stepPreview
	case stepPreview:
		m.apply()
		m.step = stepApplied
	case stepApplied:
		m.quit = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) selectedTargets() []string {
	var out []string
	for _, id := range m.fromID {
		if m.toSel[id] {
			out = append(out, id)
		}
	}
	return out
}

func (m model) conflictAction() ir.Action {
	if m.overwrite {
		return ir.ActionOverwrite
	}
	return ir.ActionSkip
}

func (m model) importOptions() adapter.ImportOptions {
	cats := map[string]bool{}
	for c, on := range m.catSel {
		if on {
			cats[c] = true
		}
	}
	return adapter.ImportOptions{Categories: cats, OnConflict: m.conflictAction()}
}

func (m model) labelFor(c ir.Context) string {
	if !m.recursive {
		return ""
	}
	rel, err := filepath.Rel(m.root, c.ProjectPath)
	if err != nil || rel == "." || rel == "" {
		return "(root)"
	}
	return rel
}

func (m *model) computePreview() {
	src, _ := m.reg.Get(m.from)
	opts := m.importOptions()
	m.root = m.ctx.ProjectPath
	if m.recursive {
		m.root = engine.FindProjectRoot(m.ctx.ProjectPath, m.ctx.HomeDir)
	}
	m.units = nil
	m.nFiles, m.nWarn = 0, 0
	var b strings.Builder
	for _, c := range engine.MigrationContexts(m.ctx, src, m.recursive) {
		bundle, err := engine.Export(src, c, opts)
		if err != nil {
			b.WriteString(warnStyle.Render("export error: "+err.Error()) + "\n")
			continue
		}
		u := migUnit{ctx: c, label: m.labelFor(c), plans: map[string]ir.WritePlan{}}
		if u.label != "" {
			b.WriteString(titleStyle.Render("▸ "+u.label) + "\n")
		}
		for _, id := range m.selectedTargets() {
			dst, _ := m.reg.Get(id)
			p := engine.Plan(dst, bundle, c, opts)
			u.plans[id] = p
			m.nFiles += len(p.Files)
			m.nWarn += len(p.Warnings)
			b.WriteString(plan.RenderDiff(p) + "\n")
		}
		m.units = append(m.units, u)
	}
	m.preview = b.String()
}

func (m *model) apply() {
	var b strings.Builder
	var totW, totB, totS int
	for _, u := range m.units {
		if u.label != "" {
			b.WriteString(selectedStyle.Render("▸ "+u.label) + "\n")
		}
		for _, id := range m.selectedTargets() {
			dst, _ := m.reg.Get(id)
			res := dst.Apply(u.plans[id], adapter.ApplyOptions{Backup: true, OnConflict: m.conflictAction()})
			totW += len(res.Written)
			totB += len(res.BackedUp)
			totS += len(res.Skipped)
			fmt.Fprintf(&b, "  %s %-12s %d written, %d backed up, %d skipped\n", okMark, id, len(res.Written), len(res.BackedUp), len(res.Skipped))
			for _, e := range res.Errors {
				fmt.Fprintf(&b, "    %s\n", warnStyle.Render("error: "+e.Error()))
			}
			for _, w := range u.plans[id].Warnings {
				if w.Category == "project-state" && strings.Contains(strings.ToLower(w.Reason), "trust") {
					fmt.Fprintf(&b, "    → grant trust for %s before first run\n", dst.Meta().DisplayName)
				}
			}
		}
	}
	fmt.Fprintf(&b, "\n%s\n", selectedStyle.Render(fmt.Sprintf("Total: %d written · %d backed up · %d skipped", totW, totB, totS)))
	m.applied = b.String()
}

func (m model) View() string {
	if m.quit {
		return ""
	}
	footer := dimStyle.Render("↑/↓ move · space toggle · enter next · esc back · q quit")
	switch m.step {
	case stepFrom:
		return m.viewFrom() + "\n" + footer
	case stepTo:
		return m.viewList(fmt.Sprintf("From %s → select target(s):", m.from), m.fromID, func(id string) string {
			a, _ := m.reg.Get(id)
			label := a.Meta().DisplayName
			if id == m.from {
				return dimStyle.Render(label + " (source)")
			}
			return label
		}, true) + "\n" + footer
	case stepCats:
		return m.viewList("Select categories to migrate:", engine.Categories, func(c string) string { return c }, true) + "\n" + footer
	case stepOptions:
		return m.viewOptions() + "\n" + footer
	case stepPreview:
		summary := fmt.Sprintf("Review: %d file(s) across %d target(s)", m.nFiles, len(m.selectedTargets()))
		if m.nWarn > 0 {
			summary += warnStyle.Render(fmt.Sprintf(" · %d warning(s)", m.nWarn))
		}
		body := m.preview
		if strings.TrimSpace(body) == "" {
			body = dimStyle.Render("(nothing to migrate for the selected categories)")
		}
		return titleStyle.Render("Plan preview") + "\n" + summary + "\n\n" + body + "\n" +
			selectedStyle.Render("enter = apply") + dimStyle.Render("  (.bak backups kept)  ·  esc = back  ·  q = cancel")
	case stepApplied:
		return titleStyle.Render("Migration applied") + "\n\n" + m.applied + "\n" + dimStyle.Render("enter/q to exit")
	}
	return ""
}

func (m model) viewFrom() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("agensync — migrate your AI coding agent config") + "\n")
	b.WriteString(dimStyle.Render("project: "+m.ctx.ProjectPath) + "\n")
	if det := m.detectedIDs(); len(det) > 0 {
		b.WriteString(selectedStyle.Render("detected here: ") + strings.Join(det, ", ") + "\n\n")
	} else {
		b.WriteString(dimStyle.Render("no tools detected here — pick a source anyway") + "\n\n")
	}
	b.WriteString(titleStyle.Render("Select the From tool:") + "\n\n")
	for i, id := range m.fromID {
		a, _ := m.reg.Get(id)
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}
		tag := ""
		if a.Detect(m.ctx).Present {
			tag = selectedStyle.Render(" ●")
		}
		b.WriteString(cursor + a.Meta().DisplayName + tag + "\n")
	}
	return b.String()
}

func (m model) viewOptions() string {
	opts := []struct {
		on   bool
		text string
	}{
		{m.overwrite, "Overwrite existing files  " + dimStyle.Render("(.bak backups always kept; off = skip existing)")},
		{m.recursive, "Recurse into subdirectories  " + dimStyle.Render("(monorepo: migrate every nested project in place)")},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Options:") + "\n\n")
	for i, o := range opts {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}
		checked := " "
		if o.on {
			checked = selectedStyle.Render("x")
		}
		b.WriteString(cursor + "[" + checked + "] " + o.text + "\n")
	}
	return b.String()
}

func (m model) viewList(title string, items []string, label func(string) string, multi bool) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title) + "\n\n")
	for i, it := range items {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}
		mark := ""
		if multi {
			checked := " "
			if (m.step == stepTo && m.toSel[it]) || (m.step == stepCats && m.catSel[it]) {
				checked = selectedStyle.Render("x")
			}
			mark = "[" + checked + "] "
		}
		b.WriteString(cursor + mark + label(it) + "\n")
	}
	return b.String()
}
