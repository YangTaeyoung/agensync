// Package tui implements the interactive Bubble Tea flow that wraps the same
// engine/plan pipeline the non-interactive CLI uses. All business logic stays
// in engine/plan; this package only collects choices and renders results.
package tui

import (
	"fmt"
	"io"
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
)

type step int

const (
	stepFrom step = iota
	stepTo
	stepCats
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

	preview string
	plans   map[string]ir.WritePlan
	applied string
	quit    bool
}

func newModel(ctx ir.Context, out io.Writer) model {
	reg := all.Default()
	return model{
		reg:    reg,
		ctx:    ctx,
		out:    out,
		fromID: detectedFirst(reg, ctx),
		toSel:  map[string]bool{},
		catSel: allCats(),
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
	case "enter":
		return m.advance()
	}
	return m, nil
}

func (m model) maxCursor() int {
	switch m.step {
	case stepFrom:
		return len(m.fromID) - 1
	case stepTo:
		return len(m.fromID) - 1
	case stepCats:
		return len(engine.Categories) - 1
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
	}
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

func (m model) importOptions() adapter.ImportOptions {
	cats := map[string]bool{}
	for c, on := range m.catSel {
		if on {
			cats[c] = true
		}
	}
	return adapter.ImportOptions{Categories: cats, OnConflict: ir.ActionSkip}
}

func (m *model) computePreview() {
	src, _ := m.reg.Get(m.from)
	opts := m.importOptions()
	bundle, err := engine.Export(src, m.ctx, opts)
	var b strings.Builder
	if err != nil {
		b.WriteString("export error: " + err.Error() + "\n")
		m.preview = b.String()
		return
	}
	m.plans = map[string]ir.WritePlan{}
	for _, id := range m.selectedTargets() {
		dst, _ := m.reg.Get(id)
		p := engine.Plan(dst, bundle, m.ctx, opts)
		m.plans[id] = p
		b.WriteString(plan.RenderDiff(p))
		b.WriteString("\n")
	}
	m.preview = b.String()
}

func (m *model) apply() {
	var b strings.Builder
	for _, id := range m.selectedTargets() {
		dst, _ := m.reg.Get(id)
		res := dst.Apply(m.plans[id], adapter.ApplyOptions{Backup: true, OnConflict: ir.ActionSkip})
		fmt.Fprintf(&b, "%s: %d written, %d backed up\n", id, len(res.Written), len(res.BackedUp))
		for _, w := range m.plans[id].Warnings {
			if w.Category == "project-state" && strings.Contains(strings.ToLower(w.Reason), "trust") {
				fmt.Fprintf(&b, "  → grant trust for %s before first run\n", dst.Meta().DisplayName)
			}
		}
	}
	m.applied = b.String()
}

func (m model) View() string {
	if m.quit {
		return ""
	}
	switch m.step {
	case stepFrom:
		return m.viewList("Select the From tool (↑/↓ move, enter select, q quit):", m.fromID, func(id string) string {
			a, _ := m.reg.Get(id)
			tag := ""
			if a.Detect(m.ctx).Present {
				tag = selectedStyle.Render(" (detected)")
			}
			return a.Meta().DisplayName + tag
		}, false)
	case stepTo:
		return m.viewList(fmt.Sprintf("From %s → select target(s) (space toggle, enter continue):", m.from), m.fromID, func(id string) string {
			a, _ := m.reg.Get(id)
			label := a.Meta().DisplayName
			if id == m.from {
				return dimStyle.Render(label + " (source)")
			}
			return label
		}, true)
	case stepCats:
		return m.viewList("Select categories to migrate (space toggle, enter continue):", engine.Categories, func(c string) string { return c }, true)
	case stepPreview:
		return titleStyle.Render("Plan preview") + "\n\n" + m.preview + "\n" + dimStyle.Render("enter = apply (with .bak backups), q = cancel")
	case stepApplied:
		return titleStyle.Render("Migration applied") + "\n\n" + m.applied + "\n" + dimStyle.Render("enter/q to exit")
	}
	return ""
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
