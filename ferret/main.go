// Command ferret measures whether this repo can be built and run on this
// machine, and says so in ten seconds.
//
// This is stage one. Stage zero is ./bootstrap/run.sh, which is POSIX sh and
// measures the machine before any toolchain exists.
//
// As of M1 this is the CAPTURE half only: it loads the manifest, runs the
// probes, and writes evidence.json. It does not decide anything. The verdict
// engine (M2) and the renderer (M4) are separate, and the separation is the
// design -- see docs/design/ARCHITECTURE.md §1.
//
// Go, standard library only. No third-party modules, ever:
// docs/design/LANGUAGE_CHOICE.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// version is stamped into evidence.json so a record can be traced to the code
// that produced it.
const version = "0.1.0-dev"

// Exit codes, per docs/design/ARCHITECTURE.md §11. Shared with the shell
// entry points, so they must not drift.
const (
	exitGo      = 0 // checked, and it passed
	exitNoGo    = 1 // a blocker was found
	exitUnknown = 2 // unknowns present — never collapsed into 0
	exitCannot  = 3 // could not determine: no baseline, no runner, bad manifest
)

func main() {
	var (
		manifestDir = flag.String("manifest", "manifest", "directory of manifest JSON files")
		outPath     = flag.String("out", ".ferret/evidence.json", "where to write evidence")
		workDir     = flag.String("dir", ".", "directory to run probes in")
		redactFlag  = flag.String("redact", "", "run-wide redaction: identity, secret, or empty")
		rawDir      = flag.String("raw", ".ferret/raw", "untruncated probe output, or empty to skip")
		validate    = flag.Bool("validate", false, "validate the manifest and exit; run no probes")
		verdictOnly = flag.Bool("verdict", false,
			"skip probing: read an existing evidence.json and re-decide from it")
	)
	flag.Parse()

	code, err := run(*manifestDir, *outPath, *workDir, *rawDir,
		Redact(*redactFlag), *validate, *verdictOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ferret: %v\n", err)
		os.Exit(exitCannot)
	}
	os.Exit(code)
}

func run(manifestDir, outPath, workDir, rawDir string, redact Redact, validateOnly, verdictOnly bool) (int, error) {
	switch redact {
	case RedactNone, RedactIdentity, RedactSecret:
	default:
		return exitCannot, fmt.Errorf("unknown -redact %q (want: identity, secret, or empty)", redact)
	}

	// §10 steps 1-2. Validation runs before any probe executes: a malformed
	// manifest is Ferret being broken, not a finding about the machine, and
	// running half of a broken manifest produces a report that looks complete
	// and is not.
	m, warnings, err := LoadManifest(manifestDir)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "ferret: warning: %s\n", w)
	}
	if err != nil {
		var ve *ValidationError
		if ok := asValidationError(err, &ve); ok {
			fmt.Fprintf(os.Stderr, "ferret: manifest validation failed\n")
			for _, p := range ve.Problems {
				fmt.Fprintf(os.Stderr, "  - %s\n", p)
			}
			return exitCannot, fmt.Errorf("%d manifest problem(s); no probes were run", len(ve.Problems))
		}
		return exitCannot, err
	}

	if validateOnly {
		fmt.Printf("manifest ok: %d checks across %d file(s)\n", len(m.Checks), len(m.Files))
		return exitGo, nil
	}

	var ev *Evidence

	if verdictOnly {
		// Re-decide from stored evidence, touching nothing. This is the
		// property the capture/interpret split buys: change an `expect`, run
		// this, get a new verdict without re-measuring the machine.
		ev, err = ReadEvidence(outPath)
		if err != nil {
			return exitCannot, err
		}
		fmt.Fprintf(os.Stderr, "ferret: re-deciding from %s (captured %s); no probes run\n",
			outPath, ev.StartedAt)
	} else {
		if rawDir != "" {
			if err := os.MkdirAll(rawDir, 0o755); err != nil {
				// Losing the debug directory is not worth losing the sweep.
				fmt.Fprintf(os.Stderr, "ferret: warning: no raw dir (%v); continuing\n", err)
				rawDir = ""
			}
		}

		abs, err := filepath.Abs(workDir)
		if err != nil {
			return exitCannot, fmt.Errorf("resolving -dir: %w", err)
		}

		r := &Runner{
			Manifest:    m,
			WorkDir:     abs,
			RawDir:      rawDir,
			RedactLevel: redact,
			Version:     version,
		}

		ev, err = r.Run(context.Background())
		if err != nil {
			return exitCannot, err
		}

		if err := WriteEvidence(outPath, ev); err != nil {
			return exitCannot, err
		}
		fmt.Fprintf(os.Stderr, "ferret: captured %d checks -> %s\n", len(ev.Records), outPath)
	}

	// Capture is done. Interpretation starts here, and it is a separate step
	// against a file -- not a continuation of the sweep. The runner above
	// produced evidence without knowing what any of it means.
	rep, err := Evaluate(m, ev)
	if err != nil {
		return exitCannot, err
	}

	printSummary(rep)

	// Exit codes are the verdict engine's to set, per ARCHITECTURE §11. The
	// runner alone never returns 1 or 2, because it has made no judgment.
	return rep.ExitCode(), nil
}

// printSummary is a placeholder for M4's renderer.
//
// It is deliberately plain: the severity-ordered glance is the product and it
// gets designed on its own, not smuggled in as a debug print that nobody
// revisits. This exists so M2 is runnable and verifiable now.
func printSummary(rep *Report) {
	for _, v := range rep.Verdicts {
		line := fmt.Sprintf("%-8s %-28s %s", v.State, v.ID, v.Detail)
		if v.State == StateUnknown {
			line = fmt.Sprintf("%-8s %-28s %s: %s", v.State, v.ID, v.Reason, v.Detail)
		}
		fmt.Println(line)
		if v.Remedy != "" {
			fmt.Printf("         %-28s -> %s\n", "", v.Remedy)
		}
		if v.TaintedCount > 0 {
			fmt.Printf("         %-28s -> taints %d check(s) below\n", "", v.TaintedCount)
		}
	}
	fmt.Printf("\n%d GO  %d NO-GO  %d UNKNOWN  %d N/A\n",
		rep.Counts[StateGo], rep.Counts[StateNoGo],
		rep.Counts[StateUnknown], rep.Counts[StateNA])
}

func asValidationError(err error, target **ValidationError) bool {
	if ve, ok := err.(*ValidationError); ok {
		*target = ve
		return true
	}
	return false
}
