// Command csv is a small CSV editor built on the modal engine, to show
// keys resolving to steps that the application then chooses to run.
//
//	csv FILE                      the TUI
//	csv --keys "j d d y" FILE     run keys headless, print each key's steps and the sheet
//	csv --explain "3 d d" FILE    run the keys on a copy and print what they did; the file is untouched
//
// In --keys, the word <wait> stands for the sequence timer firing.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func main() {
	keys := flag.String("keys", "", "space-separated keys to run headless")
	explain := flag.String("explain", "", "space-separated keys to run on a copy")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: csv [--keys KEYS | --explain KEYS] FILE.csv")
		os.Exit(2)
	}
	sheet, err := Load(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	app, err := NewApp(sheet)
	if err != nil {
		fail(err)
	}

	switch {
	case *explain != "":
		sim := app.Simulate(strings.Fields(*explain))
		for _, s := range sim.Ran {
			fmt.Println(DescribeStep(s))
		}
		fmt.Printf("\nmode %s (on a copy; %s is unchanged)\n%s", sim.State.Mode, sheet.Path, sim.Sheet.Table(func(s string) string { return "[" + s[1:len(s)-1] + "]" }))
		if sim.Sheet.Status != "" {
			fmt.Println(sim.Sheet.Status)
		}
		if sim.Err != nil {
			fail(sim.Err)
		}
	case *keys != "":
		if err := Script(app, strings.Fields(*keys)); err != nil {
			fail(err)
		}
		for _, l := range app.Log {
			fmt.Println(l)
		}
		fmt.Printf("\nmode %s\n%s", app.Machine.State.Mode, sheet.Table(func(s string) string { return "[" + s[1:len(s)-1] + "]" }))
		if sheet.Status != "" {
			fmt.Println(sheet.Status)
		}
	default:
		if _, err := tea.NewProgram(newModel(app)).Run(); err != nil {
			fail(err)
		}
	}
}

// Script presses keys in order; <wait> fires the sequence timer.
func Script(app *App, keys []string) error {
	for _, k := range keys {
		var err error
		if k == "<wait>" {
			err = app.Machine.Expire()
		} else {
			_, err = app.Press(k)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "csv:", err)
	os.Exit(1)
}
