package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ofam.dev/o-modes/modal"
)

//go:embed keymap.json
var keymapJSON []byte

func loadKeymap() (modal.Keymap, error) {
	var km modal.Keymap
	err := json.Unmarshal(keymapJSON, &km)
	return km, err
}

// App wires the pure engine to the sheet. The Machine's Run hook is where
// the app decides what to do with each step: here it intercepts
// app.preview, and otherwise hands the step to the sheet.
type App struct {
	Sheet   *Sheet
	Machine *modal.Machine
	Log     []string // what each key produced, newest last

	// Preview mode: keys resolve against a shadow state and run nothing.
	Preview     bool
	previewKeys []string
}

func NewApp(s *Sheet) (*App, error) {
	km, err := loadKeymap()
	if err != nil {
		return nil, err
	}
	a := &App{Sheet: s}
	a.Machine = &modal.Machine{
		Keymap: km,
		State:  modal.State{Mode: "normal"},
		Env:    s.Env,
		Run:    a.run,
		Trace:  a.trace,
	}
	return a, nil
}

func (a *App) setMode(mode string) { a.Machine.State.Mode = mode }

func (a *App) run(step modal.Step) error {
	if step.Request.Op == "app.preview" {
		a.Preview, a.previewKeys = true, nil
		a.Sheet.Status = "preview: keys run on a copy; ctrl+e to leave"
		return nil
	}
	return a.Sheet.Run(step, a.setMode)
}

// Press is the app's key handler. In preview it never reaches the Machine's
// Run: it asks the engine what the keys typed so far would do and shows it.
func (a *App) Press(key string) (modal.Result, error) {
	if a.Preview {
		if key == "ctrl+e" || key == "esc" || key == "ctrl+c" {
			a.Preview, a.previewKeys = false, nil
			a.Sheet.Status = "left preview"
			return modal.Result{Claimed: true}, nil
		}
		a.previewKeys = append(a.previewKeys, key)
		return modal.Result{Claimed: true}, nil
	}
	a.Sheet.Status = ""
	return a.Machine.Press(key)
}

// Sim is the outcome of running keys on a fork of the app.
type Sim struct {
	Keys  string
	Ran   []modal.Step // the steps that ran on the fork, in order
	Sheet *Sheet       // the fork after them
	State modal.State  // the fork's engine state after them
	Err   error
}

// Simulate runs keys on a fork: a copy of the sheet and of the engine state,
// driven by its own Machine, so replays and confirms behave exactly as they
// would for real, each key seeing what the steps before it did to the copy.
// The real sheet and machine are untouched.
func (a *App) Simulate(keys []string) Sim {
	fork := a.Sheet.Fork()
	sim := Sim{Keys: strings.Join(keys, " "), Sheet: fork}
	m := &modal.Machine{Keymap: a.Machine.Keymap, State: a.Machine.State, Env: fork.Env}
	m.Run = func(step modal.Step) error {
		sim.Ran = append(sim.Ran, step)
		return fork.Run(step, func(mode string) { m.State.Mode = mode })
	}
	for _, k := range keys {
		if _, err := m.Press(k); err != nil {
			sim.Err = fmt.Errorf("%s: %w", k, err)
			break
		}
	}
	sim.State = m.State
	return sim
}

// PreviewSim is the preview keys typed so far, simulated.
func (a *App) PreviewSim() Sim { return a.Simulate(a.previewKeys) }

func (a *App) trace(key string, res modal.Result) {
	indent := strings.Repeat("  ↳ ", min(res.State.Replaying, 3))
	a.Log = append(a.Log, fmt.Sprintf("%s%-8s → %s", indent, key, Describe(res)))
	if len(a.Log) > 200 {
		a.Log = a.Log[len(a.Log)-200:]
	}
}

// Describe prints a Result the way the log and --keys show it.
func Describe(res modal.Result) string {
	var parts []string
	for _, s := range res.Steps {
		parts = append(parts, DescribeStep(s))
	}
	switch {
	case len(parts) > 0:
	case res.Waiting:
		parts = append(parts, fmt.Sprintf("wait %q", res.State.Prefix))
	case res.Claimed:
		parts = append(parts, "(engine: "+res.Note+")")
	default:
		parts = append(parts, "unclaimed")
	}
	return strings.Join(parts, "; ")
}

func DescribeStep(s modal.Step) string {
	if s.Replay != nil {
		return fmt.Sprintf("replay %v", s.Replay)
	}
	out := s.Request.Op
	if len(s.Request.Args) > 0 {
		keys := make([]string, 0, len(s.Request.Args))
		for k := range s.Request.Args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var kv []string
		for _, k := range keys {
			kv = append(kv, fmt.Sprintf("%s=%v", k, s.Request.Args[k]))
		}
		out += " {" + strings.Join(kv, " ") + "}"
	}
	return out + " @" + s.Layer
}
