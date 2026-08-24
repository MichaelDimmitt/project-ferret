// Command ferret measures whether this repo can be built and run on this
// machine, and says so in ten seconds.
//
// This is stage one. Stage zero is ./bootstrap/run.sh, which is POSIX sh and
// measures the machine before any toolchain exists.
//
// Not implemented yet — M1 lands the runner. See docs/plan/PLAN.md.
//
// Go, standard library only. No third-party modules, ever:
// docs/design/LANGUAGE_CHOICE.md.
package main

import (
	"fmt"
	"os"
)

// Exit codes, per docs/design/ARCHITECTURE.md §11. Shared with the shell
// entry points, so they must not drift.
const (
	exitGo       = 0 // checked, and it passed
	exitNoGo     = 1 // a blocker was found
	exitUnknown  = 2 // unknowns present — never collapsed into 0
	exitCannot   = 3 // could not determine: no baseline, no runner, not implemented
)

func main() {
	fmt.Fprintln(os.Stderr, "ferret: not implemented")
	fmt.Fprintln(os.Stderr, "ferret: next milestone is M1 — see docs/plan/PLAN.md")
	// Exit 3, never 0. An unimplemented sweep has verified nothing, and 0
	// means GO.
	os.Exit(exitCannot)
}
