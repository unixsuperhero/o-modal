<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-animated-dark.svg">
    <img alt="o-modes" src="assets/logo-animated-light.svg" width="360">
  </picture>
  <br>
  <a href="https://github.com/ofamtools/o-modes/tags"><img alt="Latest tag" src="https://img.shields.io/github/v/tag/ofamtools/o-modes"></a>
  <a href="https://pkg.go.dev/ofam.dev/o-modes"><img alt="Go Reference" src="https://pkg.go.dev/badge/ofam.dev/o-modes.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ofamtools/o-modes"></a>
</p>

<p align="center">A proof of concept: a modal key engine for Go. Keys in, steps out; the host decides what runs.</p>

The engine runs nothing. A key goes in with the current state and a snapshot of the host's environment; a new state and a list of steps come out. The host decides whether, when and how to run each step.

## Install

The engine is the `modal` package (standard library only); add it with the Go version in [go.mod](go.mod) or newer:

```sh
go get ofam.dev/o-modes/modal@latest
```

## Usage

```go
res := keymap.Feed(state, env, key) // pure: same inputs, same Result
state = res.State
for _, step := range res.Steps {
    run(step) // the host's choice
}
```

`Machine` wraps that loop for a host that runs steps itself. `cmd/csv` is a whole host built on it: see [Run it](#run-it).

## Layout

| Path | What |
|---|---|
| `modal/modal.go` | The engine (stdlib only): `Keymap`, `Row`, `State`, `Env`, `Step`, `Feed`, `Timeout` |
| `modal/machine.go` | Optional host loop: `Machine.Press` (Feed, then run), `Expire` (the sequence timer), `Preview` (resolve, run nothing) |
| `cmd/csv/keymap.json` | The CSV app's bindings, as data |
| `cmd/csv/sheet.go` | The application: `Sheet.Run(step)` is the only code that changes the sheet |
| `cmd/csv/app.go` | Wires the two; intercepts `app.preview` |
| `cmd/csv/tui.go` | Bubble Tea v2 view, the timer, and a log of what each key produced |

## What the engine owns

- **Modes:** rows are filtered by mode; `modal.mode` switches it.
- **Sequences:** `d d`, `g g`, `t u`. A prefix waits. On a timeout, its fallback runs (`y` alone yanks; `y y` duplicates). On a mismatch, the fallback runs and then the key is offered again (`y j`).
- **Counts:** digits in counting modes; the count rides on the request as `args.count`.
- **Capture rows:** consulted outer layer first (`ctrl+c` quits from any mode).
- **Key classes:** `text` takes any printable key and passes it as `args.text`; it ends the scan, so outer sequences never steal text.
- **Guards:** a row's `when` is answered by the host's `Env.Guard`.
- **Macros:** `q a … q` records, `@ a` replays.

Replays and flushed keys are returned as `Step.Replay`: keys the host feeds back one at a time *after* running the steps before them. Each replayed key then sees the environment the earlier steps left behind. That's the one part a pure resolver cannot do ahead of time.

## Run it

```sh
go run ./cmd/csv cmd/csv/testdata/people.csv                         # TUI
go run ./cmd/csv --keys "j d d y q a t u j q 3 @ a" cmd/csv/testdata/people.csv
go run ./cmd/csv --explain "j 2 d d y ctrl+s" cmd/csv/testdata/people.csv  # run on a copy
```

`--keys` prints each key's steps and the final sheet; the word `<wait>` fires the sequence timer.

## Preview, two ways

- **`Machine.Preview(keys)`** is pure. It resolves the keys against a copy of the engine state and today's `Env`, and runs nothing. It's cheap, but it guesses: replayed keys all see today's sheet, and it can't know the app will enter a mode such as `confirm`.
- **`App.Simulate(keys)`** forks the app (`Sheet.Fork`: rows, undo stacks, the parked request) and drives the copy with a second `Machine`, so every step really runs, on the copy. Replays and confirms come out exactly as they would for real. Effects outside the sheet are only described: the fork has `DryRun` set, so `ctrl+s` reports "would save …".

`--explain` and the TUI's preview (`ctrl+e`) use the simulation: the preview shows the copy's table, the steps that ran on it, and its mode.

## Tests

```sh
go test ./...
```
