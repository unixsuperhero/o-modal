package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Timeout is how long a key sequence waits for its next key.
const Timeout = 600 * time.Millisecond

type model struct {
	app *App
	gen int // bumps on every key, so a stale timer does nothing
	err error
}

type expireMsg struct{ gen int }

func newModel(app *App) model { return model{app: app} }

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.gen++
		res, err := m.app.Press(msg.String())
		m.err = err
		if m.app.Sheet.Quit {
			return m, tea.Quit
		}
		if res.Waiting {
			gen := m.gen
			return m, tea.Tick(Timeout, func(time.Time) tea.Msg { return expireMsg{gen} })
		}
	case expireMsg:
		if msg.gen == m.gen {
			m.err = m.app.Machine.Expire()
		}
	}
	return m, nil
}

var (
	cursorStyle  = lipgloss.NewStyle().Reverse(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
	headStyle    = lipgloss.NewStyle().Bold(true)
	previewStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

func (m model) View() tea.View {
	a, s, st := m.app, m.app.Sheet, m.app.Machine.State
	var b strings.Builder
	b.WriteString(s.Table(func(t string) string { return cursorStyle.Render(t) }))
	b.WriteString("\n")

	status := []string{headStyle.Render(strings.ToUpper(st.Mode))}
	if st.Count > 0 {
		status = append(status, fmt.Sprint(st.Count))
	}
	if st.Prefix != "" {
		status = append(status, st.Prefix+"…")
	}
	if st.Recording != "" {
		status = append(status, "recording @"+st.Recording)
	}
	if st.Mode == "insert" {
		status = append(status, "edit: "+s.Edit+"▏")
	}
	if s.Status != "" {
		status = append(status, s.Status)
	}
	if m.err != nil {
		status = append(status, "error: "+m.err.Error())
	}
	b.WriteString(strings.Join(status, "  ") + "\n\n")

	if a.Preview {
		sim := a.PreviewSim()
		b.WriteString(previewStyle.Render("PREVIEW  keys: "+sim.Keys+"   (a copy; nothing here is real)") + "\n")
		if len(sim.Ran) > 0 {
			table := sim.Sheet.Table(func(t string) string { return cursorStyle.Render(t) })
			b.WriteString(previewStyle.Render(strings.TrimRight(table, "\n")) + "\n")
		}
		for _, step := range sim.Ran {
			b.WriteString(previewStyle.Render("  ran: "+DescribeStep(step)) + "\n")
		}
		line := "  mode " + sim.State.Mode
		if sim.State.Prefix != "" {
			line += "  waiting " + sim.State.Prefix + "…"
		}
		if sim.Sheet.Status != "" {
			line += "  " + sim.Sheet.Status
		}
		if sim.Err != nil {
			line += "  error: " + sim.Err.Error()
		}
		b.WriteString(previewStyle.Render(line) + "\n\n")
	}

	b.WriteString(dimStyle.Render("key → steps the engine returned") + "\n")
	log := a.Log[max(len(a.Log)-8, 0):]
	for _, l := range log {
		b.WriteString(dimStyle.Render("  "+l) + "\n")
	}
	b.WriteString(dimStyle.Render("\nhjkl gg G 0 $ · i edit · x y p · t u/l/t · dd yy o · u ctrl+r · q@ macros · ctrl+e preview · ctrl+s save · ctrl+c quit"))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}
