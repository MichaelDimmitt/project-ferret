package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "../tests/fixtures"

// The positive case. If this file stops loading, the schema and the fixture
// have diverged and one of them is wrong.
func TestValidManifestLoads(t *testing.T) {
	m, warnings, err := LoadManifest(filepath.Join(fixtures, "manifest-valid"))
	if err != nil {
		if ve, ok := err.(*ValidationError); ok {
			for _, p := range ve.Problems {
				t.Errorf("unexpected problem: %s", p)
			}
		}
		t.Fatalf("valid manifest failed to load: %v", err)
	}
	for _, w := range warnings {
		t.Logf("warning (not a failure): %s", w)
	}

	if len(m.Checks) != 12 {
		t.Errorf("got %d checks, want 12", len(m.Checks))
	}

	// Every expect type must appear, or the fixture is not covering the schema.
	seen := map[string]bool{}
	for i := range m.Checks {
		seen[m.Checks[i].Expect.Type] = true
	}
	for want := range expectFields {
		if !seen[want] {
			t.Errorf("fixture does not exercise expect type %q", want)
		}
	}
}

func TestDefaults(t *testing.T) {
	m, _, err := LoadManifest(filepath.Join(fixtures, "manifest-valid"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	blocker := m.Get("fixture.matches")
	if blocker == nil {
		t.Fatal("fixture.matches missing")
	}
	if !blocker.IsDecisive() {
		t.Error("a blocker with no explicit decisive should default to decisive")
	}
	if blocker.Timeout() != defaultTimeoutS {
		t.Errorf("timeout default = %v, want %v", blocker.Timeout(), defaultTimeoutS)
	}
	if blocker.VolatilityClass() != defaultVolatility {
		t.Errorf("volatility default = %q, want %q", blocker.VolatilityClass(), defaultVolatility)
	}
	if blocker.LayerOf(0) != 4 {
		t.Errorf("layer should be inherited from the file, got %d", blocker.LayerOf(0))
	}

	info := m.Get("fixture.absent")
	if info == nil {
		t.Fatal("fixture.absent missing")
	}
	if info.IsDecisive() {
		t.Error("a non-blocker should default to non-decisive")
	}
}

// Array-form probes join with newlines, so a long script stays readable in
// JSON without changing what sh receives.
func TestArrayProbeJoins(t *testing.T) {
	m, _, err := LoadManifest(filepath.Join(fixtures, "manifest-valid"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := m.Get("fixture.numeric_lt")
	if c == nil {
		t.Fatal("fixture.numeric_lt missing")
	}
	if !strings.Contains(c.Probe.Script, "\n") {
		t.Errorf("array probe should join with newlines, got %q", c.Probe.Script)
	}
}

func TestRedactLevelParsing(t *testing.T) {
	m, _, err := LoadManifest(filepath.Join(fixtures, "manifest-valid"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := m.Get("fixture.secret").RedactLevel; got != RedactSecret {
		t.Errorf("secret check redact = %q, want %q", got, RedactSecret)
	}
	if got := m.Get("fixture.non_empty").RedactLevel; got != RedactIdentity {
		t.Errorf("identity check redact = %q, want %q", got, RedactIdentity)
	}
	if got := m.Get("fixture.absent").RedactLevel; got != RedactNone {
		t.Errorf("unset redact = %q, want empty", got)
	}
}

// The negative cases. Each fixture is invalid for exactly one documented
// reason, and the error message must name that reason -- a test that passes
// because the loader tripped on a different rule proves nothing.
func TestInvalidManifestsAreRejected(t *testing.T) {
	cases := []struct {
		file string
		rule string // §8 rule number, for the failure message
		want string // substring the error must contain
	}{
		{"unknown-field.json", "1", "timout_s"},
		{"duplicate-id.json", "2", "duplicate id"},
		{"dangling-taint.json", "3", "does not exist"},
		{"taint-cycle.json", "4", "cycle"},
		{"missing-remedy.json", "5", "requires a remedy"},
		{"mutating-no-why.json", "6", "mutating_why"},
		{"mutating-true.json", "6", "must be false"},
		{"declared-unset.json", "7", "no declared field"},
		{"unknown-token.json", "8", "unknown interpolation token"},
		{"bad-expect-type.json", "9", "unknown expect.type"},
		{"expect-missing-field.json", "9", "requires field"},
		{"bad-regex.json", "10", "not a valid regexp"},
		{"bad-timeout.json", "11", "timeout_s must be > 0"},
		{"bad-severity.json", "11", "is not blocker|warning|info"},
		{"bad-layer.json", "12", "outside 0-10"},
		{"bad-id.json", "12", "does not match"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			link(t, filepath.Join(fixtures, "manifest-invalid", tc.file),
				filepath.Join(dir, tc.file))

			_, _, err := LoadManifest(dir)
			if err == nil {
				t.Fatalf("§8 rule %s: %s loaded successfully; it must be rejected",
					tc.rule, tc.file)
			}
			if !strings.Contains(err.Error()+problemsOf(err), tc.want) {
				t.Errorf("§8 rule %s: error does not mention %q\ngot: %s",
					tc.rule, tc.want, problemsOf(err))
			}
		})
	}
}

// Rule 2 across files, which a single directory cannot express.
func TestDuplicateIDAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	link(t, filepath.Join(fixtures, "manifest-dup-a", "04-runtime.json"),
		filepath.Join(dir, "04-runtime.json"))
	link(t, filepath.Join(fixtures, "manifest-dup-b", "05-package.json"),
		filepath.Join(dir, "05-package.json"))

	_, _, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("duplicate id across files was accepted")
	}
	if !strings.Contains(problemsOf(err), "duplicate id") {
		t.Errorf("error should name the duplicate: %s", problemsOf(err))
	}
}

// A cycle must name both ends rather than resolving arbitrarily.
func TestCycleNamesBothEnds(t *testing.T) {
	dir := t.TempDir()
	link(t, filepath.Join(fixtures, "manifest-invalid", "taint-cycle.json"),
		filepath.Join(dir, "taint-cycle.json"))

	_, _, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("cycle was accepted")
	}
	got := problemsOf(err)
	for _, end := range []string{"bad.cycle_a", "bad.cycle_b"} {
		if !strings.Contains(got, end) {
			t.Errorf("cycle error does not name %s: %s", end, got)
		}
	}
}

// Validation collects every problem rather than stopping at the first. Three
// round trips to see three typos is a bad tool.
func TestValidationCollectsAllProblems(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.json"), `{
	  "layer": 4,
	  "checks": [
	    {"id": "a.one", "title": "", "probe": "true",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "blocker"},
	    {"id": "BAD-ID", "title": "t", "probe": "true",
	     "expect": {"type": "nope", "actual": "$probe"}, "severity": "wrong"}
	  ]
	}`)

	_, _, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected rejection")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("want *ValidationError, got %T", err)
	}
	if len(ve.Problems) < 4 {
		t.Errorf("want at least 4 problems (empty title, missing remedy, bad id, "+
			"bad severity, bad expect type), got %d:\n%s",
			len(ve.Problems), strings.Join(ve.Problems, "\n"))
	}
}

func TestInterpolationTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"$probe", []string{"probe"}},
		{"$declared and $probe_exit", []string{"declared", "probe_exit"}},
		{"$$literal", nil},
		{"cost is $$5 for $probe", []string{"probe"}},
		{"no tokens here", nil},
	}
	for _, tc := range cases {
		got := interpolationTokens(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("interpolationTokens(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("interpolationTokens(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

// Warnings do not fail a load. A $probe inside a probe is legal shell and
// almost certainly a mistake, but it is the author's mistake to make.
func TestWarningsDoNotFail(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "warn.json"), `{
	  "layer": 4,
	  "checks": [
	    {"id": "warn.one", "title": "Shell interpolation confusion",
	     "probe": "echo $probe", "severity": "info",
	     "expect": {"type": "non_empty", "actual": "$probe"}}
	  ]
	}`)

	_, warnings, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("warning should not fail the load: %v", problemsOf(err))
	}
	if len(warnings) == 0 {
		t.Error("expected a warning about $probe in a probe")
	}
}
