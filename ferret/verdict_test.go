package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evaluate builds a manifest and evidence from inline JSON and resolves them.
func evaluate(t *testing.T, manifestJSON, evidenceJSON string) *Report {
	t.Helper()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "00-test.json"), manifestJSON)
	m, _, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("load manifest: %v", problemsOf(err))
	}

	var ev Evidence
	if err := json.Unmarshal([]byte(evidenceJSON), &ev); err != nil {
		t.Fatalf("parse evidence: %v", err)
	}

	rep, err := Evaluate(m, &ev)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return rep
}

func verdictFor(t *testing.T, rep *Report, id string) Verdict {
	t.Helper()
	for _, v := range rep.Verdicts {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no verdict for %s", id)
	return Verdict{}
}

// A minimal evidence document with one probe result.
func evidenceWith(records string) string {
	return `{"schema_version": 1, "ferret_version": "test",
	  "started_at": "2026-08-24T00:00:00Z", "finished_at": "2026-08-24T00:00:01Z",
	  "records": [` + records + `]}`
}

func probeRec(id, stdout string, exit int) string {
	b, _ := json.Marshal(stdout)
	return `{"id": "` + id + `", "applies": true,
	  "probe": {"stdout": ` + string(b) + `, "stderr": "", "exit": ` +
		itoa(exit) + `, "duration_ms": 1}, "provenance": "observed"}`
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// A remedy interpolates §5's tokens, exactly as `expect` values do.
//
// MANIFEST_SCHEMA.md §5 says interpolation applies "Inside `expect` values,
// `remedy`, and nowhere else", and §3's own example is `nvm use $declared`.
// Only expect was wired up: Evaluate assigned c.Remedy raw, so every remedy
// using a token printed it literally and handed the reader a command naming
// an unset shell variable. Found in M7 by reading a rendered glance, not by a
// test -- the check itself was correct and only its advice was broken, which
// is the kind of defect that survives a green suite.
func TestRemedyInterpolatesTokens(t *testing.T) {
	rec := `{"id": "r.pin", "applies": true,
	  "probe": {"stdout": "v24.13.1", "stderr": "", "exit": 0, "duration_ms": 1},
	  "declared": {"stdout": "20.15.1", "stderr": "", "exit": 0, "duration_ms": 1},
	  "provenance": "observed"}`

	rep := evaluate(t, `{
	  "layer": 4,
	  "checks": [
	    {"id": "r.pin", "title": "Node matches pin", "probe": "node -v",
	     "declared": "cat .nvmrc",
	     "expect": {"type": "semver_satisfies", "actual": "$probe", "range": "$declared"},
	     "severity": "blocker", "remedy": "nvm use $declared   # was $probe"}
	  ]
	}`, evidenceWith(rec))

	got := verdictFor(t, rep, "r.pin").Remedy
	want := "nvm use 20.15.1   # was v24.13.1"
	if got != want {
		t.Errorf("remedy = %q, want %q; an uninterpolated remedy names a\n"+
			"shell variable that is not set, which is worse than no remedy", got, want)
	}
}

// ============================================================
// THE INVARIANT
// ============================================================

// No code path converts UNKNOWN into GO. PLAN.md M2 requires this assertion
// explicitly, and it is the failure mode the whole tool exists to prevent: a
// silent omission rendering as a verified pass.
func TestUnknownNeverBecomesGo(t *testing.T) {
	// Every way a check can arrive at UNKNOWN, exercised in one manifest.
	manifest := `{
	  "layer": 0,
	  "checks": [
	    {"id": "u.timeout", "title": "Timed out", "probe": "sleep 99",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "r"},
	    {"id": "u.absent", "title": "Tool missing", "probe": "nope",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "r"},
	    {"id": "u.nonnumeric", "title": "Garbage where a number was wanted",
	     "probe": "echo notanumber",
	     "expect": {"type": "numeric_lt", "actual": "$probe", "value": 10},
	     "severity": "blocker", "remedy": "r"},
	    {"id": "u.badrange", "title": "Range outside the table",
	     "probe": "echo 20.1.0", "declared": "echo 1.x || 2.x",
	     "expect": {"type": "semver_satisfies", "actual": "$probe", "range": "$declared"},
	     "severity": "blocker", "remedy": "r"},
	    {"id": "u.norange", "title": "Nothing was declared",
	     "probe": "echo 20.1.0", "declared": "true",
	     "expect": {"type": "semver_satisfies", "actual": "$probe", "range": "$declared"},
	     "severity": "blocker", "remedy": "r"},
	    {"id": "u.missing", "title": "No evidence at all", "probe": "true",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "r"}
	  ]
	}`

	ev := evidenceWith(`
	  {"id": "u.timeout", "applies": true,
	   "probe": {"stdout": "", "stderr": "", "exit": -1, "timed_out": true},
	   "error": {"kind": "timeout", "detail": "exceeded"}},
	  {"id": "u.absent", "applies": true,
	   "probe": {"stdout": "", "stderr": "not found", "exit": 127},
	   "error": {"kind": "tool_absent", "detail": "nope: not found"}},
	  ` + probeRec("u.nonnumeric", "notanumber", 0) + `,
	  {"id": "u.badrange", "applies": true,
	   "probe": {"stdout": "20.1.0", "exit": 0},
	   "declared": {"stdout": "1.x || 2.x", "exit": 0}},
	  {"id": "u.norange", "applies": true,
	   "probe": {"stdout": "20.1.0", "exit": 0},
	   "declared": {"stdout": "", "exit": 0}}`)

	rep := evaluate(t, manifest, ev)

	for _, v := range rep.Verdicts {
		if v.State == StateGo {
			t.Errorf("%s resolved GO; every check here must be UNKNOWN\ndetail: %s",
				v.ID, v.Detail)
		}
		if v.State != StateUnknown {
			t.Errorf("%s = %s, want UNKNOWN", v.ID, v.State)
		}
		// An UNKNOWN with no reason is indistinguishable from a shrug.
		if v.Reason == "" {
			t.Errorf("%s is UNKNOWN with no reason", v.ID)
		}
	}

	// And it must not pass as a process either.
	if code := rep.ExitCode(); code == exitGo {
		t.Error("a report of nothing but UNKNOWNs exited 0")
	}
}

// Exit code 2 is never collapsed into 0.
func TestUnknownExitsTwoNotZero(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "a.ok", "title": "Fine", "probe": "echo x",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"},
	    {"id": "a.unknown", "title": "Could not check", "probe": "nope",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "install it"}
	  ]
	}`, evidenceWith(probeRec("a.ok", "x", 0)+`,
	  {"id": "a.unknown", "applies": true,
	   "probe": {"stdout": "", "exit": 127},
	   "error": {"kind": "tool_absent", "detail": "missing"}}`))

	if got := rep.ExitCode(); got != exitUnknown {
		t.Errorf("exit = %d, want %d (unknowns must not collapse into 0)", got, exitUnknown)
	}
}

// A non-decisive UNKNOWN does not force exit 2. That is what the field is for.
func TestNonDecisiveUnknownDoesNotForceExitTwo(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "b.context", "title": "Context only", "probe": "nope",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info", "decisive": false}
	  ]
	}`, evidenceWith(`{"id": "b.context", "applies": true,
	   "probe": {"stdout": "", "exit": 127},
	   "error": {"kind": "tool_absent", "detail": "missing"}}`))

	if got := rep.ExitCode(); got != exitGo {
		t.Errorf("exit = %d, want 0: a non-decisive unknown is context", got)
	}
}

// ============================================================
// Re-runnability: evidence in, verdict out, no machine touched
// ============================================================

// PLAN.md M2 exit criterion: editing an expect and re-running changes the
// verdict without touching the machine.
func TestEditingExpectChangesVerdictWithoutReprobing(t *testing.T) {
	ev := evidenceWith(probeRec("c.node", "v18.19.0", 0))

	strict := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "c.node", "title": "Node 20", "probe": "node -v",
	     "expect": {"type": "matches", "actual": "$probe", "pattern": "^v20\\."},
	     "severity": "blocker", "remedy": "nvm use 20"}
	  ]
	}`, ev)

	if got := verdictFor(t, strict, "c.node").State; got != StateNoGo {
		t.Errorf("with a 20-only pattern: %s, want NO-GO", got)
	}

	// Same evidence, looser expectation.
	loose := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "c.node", "title": "Node 18 or 20", "probe": "node -v",
	     "expect": {"type": "matches", "actual": "$probe", "pattern": "^v(18|20)\\."},
	     "severity": "blocker", "remedy": "nvm use 20"}
	  ]
	}`, ev)

	if got := verdictFor(t, loose, "c.node").State; got != StateGo {
		t.Errorf("with a permissive pattern: %s, want GO", got)
	}
}

// A NO-GO must carry its remedy; a GO must not (there is nothing to fix).
func TestRemedyAccompaniesNoGo(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "d.fail", "title": "Fails", "probe": "echo wrong",
	     "expect": {"type": "equals", "actual": "$probe", "value": "right"},
	     "severity": "blocker", "remedy": "do the thing"},
	    {"id": "d.pass", "title": "Passes", "probe": "echo right",
	     "expect": {"type": "equals", "actual": "$probe", "value": "right"},
	     "severity": "blocker", "remedy": "do the thing"}
	  ]
	}`, evidenceWith(probeRec("d.fail", "wrong", 0)+`,`+probeRec("d.pass", "right", 0)))

	if r := verdictFor(t, rep, "d.fail").Remedy; r != "do the thing" {
		t.Errorf("NO-GO remedy = %q, want the manifest's", r)
	}
	if r := verdictFor(t, rep, "d.pass").Remedy; r != "" {
		t.Errorf("GO carries a remedy %q; there is nothing to fix", r)
	}
}

// ============================================================
// Predicates
// ============================================================

func TestPredicates(t *testing.T) {
	cases := []struct {
		name   string
		expect string
		stdout string
		exit   int
		want   State
	}{
		{"equals hit", `{"type": "equals", "actual": "$probe", "value": "x"}`, "x", 0, StateGo},
		{"equals miss", `{"type": "equals", "actual": "$probe", "value": "x"}`, "y", 0, StateNoGo},
		{"equals trims", `{"type": "equals", "actual": "$probe", "value": "x"}`, "  x  \n", 0, StateGo},
		{"matches hit", `{"type": "matches", "actual": "$probe", "pattern": "^v?20\\."}`, "v20.11.0", 0, StateGo},
		{"matches miss", `{"type": "matches", "actual": "$probe", "pattern": "^v?20\\."}`, "v18.0.0", 0, StateNoGo},
		{"matches invert", `{"type": "matches", "actual": "$probe", "pattern": "^v19\\.", "invert": true}`, "v20.0.0", 0, StateGo},
		{"one_of hit", `{"type": "one_of", "actual": "$probe", "values": ["arm64", "x86_64"]}`, "arm64", 0, StateGo},
		{"one_of miss", `{"type": "one_of", "actual": "$probe", "values": ["arm64"]}`, "riscv", 0, StateNoGo},
		{"non_empty hit", `{"type": "non_empty", "actual": "$probe"}`, "something", 0, StateGo},
		{"non_empty miss", `{"type": "non_empty", "actual": "$probe"}`, "", 0, StateNoGo},
		{"non_empty whitespace is empty", `{"type": "non_empty", "actual": "$probe"}`, "  \n ", 0, StateNoGo},
		{"absent hit", `{"type": "absent", "actual": "$probe"}`, "", 0, StateGo},
		{"absent miss", `{"type": "absent", "actual": "$probe"}`, "set", 0, StateNoGo},
		{"numeric_lt hit", `{"type": "numeric_lt", "actual": "$probe", "value": 100}`, "42", 0, StateGo},
		{"numeric_lt miss", `{"type": "numeric_lt", "actual": "$probe", "value": 10}`, "42", 0, StateNoGo},
		{"numeric_gt hit", `{"type": "numeric_gt", "actual": "$probe", "value": 10}`, "42", 0, StateGo},
		{"numeric non-numeric is UNKNOWN", `{"type": "numeric_lt", "actual": "$probe", "value": 10}`, "command not found", 0, StateUnknown},
		{"numeric float", `{"type": "numeric_lt", "actual": "$probe", "value": 1.5}`, "1.25", 0, StateGo},
		{"exit_code hit", `{"type": "exit_code", "of": "probe", "value": 0}`, "", 0, StateGo},
		{"exit_code miss", `{"type": "exit_code", "of": "probe", "value": 0}`, "", 7, StateNoGo},
		{"exit_code values hit", `{"type": "exit_code", "of": "probe", "values": [0, 1]}`, "", 1, StateGo},
		{"exit_code values miss", `{"type": "exit_code", "of": "probe", "values": [0, 1]}`, "", 2, StateNoGo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sev := `"severity": "warning", "remedy": "r"`
			rep := evaluate(t, `{"layer": 0, "checks": [
			  {"id": "p.check", "title": "t", "probe": "x",
			   "expect": `+tc.expect+`, `+sev+`}]}`,
				evidenceWith(probeRec("p.check", tc.stdout, tc.exit)))

			if got := verdictFor(t, rep, "p.check").State; got != tc.want {
				t.Errorf("state = %s, want %s (detail: %s)",
					got, tc.want, verdictFor(t, rep, "p.check").Detail)
			}
		})
	}
}

func TestSemverPredicate(t *testing.T) {
	cases := []struct {
		actual   string
		declared string
		want     State
	}{
		{"v20.11.0", "20", StateGo},
		{"v20.11.0", "20.11", StateGo},
		{"v20.11.0", "20.11.0", StateGo},
		{"v18.19.0", "20", StateNoGo},
		{"v20.11.0", "^20.10.0", StateGo},
		{"v21.0.0", "^20.10.0", StateNoGo},
		{"v20.11.5", "~20.11.0", StateGo},
		{"v20.12.0", "~20.11.0", StateNoGo},
		{"v20.11.0", ">=20.0.0", StateGo},
		{"v19.0.0", ">=20.0.0", StateNoGo},
		{"v20.11.0", ">=20.0.0 <21.0.0", StateGo},
		{"v21.1.0", ">=20.0.0 <21.0.0", StateNoGo},
		{"v18.0.0", "18 || 20", StateGo},
		{"v20.0.0", "18 || 20", StateGo},
		{"v19.0.0", "18 || 20", StateNoGo},
		{"v20.11.0", "*", StateGo},
		// Outside the documented table: UNKNOWN, never a guess.
		{"v20.11.0", "20.x", StateUnknown},
		{"v20.11.0", "1.0.0 - 2.0.0", StateUnknown},
		{"not-a-version", "20", StateUnknown},
		{"v20.11.0", "", StateUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.actual+" vs "+tc.declared, func(t *testing.T) {
			rep := evaluate(t, `{"layer": 0, "checks": [
			  {"id": "s.node", "title": "t", "probe": "node -v", "declared": "cat .nvmrc",
			   "expect": {"type": "semver_satisfies", "actual": "$probe", "range": "$declared"},
			   "severity": "blocker", "remedy": "r"}]}`,
				evidenceWith(`{"id": "s.node", "applies": true,
				  "probe": {"stdout": "`+tc.actual+`", "exit": 0},
				  "declared": {"stdout": "`+tc.declared+`", "exit": 0}}`))

			v := verdictFor(t, rep, "s.node")
			if v.State != tc.want {
				t.Errorf("state = %s, want %s (detail: %s)", v.State, tc.want, v.Detail)
			}
		})
	}
}

// Prereleases sort below their release. Getting this backwards would let an
// rc satisfy a range meant to exclude it.
func TestPrereleaseOrdering(t *testing.T) {
	rc, _ := parseVersion("20.0.0-rc1")
	rel, _ := parseVersion("20.0.0")
	if rc.compare(rel) >= 0 {
		t.Error("20.0.0-rc1 must sort below 20.0.0")
	}
	if rel.compare(rc) <= 0 {
		t.Error("20.0.0 must sort above 20.0.0-rc1")
	}

	// And a prerelease does not satisfy a plain prefix range.
	ok, err := satisfies("20.0.0-rc1", "20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("20.0.0-rc1 should not satisfy the range \"20\"")
	}
}

// ============================================================
// Interpolation
// ============================================================

func TestInterpolation(t *testing.T) {
	rec := &Record{
		Probe:    &RunResult{Stdout: "  v20.11.0\n", Stderr: "warn", Exit: 3},
		Declared: &RunResult{Stdout: "20\n", Stderr: "", Exit: 0},
	}
	cases := []struct{ in, want string }{
		{"$probe", "v20.11.0"},
		{"$declared", "20"},
		{"$probe_exit", "3"},
		{"$declared_exit", "0"},
		{"$probe_stderr", "warn"},
		{"$$literal", "$literal"},
		{"$probe vs $declared", "v20.11.0 vs 20"},
		{"no tokens", "no tokens"},
	}
	for _, tc := range cases {
		got, err := interpolate(tc.in, rec)
		if err != nil {
			t.Errorf("interpolate(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("interpolate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// $probe_exit must not be read as $probe followed by the literal "_exit".
func TestLongestTokenWins(t *testing.T) {
	rec := &Record{Probe: &RunResult{Stdout: "OUT", Exit: 5}}
	got, err := interpolate("$probe_exit", rec)
	if err != nil {
		t.Fatal(err)
	}
	if got != "5" {
		t.Errorf("got %q, want \"5\" (token matching must be longest-first)", got)
	}
}

// ============================================================
// Taint
// ============================================================

func TestTaintMarksDependentsUnknown(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.clock", "title": "Clock skew", "probe": "echo 9999",
	     "expect": {"type": "numeric_lt", "actual": "$probe", "value": 120},
	     "severity": "blocker", "remedy": "sync the clock"},
	    {"id": "t.tls", "title": "Registry reachable", "probe": "echo ok",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "check network",
	     "tainted_by": ["t.clock"]},
	    {"id": "t.deps", "title": "Deps installable", "probe": "echo ok",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "npm ci",
	     "tainted_by": ["t.tls"]}
	  ]
	}`, evidenceWith(
		probeRec("t.clock", "9999", 0)+`,`+
			probeRec("t.tls", "ok", 0)+`,`+
			probeRec("t.deps", "ok", 0)))

	if got := verdictFor(t, rep, "t.clock").State; got != StateNoGo {
		t.Errorf("cause = %s, want NO-GO", got)
	}

	// Both dependents are tainted, even though their own probes said "ok".
	// That is the entire point: a green check below a broken clock is
	// meaningless.
	for _, id := range []string{"t.tls", "t.deps"} {
		v := verdictFor(t, rep, id)
		if v.State != StateUnknown {
			t.Errorf("%s = %s, want UNKNOWN (its probe said ok, but the clock is wrong)",
				id, v.State)
		}
		if v.Reason != ReasonTainted {
			t.Errorf("%s reason = %q, want %q", id, v.Reason, ReasonTainted)
		}
	}

	// The rollup lands on the root cause, for "-> taints N checks below".
	if n := verdictFor(t, rep, "t.clock").TaintedCount; n != 2 {
		t.Errorf("TaintedCount = %d, want 2", n)
	}
}

// The probe output survives taint. Taint is a judgment about trustworthiness,
// not a reason to discard what was measured -- which is what makes it possible
// to fix the clock and re-run the verdict without re-probing.
func TestTaintPreservesEvidence(t *testing.T) {
	ev := evidenceWith(
		probeRec("t2.cause", "9999", 0) + `,` + probeRec("t2.dep", "measured-value", 0))

	var parsed Evidence
	if err := json.Unmarshal([]byte(ev), &parsed); err != nil {
		t.Fatal(err)
	}

	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t2.cause", "title": "Cause", "probe": "x",
	     "expect": {"type": "numeric_lt", "actual": "$probe", "value": 10},
	     "severity": "blocker", "remedy": "fix"},
	    {"id": "t2.dep", "title": "Dependent", "probe": "x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "fix", "tainted_by": ["t2.cause"]}
	  ]
	}`, ev)

	if verdictFor(t, rep, "t2.dep").State != StateUnknown {
		t.Fatal("dependent should be tainted")
	}
	// The evidence object is untouched by evaluation.
	for _, r := range parsed.Records {
		if r.ID == "t2.dep" && !strings.Contains(r.Probe.Stdout, "measured-value") {
			t.Error("evidence was mutated by the verdict engine")
		}
	}
}

// A tainted check's remedy belongs to the prerequisite, not to it. Telling
// someone to fix a symptom is a wrong remedy, and a wrong remedy is worse
// than none.
func TestTaintedCheckDropsItsRemedy(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t3.cause", "title": "Cause", "probe": "x",
	     "expect": {"type": "absent", "actual": "$probe"},
	     "severity": "blocker", "remedy": "fix the cause"},
	    {"id": "t3.dep", "title": "Dependent", "probe": "x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "reinstall everything",
	     "tainted_by": ["t3.cause"]}
	  ]
	}`, evidenceWith(probeRec("t3.cause", "present", 0)+`,`+probeRec("t3.dep", "ok", 0)))

	if r := verdictFor(t, rep, "t3.dep").Remedy; r != "" {
		t.Errorf("tainted check carries remedy %q; the fix belongs to the prerequisite", r)
	}
	if r := verdictFor(t, rep, "t3.cause").Remedy; r != "fix the cause" {
		t.Errorf("the cause should keep its remedy, got %q", r)
	}
}

// A GO prerequisite taints nothing.
func TestPassingPrerequisiteDoesNotTaint(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t4.cause", "title": "Fine", "probe": "x",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"},
	    {"id": "t4.dep", "title": "Dependent", "probe": "x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info", "tainted_by": ["t4.cause"]}
	  ]
	}`, evidenceWith(probeRec("t4.cause", "ok", 0)+`,`+probeRec("t4.dep", "ok", 0)))

	if got := verdictFor(t, rep, "t4.dep").State; got != StateGo {
		t.Errorf("dependent = %s, want GO: its prerequisite passed", got)
	}
}

// ============================================================
// N/A and missing evidence
// ============================================================

func TestNotApplicable(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "n.pnpm", "title": "pnpm lockfile", "applies_if": "test -f pnpm-lock.yaml",
	     "probe": "pnpm -v",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "warning", "remedy": "corepack enable"}
	  ]
	}`, evidenceWith(`{"id": "n.pnpm", "applies": false,
	   "applies_if": {"exit": 1, "duration_ms": 2}}`))

	v := verdictFor(t, rep, "n.pnpm")
	if v.State != StateNA {
		t.Errorf("state = %s, want N/A", v.State)
	}
	// N/A is not a failure and must not affect the exit code.
	if rep.ExitCode() != exitGo {
		t.Error("an N/A check changed the exit code")
	}
}

// A manifest check with no evidence must not silently vanish. A sweep that
// grew a check since the evidence was captured has not verified it.
func TestMissingEvidenceIsUnknown(t *testing.T) {
	rep := evaluate(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "m.new", "title": "Added after the sweep", "probe": "x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "re-run"}
	  ]
	}`, evidenceWith(``))

	v := verdictFor(t, rep, "m.new")
	if v.State != StateUnknown {
		t.Errorf("state = %s, want UNKNOWN", v.State)
	}
	if v.Reason != ReasonExpired {
		t.Errorf("reason = %q, want %q", v.Reason, ReasonExpired)
	}
}

// Evidence from a future schema version is refused rather than misread.
func TestSchemaVersionMismatchIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "00.json"), `{"layer": 0, "checks": [
	  {"id": "v.x", "title": "t", "probe": "x",
	   "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"}]}`)
	m, _, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Evaluate(m, &Evidence{SchemaVersion: 99})
	if err == nil {
		t.Fatal("evidence from an unknown schema version was accepted")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error should name the mismatch: %v", err)
	}
}

// ============================================================
// Exit codes
// ============================================================

func TestExitCodes(t *testing.T) {
	cases := []struct {
		name     string
		verdicts []Verdict
		want     int
	}{
		{"all green", []Verdict{{State: StateGo, Severity: "blocker", Decisive: true}}, exitGo},
		{"blocker fails", []Verdict{{State: StateNoGo, Severity: "blocker", Decisive: true}}, exitNoGo},
		{"warning fails", []Verdict{{State: StateNoGo, Severity: "warning", Decisive: false}}, exitGo},
		{"decisive unknown", []Verdict{{State: StateUnknown, Decisive: true}}, exitUnknown},
		{"blocker beats unknown", []Verdict{
			{State: StateNoGo, Severity: "blocker", Decisive: true},
			{State: StateUnknown, Decisive: true},
		}, exitNoGo},
		{"na is neutral", []Verdict{{State: StateNA, Decisive: true}}, exitGo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Report{Verdicts: tc.verdicts}
			if got := r.ExitCode(); got != tc.want {
				t.Errorf("exit = %d, want %d", got, tc.want)
			}
		})
	}
}

// ============================================================
// The committed fixture
// ============================================================

// PLAN.md M2 exit criterion: verdict.go runs standalone against a committed
// fixture evidence.json and produces stable output.
func TestFixtureEvidenceProducesStableVerdicts(t *testing.T) {
	m, _, err := LoadManifest(filepath.Join(fixtures, "verdict", "manifest"))
	if err != nil {
		t.Fatalf("load fixture manifest: %v", problemsOf(err))
	}

	raw, err := os.ReadFile(filepath.Join(fixtures, "verdict", "evidence.json"))
	if err != nil {
		t.Fatalf("read fixture evidence: %v", err)
	}
	var ev Evidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("parse fixture evidence: %v", err)
	}

	rep, err := Evaluate(m, &ev)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	want := map[string]State{
		"fx.clock.skew":    StateNoGo,
		"fx.arch":          StateGo,
		"fx.node.version":  StateUnknown, // tainted by the clock
		"fx.registry":      StateUnknown, // tainted, transitively
		"fx.pnpm.lockfile": StateNA,
		"fx.disk.free":     StateGo,
		"fx.tool.missing":  StateUnknown,
		"fx.proxy.absent":  StateGo,
	}

	for id, wantState := range want {
		got := verdictFor(t, rep, id)
		if got.State != wantState {
			t.Errorf("%s = %s, want %s (detail: %s)", id, got.State, wantState, got.Detail)
		}
	}

	// Stable: evaluating twice gives the same answer.
	rep2, err := Evaluate(m, &ev)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rep.Verdicts {
		if rep.Verdicts[i].State != rep2.Verdicts[i].State {
			t.Errorf("%s is not stable across runs", rep.Verdicts[i].ID)
		}
	}

	// One blocker failed, so the sweep is NO-GO.
	if got := rep.ExitCode(); got != exitNoGo {
		t.Errorf("exit = %d, want %d", got, exitNoGo)
	}
}
