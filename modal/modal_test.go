package modal

import (
	"reflect"
	"testing"
)

func testKeymap() Keymap {
	return Keymap{
		Counting: []string{"normal"},
		Layers: map[string][]Row{
			"cell": {
				{Mode: "normal", Keys: "j", Do: Request{Op: "move", Args: Args{"dir": "down"}}},
				{Mode: "normal", Keys: "y", Do: Request{Op: "cell.yank"}},
				{Mode: "normal", Keys: "i", Do: Request{Op: OpMode, Args: Args{"mode": "insert"}}},
				{Mode: "insert", Class: "text", Do: Request{Op: "edit.insert"}},
				{Mode: "insert", Keys: "esc", Do: Request{Op: OpMode, Args: Args{"mode": "normal"}}},
			},
			"row": {
				{Mode: "normal", Keys: "d d", When: "many", Do: Request{Op: "row.delete"}},
				{Mode: "normal", Keys: "y y", Do: Request{Op: "row.duplicate"}},
			},
			"app": {
				{Mode: "normal", Keys: "g g", Do: Request{Op: "top"}},
				{Mode: "normal", Keys: "q", Do: Request{Op: OpRecord}},
				{Mode: "normal", Keys: "@", Do: Request{Op: OpReplay}},
				{Mode: "*", Keys: "ctrl+c", Capture: true, Do: Request{Op: "quit"}},
			},
		},
	}
}

func env(guards ...string) Env {
	set := map[string]bool{}
	for _, g := range guards {
		set[g] = true
	}
	return Env{Walk: []string{"cell", "row", "app"}, Guard: func(n string) bool { return set[n] }}
}

// feed presses keys in order without running anything and collects steps.
func feed(t *testing.T, km Keymap, st State, e Env, keys ...string) (State, []Step) {
	t.Helper()
	var steps []Step
	for _, k := range keys {
		res := km.Feed(st, e, k)
		st = res.State
		steps = append(steps, res.Steps...)
	}
	return st, steps
}

func ops(steps []Step) []string {
	var out []string
	for _, s := range steps {
		if s.Replay != nil {
			out = append(out, "replay:"+s.Keys)
			continue
		}
		out = append(out, s.Request.Op)
	}
	return out
}

func TestSequenceWaitsThenRuns(t *testing.T) {
	km := testKeymap()
	res := km.Feed(State{Mode: "normal"}, env("many"), "d")
	if !res.Waiting || len(res.Steps) != 0 || res.State.Prefix != "d" {
		t.Fatalf("d: %+v", res)
	}
	_, steps := feed(t, km, res.State, env("many"), "d")
	if got := ops(steps); !reflect.DeepEqual(got, []string{"row.delete"}) {
		t.Fatalf("d d: %v", got)
	}
	if steps[0].Layer != "row" || steps[0].Keys != "d d" {
		t.Fatalf("step: %+v", steps[0])
	}
}

func TestGuardHidesRow(t *testing.T) {
	res := testKeymap().Feed(State{Mode: "normal"}, env(), "d")
	if res.Claimed || res.Waiting {
		t.Fatalf("d without the guard must be unclaimed: %+v", res)
	}
}

func TestPrefixFallbackOnTimeout(t *testing.T) {
	km := testKeymap()
	st, _ := feed(t, km, State{Mode: "normal"}, env(), "y")
	if st.Prefix != "y" || st.Fallback == nil {
		t.Fatalf("y waits with a fallback: %+v", st)
	}
	res := km.Timeout(st)
	if got := ops(res.Steps); !reflect.DeepEqual(got, []string{"cell.yank"}) || res.State.Prefix != "" {
		t.Fatalf("timeout: %v %+v", got, res.State)
	}
}

func TestPrefixMismatchRunsFallbackThenReplaysKey(t *testing.T) {
	km := testKeymap()
	_, steps := feed(t, km, State{Mode: "normal"}, env(), "y", "j")
	if got := ops(steps); !reflect.DeepEqual(got, []string{"cell.yank", "replay:j"}) {
		t.Fatalf("y j: %v", got)
	}
}

func TestCountRidesOnTheRequest(t *testing.T) {
	_, steps := feed(t, testKeymap(), State{Mode: "normal"}, env(), "1", "2", "j")
	if len(steps) != 1 || steps[0].Request.Args["count"] != 12 {
		t.Fatalf("12j: %+v", steps)
	}
}

func TestModesAreEngineState(t *testing.T) {
	km := testKeymap()
	st, steps := feed(t, km, State{Mode: "normal"}, env(), "i", "h", "space", "esc")
	if st.Mode != "normal" {
		t.Fatalf("mode %q", st.Mode)
	}
	if got := ops(steps); !reflect.DeepEqual(got, []string{"edit.insert", "edit.insert"}) {
		t.Fatalf("steps %v", got)
	}
	if steps[1].Request.Args["text"] != " " {
		t.Fatalf("space types a space: %+v", steps[1])
	}
}

func TestClassRowStopsOuterSequences(t *testing.T) {
	// In insert mode "g" is text, never the start of the app's "g g".
	_, steps := feed(t, testKeymap(), State{Mode: "insert"}, env(), "g")
	if got := ops(steps); !reflect.DeepEqual(got, []string{"edit.insert"}) {
		t.Fatalf("%v", got)
	}
}

func TestCaptureBeatsInnerRows(t *testing.T) {
	_, steps := feed(t, testKeymap(), State{Mode: "insert"}, env(), "ctrl+c")
	if got := ops(steps); !reflect.DeepEqual(got, []string{"quit"}) {
		t.Fatalf("%v", got)
	}
}

func TestMacroRecordAndReplay(t *testing.T) {
	km := testKeymap()
	st, steps := feed(t, km, State{Mode: "normal"}, env(), "q", "a", "j", "j", "q")
	if !reflect.DeepEqual(st.Macros["a"], []string{"j", "j"}) || st.Recording != "" {
		t.Fatalf("recorded %v (recording %q)", st.Macros["a"], st.Recording)
	}
	if len(steps) != 2 {
		t.Fatalf("keys still run while recording: %v", ops(steps))
	}
	_, steps = feed(t, km, st, env(), "2", "@", "a")
	if len(steps) != 1 || !reflect.DeepEqual(steps[0].Replay, []string{"j", "j", "j", "j"}) {
		t.Fatalf("2@a: %+v", steps)
	}
}

func TestFeedNeverChangesItsInput(t *testing.T) {
	km := testKeymap()
	in := State{Mode: "normal", Macros: map[string][]string{"a": {"j"}}}
	st, _ := feed(t, km, in, env(), "q", "a")
	feed(t, km, st, env(), "j", "q")
	if !reflect.DeepEqual(in.Macros, map[string][]string{"a": {"j"}}) || in.Mode != "normal" {
		t.Fatalf("input state changed: %+v", in)
	}
	again, _ := feed(t, km, in, env(), "q", "a")
	if !reflect.DeepEqual(st, again) {
		t.Fatalf("same input, different result:\n%+v\n%+v", st, again)
	}
}

func TestMachineReplayReadsEnvBetweenKeys(t *testing.T) {
	// Each replayed key sees the Env the step before it left. With three
	// rows, recording "d d" deletes one; 2@a deletes the next, then finds
	// the guard false with one row left and does nothing.
	rows := 3
	var ran []string
	m := &Machine{
		Keymap: testKeymap(),
		State:  State{Mode: "normal"},
		Env:    func() Env { return env(map[bool]string{true: "many"}[rows > 1]) },
		Run: func(s Step) error {
			ran = append(ran, s.Request.Op)
			if s.Request.Op == "row.delete" {
				rows--
			}
			return nil
		},
	}
	for _, k := range []string{"q", "a", "d", "d", "q", "2", "@", "a"} {
		if _, err := m.Press(k); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(ran, []string{"row.delete", "row.delete"}) || rows != 1 {
		t.Fatalf("ran %v, rows %d", ran, rows)
	}
}

func TestPreviewRunsNothing(t *testing.T) {
	ran := 0
	m := &Machine{
		Keymap: testKeymap(),
		State:  State{Mode: "normal"},
		Env:    func() Env { return env("many") },
		Run:    func(Step) error { ran++; return nil },
	}
	steps := m.Preview([]string{"3", "d", "d"})
	if ran != 0 || len(steps) != 1 || steps[0].Request.Op != "row.delete" || steps[0].Request.Args["count"] != 3 {
		t.Fatalf("ran %d, steps %+v", ran, steps)
	}
	if m.State.Prefix != "" || m.State.Count != 0 {
		t.Fatalf("preview moved the machine: %+v", m.State)
	}
}
