// Package modal is a modal key engine with no side effects: a key goes in
// with the current State and a snapshot of the host's Env, and a new State
// and the Steps the host should run come out. The engine never runs an
// operation; the host decides whether, when and how to run each Step.
//
// The engine owns everything that is about keys rather than about the
// application: modes, key-sequence prefixes and their fallback, counts,
// capture rows, key classes, and macro recording. Everything else is a
// Request the host executes.
package modal

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Args are a request's arguments.
type Args map[string]any

// Request names one host operation. The engine only carries it.
type Request struct {
	Op   string `json:"op"`
	Args Args   `json:"args,omitempty"`
}

// Engine operations are rows the engine interprets itself; they never
// reach the host as a Step.
const (
	OpMode   = "modal.mode"   // {mode}: switch the mode
	OpRecord = "modal.record" // start recording into the next key's register, or stop
	OpReplay = "modal.replay" // replay the next key's register
)

// Row is one binding: in Mode, the Keys (space-separated for a sequence)
// or any key of Class answer with Do. When names a guard the host answers
// in Env. A capture row is consulted outer layer first, before the walk.
type Row struct {
	Keys    string  `json:"keys,omitempty"`
	Class   string  `json:"class,omitempty"` // "text": one printable key, passed as args.text
	Mode    string  `json:"mode"`            // "*" answers in every mode
	When    string  `json:"when,omitempty"`
	Capture bool    `json:"capture,omitempty"`
	Do      Request `json:"do"`
}

// Keymap is static data: the rows of each layer and the modes in which
// digits build a count.
type Keymap struct {
	Layers   map[string][]Row `json:"layers"`
	Counting []string         `json:"counting,omitempty"`
}

// Env is the host's answer to "where are we": the layers from the focus
// outward, and its guards. The engine reads it and never changes it.
type Env struct {
	Walk  []string
	Guard func(name string) bool
}

// State is everything the engine remembers between keys. It is a value:
// Feed never changes the State it was given.
type State struct {
	Mode      string
	Prefix    string   // keys of an unfinished sequence, "g" or "t"
	Fallback  *Step    // what the prefix runs if the sequence goes no further
	Count     int      // digits typed before a command
	Await     string   // "record" or "replay": the next key names a register
	Recording string   // the register being recorded, "" when not
	Tape      []string // keys recorded so far
	Macros    map[string][]string
	Replaying int // set by the host while it feeds a replay back; such keys are not recorded
}

// Step is one thing for the host to do, in order. Either Request is set,
// or Replay is: keys the host feeds back through the engine one at a time
// after running everything before, so each sees the Env the earlier steps
// left behind.
type Step struct {
	Request Request  `json:"request,omitempty"`
	Layer   string   `json:"layer,omitempty"`
	Keys    string   `json:"keys,omitempty"` // the keys that produced it
	Replay  []string `json:"replay,omitempty"`
}

// Result is what one key produced.
type Result struct {
	State   State
	Steps   []Step
	Claimed bool   // some row, count or engine state took the key
	Waiting bool   // a sequence is pending; the host should start a timer
	Note    string // what the engine did itself, when it produced no step
}

// Feed resolves one key. It is pure: same State, Env answers and key, same
// Result.
func (km Keymap) Feed(st State, env Env, key string) Result {
	st = st.clone()
	if st.Recording != "" && st.Replaying == 0 {
		st.Tape = append(st.Tape, key)
	}

	if st.Await != "" {
		return km.register(st, key)
	}

	if km.counting(st.Mode) && st.Prefix == "" && isDigit(key) && (key != "0" || st.Count > 0) {
		st.Count = st.Count*10 + int(key[0]-'0')
		return Result{State: st, Claimed: true, Note: fmt.Sprintf("count %d", st.Count)}
	}

	seq := key
	if st.Prefix != "" {
		seq = st.Prefix + " " + key
	}

	// Capture pass: outer layer first; the first exact row wins.
	for i := len(env.Walk) - 1; i >= 0; i-- {
		layer := env.Walk[i]
		for _, row := range km.Layers[layer] {
			if row.Capture && row.Keys == seq && km.live(st, env, row) {
				return km.apply(st, layer, row, seq, nil)
			}
		}
	}

	// Bubbling pass: inner layer first. A sequence that extends the keys at
	// any layer waits, with the innermost exact row as its fallback;
	// otherwise the innermost hit runs. A class row ends the scan, so outer
	// sequences never steal a text key.
	var (
		hit, exact       *Row
		hitLayer, exLyr  string
		longer, classHit bool
	)
scan:
	for _, layer := range env.Walk {
		for i := range km.Layers[layer] {
			row := &km.Layers[layer][i]
			if row.Capture || !km.live(st, env, *row) {
				continue
			}
			switch {
			case row.Keys == seq:
				if exact == nil {
					exact, exLyr = row, layer
				}
				if hit == nil {
					hit, hitLayer = row, layer
				}
			case row.Keys != "" && strings.HasPrefix(row.Keys, seq+" "):
				longer = true
			case row.Class == "text" && st.Prefix == "" && hit == nil:
				if _, ok := TextOf(key); ok {
					hit, hitLayer, classHit = row, layer, true
					break scan
				}
			}
		}
	}

	if longer {
		// A deeper prefix with no exact row keeps the shorter one's fallback.
		if exact != nil {
			fb := km.apply(st, exLyr, *exact, seq, nil)
			st.Fallback = nil
			if len(fb.Steps) == 1 {
				st.Fallback = &fb.Steps[0]
			}
		} else if st.Prefix == "" {
			st.Fallback = nil
		}
		st.Prefix = seq
		return Result{State: st, Claimed: true, Waiting: true}
	}
	if hit != nil {
		st.Prefix, st.Fallback = "", nil
		var extra Args
		if classHit {
			text, _ := TextOf(key)
			extra = Args{"text": text}
		}
		return km.apply(st, hitLayer, *hit, seq, extra)
	}
	if st.Prefix != "" {
		// The sequence went no further: its fallback runs, then the key is
		// offered fresh, after the fallback has run.
		var steps []Step
		if st.Fallback != nil {
			steps = append(steps, *st.Fallback)
		}
		steps = append(steps, Step{Keys: key, Replay: []string{key}})
		st.Prefix, st.Fallback, st.Count = "", nil, 0
		if st.Recording != "" && st.Replaying == 0 {
			st.Tape = st.Tape[:len(st.Tape)-1] // the replayed key records itself again
		}
		return Result{State: st, Steps: steps, Claimed: true}
	}
	st.Count = 0
	return Result{State: st}
}

// Timeout ends a pending sequence: its fallback, if any, is the Step.
func (km Keymap) Timeout(st State) Result {
	if st.Prefix == "" {
		return Result{State: st}
	}
	st = st.clone()
	var steps []Step
	if st.Fallback != nil {
		steps = append(steps, *st.Fallback)
	}
	st.Prefix, st.Fallback, st.Count = "", nil, 0
	return Result{State: st, Steps: steps, Claimed: true}
}

// register consumes the key after q or @.
func (km Keymap) register(st State, reg string) Result {
	await := st.Await
	st.Await = ""
	switch await {
	case "record":
		st.Recording, st.Tape = reg, nil
		return Result{State: st, Claimed: true, Note: "recording @" + reg}
	case "replay":
		keys := st.Macros[reg]
		times := max(st.Count, 1)
		st.Count = 0
		var replay []string
		for range times {
			replay = append(replay, keys...)
		}
		if len(replay) == 0 {
			return Result{State: st, Claimed: true, Note: "@" + reg + " is empty"}
		}
		return Result{State: st, Steps: []Step{{Keys: "@" + reg, Replay: replay}}, Claimed: true}
	}
	return Result{State: st, Claimed: true}
}

// apply turns a matched row into engine state changes or a host Step.
func (km Keymap) apply(st State, layer string, row Row, seq string, extra Args) Result {
	switch row.Do.Op {
	case OpMode:
		st.Mode, _ = row.Do.Args["mode"].(string)
		st.Count = 0
		return Result{State: st, Claimed: true, Note: "mode " + st.Mode}
	case OpRecord:
		if st.Recording != "" {
			// Stop: the tape holds the keys of this row too; drop them.
			n := len(strings.Fields(seq))
			tape := st.Tape[:max(len(st.Tape)-n, 0)]
			st.Macros[st.Recording] = append([]string(nil), tape...)
			note := fmt.Sprintf("recorded @%s: %s", st.Recording, strings.Join(tape, " "))
			st.Recording, st.Tape = "", nil
			return Result{State: st, Claimed: true, Note: note}
		}
		st.Await = "record"
		return Result{State: st, Claimed: true, Note: "record: next key names the register"}
	case OpReplay:
		st.Await = "replay" // the count stays for register()
		return Result{State: st, Claimed: true, Note: "replay: next key names the register"}
	}
	q := Request{Op: row.Do.Op}
	if len(row.Do.Args) > 0 || len(extra) > 0 || st.Count > 0 {
		q.Args = Args{}
		for k, v := range row.Do.Args {
			q.Args[k] = v
		}
		for k, v := range extra {
			q.Args[k] = v
		}
		if st.Count > 0 {
			q.Args["count"] = st.Count
		}
	}
	st.Count = 0
	return Result{State: st, Steps: []Step{{Request: q, Layer: layer, Keys: seq}}, Claimed: true}
}

func (km Keymap) live(st State, env Env, row Row) bool {
	if row.Mode != st.Mode && row.Mode != "*" {
		return false
	}
	return row.When == "" || (env.Guard != nil && env.Guard(row.When))
}

func (km Keymap) counting(mode string) bool {
	for _, m := range km.Counting {
		if m == mode {
			return true
		}
	}
	return false
}

// clone copies the parts of a State that Feed may change in place.
func (st State) clone() State {
	st.Tape = append([]string(nil), st.Tape...)
	macros := make(map[string][]string, len(st.Macros))
	for k, v := range st.Macros {
		macros[k] = v
	}
	st.Macros = macros
	if st.Fallback != nil {
		fb := *st.Fallback
		st.Fallback = &fb
	}
	return st
}

// TextOf reports the text a key types: one printable character, or space.
func TextOf(key string) (string, bool) {
	if key == "space" {
		return " ", true
	}
	if utf8.RuneCountInString(key) == 1 && key[0] >= ' ' && key != "\x7f" {
		return key, true
	}
	return "", false
}

func isDigit(key string) bool { return len(key) == 1 && key[0] >= '0' && key[0] <= '9' }
