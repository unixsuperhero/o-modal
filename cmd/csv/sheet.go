package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"ofam.dev/o-modes/modal"
)

// Sheet is the application: a CSV grid and its cursor. It knows nothing
// about keys. Run is the one entry point, and it takes a Step the engine
// produced (or any other caller built: a script, a test, a socket).
type Sheet struct {
	Path   string
	Header []string
	Rows   [][]string
	R, C   int
	Yank   string

	Edit    string         // the cell editor's text while in insert mode
	Pending *modal.Request // a destructive request waiting for y/n
	Status  string
	Quit    bool
	DryRun  bool // a fork: effects outside the sheet (the file) are only described

	undo, redo [][][]string
}

func Load(path string) (*Sheet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%s: empty", path)
	}
	return &Sheet{Path: path, Header: recs[0], Rows: recs[1:]}, nil
}

// Env is the sheet's answer to the engine: the layers from the focused
// cell outward, and its guards.
func (s *Sheet) Env() modal.Env {
	return modal.Env{
		Walk: []string{"cell", "row", "sheet", "app"},
		Guard: func(name string) bool {
			switch name {
			case "many-rows":
				return len(s.Rows) > 1
			}
			return false
		},
	}
}

// Run performs one step. It may change the engine's mode through setMode:
// entering the cell editor and asking for confirmation are the
// application's decisions, not the keymap's.
func (s *Sheet) Run(step modal.Step, setMode func(string)) error {
	q := step.Request
	n := 1
	if c, ok := q.Args["count"].(int); ok && c > 0 {
		n = c
	}
	str := func(k string) string { v, _ := q.Args[k].(string); return v }

	switch q.Op {
	case "cursor.move":
		for range n {
			switch str("dir") {
			case "down":
				s.R = min(s.R+1, len(s.Rows)-1)
			case "up":
				s.R = max(s.R-1, 0)
			case "left":
				s.C = max(s.C-1, 0)
			case "right":
				s.C = min(s.C+1, len(s.Header)-1)
			case "row-start":
				s.C = 0
			case "row-end":
				s.C = len(s.Header) - 1
			}
		}
	case "cursor.line":
		if str("to") == "top" {
			s.R = 0
		} else {
			s.R = len(s.Rows) - 1
		}

	case "cell.edit":
		s.Edit = s.cell()
		setMode("insert")
	case "edit.insert":
		s.Edit += str("text")
	case "edit.backspace":
		if r := []rune(s.Edit); len(r) > 0 {
			s.Edit = string(r[:len(r)-1])
		}
	case "edit.commit":
		s.snapshot()
		s.setCell(s.Edit)
		setMode("normal")
	case "edit.cancel":
		setMode("normal")

	case "cell.clear":
		s.snapshot()
		s.setCell("")
	case "cell.yank":
		s.Yank = s.cell()
		s.Status = fmt.Sprintf("yanked %q", s.Yank)
	case "cell.paste":
		s.snapshot()
		s.setCell(s.Yank)
	case "cell.transform":
		fn := map[string]func(string) string{"upper": strings.ToUpper, "lower": strings.ToLower, "trim": strings.TrimSpace}[str("name")]
		if fn == nil {
			return fmt.Errorf("unknown transform %q", str("name"))
		}
		s.snapshot()
		s.setCell(fn(s.cell()))

	case "row.delete":
		if q.Args["confirmed"] != true {
			// The application intercepts its own destructive verb: it parks
			// the request and asks. The engine just sees a mode change.
			s.Pending = &q
			s.Status = fmt.Sprintf("delete %d row(s) from %q? y/n", n, s.Rows[s.R][0])
			setMode("confirm")
			return nil
		}
		s.snapshot()
		n = min(n, len(s.Rows)-s.R, len(s.Rows)-1)
		s.Rows = append(s.Rows[:s.R:s.R], s.Rows[s.R+n:]...)
		s.R = min(s.R, len(s.Rows)-1)
		s.Status = fmt.Sprintf("deleted %d row(s)", n)
	case "confirm.yes":
		q := *s.Pending
		s.Pending = nil
		setMode("normal")
		args := modal.Args{"confirmed": true}
		for k, v := range q.Args {
			args[k] = v
		}
		q.Args = args
		return s.Run(modal.Step{Request: q, Keys: step.Keys}, setMode)
	case "confirm.no":
		s.Pending = nil
		s.Status = "cancelled"
		setMode("normal")
	case "row.duplicate":
		s.snapshot()
		for range n {
			s.insertRow(append([]string(nil), s.Rows[s.R]...))
		}
	case "row.insertBelow":
		s.snapshot()
		s.insertRow(make([]string, len(s.Header)))
		s.R++

	case "sheet.undo":
		s.restore(&s.undo, &s.redo)
	case "sheet.redo":
		s.restore(&s.redo, &s.undo)
	case "sheet.save":
		return s.save()

	case "app.quit":
		s.Quit = true
	default:
		return fmt.Errorf("unknown operation %q", q.Op)
	}
	return nil
}

func (s *Sheet) cell() string { return s.Rows[s.R][s.C] }

func (s *Sheet) setCell(v string) { s.Rows[s.R][s.C] = v }

func (s *Sheet) insertRow(row []string) {
	at := s.R + 1
	s.Rows = append(s.Rows[:at], append([][]string{row}, s.Rows[at:]...)...)
}

func (s *Sheet) snapshot() {
	s.undo = append(s.undo, copyRows(s.Rows))
	s.redo = nil
}

func (s *Sheet) restore(from, to *[][][]string) {
	if len(*from) == 0 {
		s.Status = "nothing to undo"
		return
	}
	*to = append(*to, copyRows(s.Rows))
	s.Rows = (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	s.R = min(s.R, len(s.Rows)-1)
}

// Fork copies everything the sheet owns, so a run on the copy cannot reach
// the original: rows, the undo stacks (restore makes a stack entry the live
// rows, which edits then change in place), and the parked request.
func (s *Sheet) Fork() *Sheet {
	c := *s
	c.Header = append([]string(nil), s.Header...)
	c.Rows = copyRows(s.Rows)
	c.undo, c.redo = copyStack(s.undo), copyStack(s.redo)
	if s.Pending != nil {
		p := *s.Pending
		c.Pending = &p
	}
	c.DryRun = true
	return &c
}

func copyStack(stack [][][]string) [][][]string {
	out := make([][][]string, len(stack))
	for i, rows := range stack {
		out[i] = copyRows(rows)
	}
	return out
}

func copyRows(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = append([]string(nil), r...)
	}
	return out
}

func (s *Sheet) save() error {
	if s.DryRun {
		s.Status = "would save " + s.Path
		return nil
	}
	f, err := os.Create(s.Path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write(s.Header)
	w.WriteAll(s.Rows)
	if err := f.Close(); err != nil {
		return err
	}
	s.Status = "saved " + s.Path
	return w.Error()
}

// Table renders the sheet as plain text, the cursor cell in [brackets].
func (s *Sheet) Table(cursor func(string) string) string {
	widths := make([]int, len(s.Header))
	for c, h := range s.Header {
		widths[c] = len(h)
		for _, r := range s.Rows {
			widths[c] = max(widths[c], len(r[c]))
		}
	}
	var b strings.Builder
	line := func(cells []string, row int) {
		for c, v := range cells {
			cellText := fmt.Sprintf(" %-*s ", widths[c], v)
			if row == s.R && c == s.C {
				cellText = cursor(cellText)
			}
			b.WriteString(cellText)
			b.WriteString("│")
		}
		b.WriteString("\n")
	}
	line(s.Header, -1)
	for c := range s.Header {
		b.WriteString(strings.Repeat("─", widths[c]+2) + "┼")
	}
	b.WriteString("\n")
	for i, r := range s.Rows {
		line(r, i)
	}
	return b.String()
}
