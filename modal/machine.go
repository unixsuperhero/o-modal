package modal

import "errors"

// Machine is the host side of the loop: Press feeds a key through the
// engine and runs the Steps it returns. It is the one place keys turn into
// effects, and it is optional: a host can call Keymap.Feed itself to look
// at what a key would do without running anything.
type Machine struct {
	Keymap Keymap
	State  State
	Env    func() Env           // read fresh before every key
	Run    func(Step) error     // runs one host Step
	Trace  func(string, Result) // optional: sees every key and what it produced
}

// MaxReplay bounds nested replays, so a macro that replays itself stops.
const MaxReplay = 50

// ErrReplayDepth is returned when replays nest past MaxReplay.
var ErrReplayDepth = errors.New("modal: replay nested too deep")

// Press resolves one key and runs what it produced.
func (m *Machine) Press(key string) (Result, error) {
	res := m.Keymap.Feed(m.State, m.Env(), key)
	m.State = res.State
	if m.Trace != nil {
		m.Trace(key, res)
	}
	return res, m.run(res.Steps)
}

// Expire is the host's timer firing: a pending sequence runs its fallback.
func (m *Machine) Expire() error {
	res := m.Keymap.Timeout(m.State)
	m.State = res.State
	if m.Trace != nil && res.Claimed {
		m.Trace("<timeout>", res)
	}
	return m.run(res.Steps)
}

// Preview resolves keys against a copy of the state with the current Env
// and runs nothing: "what would these keys do here?". Steps that a real
// press would only learn after running earlier ones (a replay, a flushed
// key) are resolved in place, against the same Env.
func (m *Machine) Preview(keys []string) []Step {
	st := m.State
	env := m.Env()
	var out []Step
	var feed func(keys []string, depth int)
	feed = func(keys []string, depth int) {
		for _, k := range keys {
			res := m.Keymap.Feed(st, env, k)
			st = res.State
			for _, s := range res.Steps {
				if s.Replay != nil && depth < MaxReplay {
					st.Replaying++
					feed(s.Replay, depth+1)
					st.Replaying--
					continue
				}
				out = append(out, s)
			}
		}
	}
	feed(keys, 0)
	return out
}

func (m *Machine) run(steps []Step) error {
	for _, s := range steps {
		if s.Replay != nil {
			if err := m.replay(s.Replay); err != nil {
				return err
			}
			continue
		}
		if err := m.Run(s); err != nil {
			return err
		}
	}
	return nil
}

func (m *Machine) replay(keys []string) error {
	if m.State.Replaying >= MaxReplay {
		return ErrReplayDepth
	}
	m.State.Replaying++
	defer func() { m.State.Replaying-- }()
	for _, k := range keys {
		if _, err := m.Press(k); err != nil {
			return err
		}
	}
	return nil
}
