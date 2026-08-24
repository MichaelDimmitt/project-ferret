package main

import (
	"context"
	"strings"
	"testing"
)

// runOne is a small harness: one check, run through the real runner, returning
// its probe record. Tests below differ only in the check, so the wiring is
// shared rather than repeated.
func runOne(t *testing.T, c Check) *RunResult {
	t.Helper()
	if c.ID == "" {
		c.ID = "test.check"
	}
	r := &Runner{
		Manifest: &Manifest{Checks: []Check{c}},
		WorkDir:  t.TempDir(),
		Version:  "test",
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(ev.Records) != 1 {
		t.Fatalf("want 1 record, got %d", len(ev.Records))
	}
	if ev.Records[0].Probe == nil {
		t.Fatalf("probe did not run: %+v", ev.Records[0].Error)
	}
	return ev.Records[0].Probe
}

func isolated() *bool { b := true; return &b }

// The point of the field: a variable the user's shell exported must not reach
// an isolated probe. This is the .zshrc false negative, in miniature.
func TestIsolateClearsInheritedEnv(t *testing.T) {
	t.Setenv("FERRET_TEST_LEAKY_VAR", "leaked-value")

	got := runOne(t, Check{
		Probe:   Command{Set: true, Script: `printenv FERRET_TEST_LEAKY_VAR || echo ABSENT`},
		Isolate: isolated(),
	})

	if strings.Contains(got.Stdout, "leaked-value") {
		t.Errorf("an inherited variable reached an isolated probe: %q", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "ABSENT") {
		t.Errorf("want ABSENT, got %q", got.Stdout)
	}
}

// Isolation is opt-in. Without it, the real environment is what gets measured,
// because "is the proxy set here" is a question about this shell.
func TestWithoutIsolateEnvIsInherited(t *testing.T) {
	t.Setenv("FERRET_TEST_LEAKY_VAR", "leaked-value")

	got := runOne(t, Check{
		Probe: Command{Set: true, Script: `printenv FERRET_TEST_LEAKY_VAR || echo ABSENT`},
	})

	if !strings.Contains(got.Stdout, "leaked-value") {
		t.Errorf("a non-isolated probe should see the real environment, got %q", got.Stdout)
	}
}

// PATH is set, not cleared. A probe with no PATH reports every tool absent,
// which is the same false answer isolation exists to prevent.
func TestIsolatedProbeCanStillFindSystemTools(t *testing.T) {
	got := runOne(t, Check{
		Probe:   Command{Set: true, Script: `command -v uname >/dev/null && echo FOUND`},
		Isolate: isolated(),
	})

	if !strings.Contains(got.Stdout, "FOUND") {
		t.Errorf("an isolated probe could not find uname on the system PATH; "+
			"stdout=%q stderr=%q exit=%d", got.Stdout, got.Stderr, got.Exit)
	}
}

// HOME survives: git finds no config without it, and it is not the variable
// that causes the false negative.
func TestIsolatedProbeKeepsHome(t *testing.T) {
	got := runOne(t, Check{
		Probe:   Command{Set: true, Script: `[ -n "${HOME:-}" ] && echo HAS_HOME`},
		Isolate: isolated(),
	})

	if !strings.Contains(got.Stdout, "HAS_HOME") {
		t.Errorf("HOME did not survive isolation: %q", got.Stdout)
	}
}

// The gate is never isolated, even when the check is.
//
// A fresh-shell check gates on the tool existing in the REAL environment and
// then measures whether it survives a clean one. Isolating the gate makes that
// self-cancelling: it fails in the clean env, the check resolves N/A, and the
// problem it exists to find is silently skipped. Observed in
// shell.path.node_fresh_shell before the rule existed.
func TestGateIsNeverIsolated(t *testing.T) {
	t.Setenv("FERRET_TEST_GATE_VAR", "present")

	r := &Runner{
		Manifest: &Manifest{Checks: []Check{{
			ID: "gate.unisolated",
			// Passes only in the real environment.
			AppliesIf: Command{Set: true, Script: `[ -n "${FERRET_TEST_GATE_VAR:-}" ]`},
			// Reports what the clean environment sees.
			Probe:   Command{Set: true, Script: `printenv FERRET_TEST_GATE_VAR || echo ABSENT`},
			Isolate: isolated(),
		}}},
		WorkDir: t.TempDir(),
		Version: "test",
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	rec := ev.Records[0]
	if !rec.Applies {
		t.Fatal("the gate was isolated: the check resolved N/A and could never fire")
	}
	if rec.Probe == nil {
		t.Fatal("probe did not run")
	}
	if !strings.Contains(rec.Probe.Stdout, "ABSENT") {
		t.Errorf("the probe should still be isolated, got %q", rec.Probe.Stdout)
	}
}

// isolate: false is rejected rather than ignored. A reader who writes it
// believes it does something.
func TestIsolateFalseIsRejected(t *testing.T) {
	no := false
	m := &Manifest{Checks: []Check{{
		ID:       "a.b",
		Title:    "t",
		Probe:    Command{Set: true, Script: "true"},
		Expect:   Expect{Type: "exit_code", Raw: map[string]any{"type": "exit_code", "value": float64(0)}},
		Severity: "info",
		Isolate:  &no,
	}}}

	ve := &ValidationError{}
	m.validate(ve)
	if len(ve.Problems) == 0 {
		t.Fatal("isolate: false should be a validation error, not silently ignored")
	}
	if !strings.Contains(strings.Join(ve.Problems, "\n"), "isolate") {
		t.Errorf("the error should name the field: %v", ve.Problems)
	}
}
