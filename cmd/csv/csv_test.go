package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/o-modal/modal"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	src, err := os.ReadFile("testdata/people.csv")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func press(t *testing.T, a *App, keys string) {
	t.Helper()
	if err := Script(a, strings.Fields(keys)); err != nil {
		t.Fatal(err)
	}
}

func names(a *App) []string {
	var out []string
	for _, r := range a.Sheet.Rows {
		out = append(out, r[0])
	}
	return out
}

func TestDeleteAsksFirst(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "j d d")
	if a.Machine.State.Mode != "confirm" || len(a.Sheet.Rows) != 5 {
		t.Fatalf("d d must park the request and ask: mode %q rows %d", a.Machine.State.Mode, len(a.Sheet.Rows))
	}
	press(t, a, "y")
	if got := names(a); !reflect.DeepEqual(got, []string{"ann", "cy", "dee", "  eve "}) {
		t.Fatalf("rows %v", got)
	}
	press(t, a, "u")
	if len(a.Sheet.Rows) != 5 {
		t.Fatal("undo restores the row")
	}
}

func TestYankWaitsForYY(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "y <wait>")
	if a.Sheet.Yank != "ann" || len(a.Sheet.Rows) != 5 {
		t.Fatalf("y then timeout yanks the cell: %q", a.Sheet.Yank)
	}
	press(t, a, "y y")
	if got := names(a); got[1] != "ann" || len(got) != 6 {
		t.Fatalf("y y duplicates the row: %v", got)
	}
}

func TestEditCell(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "l i backspace backspace backspace backspace backspace backspace z space g enter")
	if got := a.Sheet.Rows[0][1]; got != "z g" {
		t.Fatalf("cell %q", got)
	}
	if a.Machine.State.Mode != "normal" {
		t.Fatalf("mode %q", a.Machine.State.Mode)
	}
}

func TestMacroUppercasesDown(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "q a t u j q 3 @ a")
	if got := names(a); !reflect.DeepEqual(got, []string{"ANN", "BO", "CY", "DEE", "  eve "}) {
		t.Fatalf("rows %v", got)
	}
}

func TestPreviewRunsOnACopy(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "ctrl+e 3 d d y")
	sim := a.PreviewSim()
	if sim.Keys != "3 d d y" || len(sim.Sheet.Rows) != 2 {
		t.Fatalf("the copy ran the delete and its confirm: %q rows %d", sim.Keys, len(sim.Sheet.Rows))
	}
	if len(a.Sheet.Rows) != 5 || a.Machine.State.Mode != "normal" {
		t.Fatal("preview changed the real sheet or engine")
	}
	press(t, a, "ctrl+e d d y")
	if len(a.Sheet.Rows) != 4 {
		t.Fatal("after leaving preview, keys run again")
	}
}

// The pure Preview resolves every replayed key against today's sheet and
// cannot see the confirm mode the app enters, so it predicts two deletes.
// The simulation runs the keys on a copy: the first d d asks, y deletes,
// and the second d d finds the many-rows guard false.
func TestSimulationIsExactWherePreviewGuesses(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "3 d d y")     // 2 rows left
	press(t, a, "q a d d y q") // records "d d y", deleting one: 1 left
	press(t, a, "u")           // back to 2
	deletes := func(steps []modal.Step) (n int) {
		for _, s := range steps {
			if s.Request.Op == "row.delete" {
				n++
			}
		}
		return n
	}
	guess := a.Machine.Preview([]string{"2", "@", "a"})
	sim := a.Simulate([]string{"2", "@", "a"})
	if deletes(guess) != 2 || deletes(sim.Ran) != 1 || len(sim.Sheet.Rows) != 1 {
		t.Fatalf("guess %d deletes, sim %d (rows %d)", deletes(guess), deletes(sim.Ran), len(sim.Sheet.Rows))
	}
	if len(a.Sheet.Rows) != 2 {
		t.Fatal("simulation changed the real sheet")
	}
}

func TestForkKeepsUndoApart(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "x u") // real undo stack now holds the cleared state in redo
	sim := a.Simulate([]string{"ctrl+r", "i", "z", "enter", "u", "u"})
	if sim.Err != nil {
		t.Fatal(sim.Err)
	}
	if a.Sheet.Rows[0][0] != "ann" {
		t.Fatalf("real cell %q", a.Sheet.Rows[0][0])
	}
	press(t, a, "ctrl+r")
	if a.Sheet.Rows[0][0] != "" {
		t.Fatalf("real redo was disturbed by the fork: %q", a.Sheet.Rows[0][0])
	}
}

func TestSimulatedSaveWritesNothing(t *testing.T) {
	a := newTestApp(t)
	before, _ := os.ReadFile(a.Sheet.Path)
	sim := a.Simulate([]string{"x", "ctrl+s"})
	after, _ := os.ReadFile(a.Sheet.Path)
	if string(before) != string(after) || sim.Sheet.Status != "would save "+a.Sheet.Path {
		t.Fatalf("status %q", sim.Sheet.Status)
	}
}

func TestLastRowIsNotDeletable(t *testing.T) {
	a := newTestApp(t)
	press(t, a, "4 d d y")
	if len(a.Sheet.Rows) != 1 {
		t.Fatalf("rows %d", len(a.Sheet.Rows))
	}
	press(t, a, "d d")
	if len(a.Sheet.Rows) != 1 || a.Machine.State.Mode != "normal" {
		t.Fatal("the many-rows guard hides d d on the last row")
	}
}
