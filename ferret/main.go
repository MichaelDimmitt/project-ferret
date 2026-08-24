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
	)
	flag.Parse()

	if err := run(*manifestDir, *outPath, *workDir, *rawDir, Redact(*redactFlag), *validate); err != nil {
		fmt.Fprintf(os.Stderr, "ferret: %v\n", err)
		os.Exit(exitCannot)
	}
}

func run(manifestDir, outPath, workDir, rawDir string, redact Redact, validateOnly bool) error {
	switch redact {
	case RedactNone, RedactIdentity, RedactSecret:
	default:
		return fmt.Errorf("unknown -redact %q (want: identity, secret, or empty)", redact)
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
			return fmt.Errorf("%d manifest problem(s); no probes were run", len(ve.Problems))
		}
		return err
	}

	if validateOnly {
		fmt.Printf("manifest ok: %d checks across %d file(s)\n", len(m.Checks), len(m.Files))
		return nil
	}

	if rawDir != "" {
		if err := os.MkdirAll(rawDir, 0o755); err != nil {
			// Losing the debug directory is not worth losing the sweep.
			fmt.Fprintf(os.Stderr, "ferret: warning: no raw dir (%v); continuing\n", err)
			rawDir = ""
		}
	}

	abs, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolving -dir: %w", err)
	}

	r := &Runner{
		Manifest:    m,
		WorkDir:     abs,
		RawDir:      rawDir,
		RedactLevel: redact,
		Version:     version,
	}

	ev, err := r.Run(context.Background())
	if err != nil {
		return err
	}

	if err := WriteEvidence(outPath, ev); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "ferret: captured %d checks -> %s\n", len(ev.Records), outPath)

	// The runner exits 0 when it captured everything it was asked to, and 3
	// when it could not do its job. It NEVER exits 1 or 2: those are verdicts,
	// and it has no idea what it captured. The process that decides is a
	// different process, and it does not exist yet (M2).
	return nil
}

func asValidationError(err error, target **ValidationError) bool {
	if ve, ok := err.(*ValidationError); ok {
		*target = ve
		return true
	}
	return false
}
