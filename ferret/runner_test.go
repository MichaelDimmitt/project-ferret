package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helpers shared with manifest_test.go

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func link(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", from, err)
	}
	write(t, to, string(b))
}

func problemsOf(err error) string {
	if ve, ok := err.(*ValidationError); ok {
		return strings.Join(ve.Problems, "\n")
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// runManifest loads an inline manifest and runs it, returning the evidence.
func runManifest(t *testing.T, jsonBody string) *Evidence {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "00-test.json"), jsonBody)

	m, _, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("load: %v", problemsOf(err))
	}

	r := &Runner{
		Manifest: m,
		WorkDir:  dir,
		Version:  "test",
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return ev
}

func recordFor(t *testing.T, ev *Evidence, id string) Record {
	t.Helper()
	for _, r := range ev.Records {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no record for %s", id)
	return Record{}
}

// M1 exit criterion: three hand-written checks produce a valid evidence.json.
func TestThreeChecksProduceEvidence(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.echo", "title": "Echo works", "probe": "echo hello",
	     "expect": {"type": "equals", "actual": "$probe", "value": "hello"},
	     "severity": "info"},
	    {"id": "t.declared", "title": "Declared vs actual",
	     "probe": "echo 18", "declared": "echo 20",
	     "expect": {"type": "equals", "actual": "$probe", "value": "$declared"},
	     "severity": "info"},
	    {"id": "t.nonzero", "title": "Non-zero exit is data, not an error",
	     "probe": "exit 7",
	     "expect": {"type": "exit_code", "of": "probe", "value": 0},
	     "severity": "info"}
	  ]
	}`)

	if ev.SchemaVersion != evidenceSchemaVersion {
		t.Errorf("schema_version = %d, want %d", ev.SchemaVersion, evidenceSchemaVersion)
	}
	if len(ev.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(ev.Records))
	}

	echo := recordFor(t, ev, "t.echo")
	if strings.TrimSpace(echo.Probe.Stdout) != "hello" {
		t.Errorf("stdout = %q, want hello", echo.Probe.Stdout)
	}
	if echo.Error != nil {
		t.Errorf("unexpected error: %+v", echo.Error)
	}

	decl := recordFor(t, ev, "t.declared")
	if decl.Declared == nil {
		t.Fatal("declared was not captured")
	}
	if strings.TrimSpace(decl.Declared.Stdout) != "20" {
		t.Errorf("declared stdout = %q, want 20", decl.Declared.Stdout)
	}

	// A non-zero exit is data. The runner does not know that 7 is bad,
	// because it does not know what the check wanted.
	nz := recordFor(t, ev, "t.nonzero")
	if nz.Probe.Exit != 7 {
		t.Errorf("exit = %d, want 7", nz.Probe.Exit)
	}
	if nz.Error != nil {
		t.Errorf("a non-zero exit must not be an error, got %+v", nz.Error)
	}

	// It must round-trip as JSON: this file is the contract M2 reads.
	if _, err := json.Marshal(ev); err != nil {
		t.Fatalf("evidence does not marshal: %v", err)
	}
}

// M1 exit criterion: a deliberately mutating probe is refused.
func TestMutatingProbeIsRefused(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.mutating", "title": "Would fetch", "probe": "git fetch origin",
	     "expect": {"type": "exit_code", "of": "probe", "value": 0},
	     "severity": "info"}
	  ]
	}`)

	rec := recordFor(t, ev, "t.mutating")
	if rec.Error == nil {
		t.Fatal("a mutating probe must be refused")
	}
	if rec.Error.Kind != errProbeError {
		t.Errorf("kind = %q, want %q", rec.Error.Kind, errProbeError)
	}
	if !strings.Contains(rec.Error.Detail, "fetch") {
		t.Errorf("the refusal must name the matched token: %s", rec.Error.Detail)
	}
	if rec.Probe.DurationMs > 100 {
		t.Error("a refused probe should not have executed")
	}
	// Not 0: a refused probe did not run, and a zero exit reads as
	// "ran, succeeded" to anything scanning exit codes.
	if rec.Probe.Exit != -1 {
		t.Errorf("refused probe exit = %d, want -1", rec.Probe.Exit)
	}
}

// The override exists so a denylist does not produce workarounds worse than
// the rule. git config --get is read-only despite matching on "set"... and on
// "config", which is why the override is required rather than optional.
func TestMutatingOverrideAllowsExecution(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.override", "title": "Reads config",
	     "probe": "echo reset-is-in-this-string",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info",
	     "mutating": false,
	     "mutating_why": "echo writes nothing; the token appears in the literal"}
	  ]
	}`)

	rec := recordFor(t, ev, "t.override")
	if rec.Error != nil {
		t.Fatalf("the override should permit execution, got %+v", rec.Error)
	}
	if !strings.Contains(rec.Probe.Stdout, "reset-is-in-this-string") {
		t.Errorf("probe did not run: %q", rec.Probe.Stdout)
	}
}

// M1 exit criterion: a deliberately hanging probe times out and records
// UNKNOWN(timeout).
func TestHangingProbeTimesOut(t *testing.T) {
	start := time.Now()
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.hang", "title": "Hangs forever", "probe": "sleep 30",
	     "expect": {"type": "exit_code", "of": "probe", "value": 0},
	     "severity": "info", "timeout_s": 0.5}
	  ]
	}`)
	elapsed := time.Since(start)

	rec := recordFor(t, ev, "t.hang")
	if rec.Error == nil || rec.Error.Kind != errTimeout {
		t.Fatalf("want a timeout error, got %+v", rec.Error)
	}
	if !rec.Probe.TimedOut {
		t.Error("timed_out flag not set")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout was not enforced: took %v", elapsed)
	}
}

// A probe that spawns a child must not outlive its timeout. Killing only the
// direct child leaves the grandchild holding the pipe, which is the hang the
// timeout exists to prevent.
func TestTimeoutKillsProcessGroup(t *testing.T) {
	start := time.Now()
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.group", "title": "Spawns a child that outlives it",
	     "probe": "sh -c 'sleep 30' & wait",
	     "expect": {"type": "exit_code", "of": "probe", "value": 0},
	     "severity": "info", "timeout_s": 0.5}
	  ]
	}`)
	elapsed := time.Since(start)

	rec := recordFor(t, ev, "t.group")
	if rec.Error == nil || rec.Error.Kind != errTimeout {
		t.Fatalf("want a timeout, got %+v", rec.Error)
	}
	if elapsed > 5*time.Second {
		t.Errorf("process group outlived the timeout: took %v", elapsed)
	}
}

// applies_if exiting non-zero cleanly means N/A: the question is not asked
// here. The probe must not run.
func TestAppliesIfFalseIsNotApplicable(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.na", "title": "Does not apply",
	     "applies_if": "test -f definitely-not-here.json",
	     "probe": "echo should-not-run",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info"}
	  ]
	}`)

	rec := recordFor(t, ev, "t.na")
	if rec.Applies {
		t.Error("applies should be false")
	}
	if rec.Probe != nil {
		t.Errorf("the probe must not run when applies_if is false, got %+v", rec.Probe)
	}
	if rec.Error != nil {
		t.Errorf("N/A is not an error: %+v", rec.Error)
	}
}

// The distinction that matters: a BROKEN gate is UNKNOWN, never N/A. Silently
// converting a broken gate into "doesn't apply here" is a false green wearing
// a different hat.
func TestBrokenAppliesIfIsUnknownNotNA(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.broken_gate", "title": "Gate hangs",
	     "applies_if": "sleep 30",
	     "probe": "echo x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info", "timeout_s": 0.5}
	  ]
	}`)

	rec := recordFor(t, ev, "t.broken_gate")
	if rec.Error == nil {
		t.Fatal("a broken gate must record an error, not a silent N/A")
	}
	if rec.Error.Kind != errTimeout {
		t.Errorf("kind = %q, want %q", rec.Error.Kind, errTimeout)
	}
}

// THE INVARIANT. The runner records no verdict, because it makes none. If a
// state or passed key ever appears in this file, the design is broken.
func TestEvidenceContainsNoVerdict(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.v", "title": "Anything", "probe": "echo x",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "blocker", "remedy": "none",
	     "tainted_by": []}
	  ]
	}`)

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	recs := generic["records"].([]any)
	rec := recs[0].(map[string]any)

	// Verdict-shaped keys, and the manifest fields the runner must not copy
	// through: knowing what counts as passing is the interpretation it is
	// forbidden to do.
	forbidden := []string{
		"state", "verdict", "passed", "ok", "result", "go", "status",
		"expect", "severity", "remedy", "tainted_by", "decisive",
	}
	for _, k := range forbidden {
		if _, present := rec[k]; present {
			t.Errorf("evidence record contains %q; the runner has started interpreting", k)
		}
	}
}

// The runner exits 0 when it captured what it was asked to, and 3 when it
// could not. It never exits 1 or 2 -- those are verdicts.
func TestRunnerReportsNoVerdictOnFailedProbes(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.fails", "title": "Fails hard", "probe": "exit 1",
	     "expect": {"type": "exit_code", "of": "probe", "value": 0},
	     "severity": "blocker", "remedy": "fix it"}
	  ]
	}`)

	// A failing probe is a complete, successful capture.
	rec := recordFor(t, ev, "t.fails")
	if rec.Probe.Exit != 1 {
		t.Errorf("exit = %d, want 1", rec.Probe.Exit)
	}
	if rec.Error != nil {
		t.Errorf("a failing probe is data, not a runner error: %+v", rec.Error)
	}
}

// Records appear in manifest order. Ordering by result would require knowing
// results.
func TestRecordsAreInManifestOrder(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.c", "title": "third", "probe": "exit 1",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"},
	    {"id": "t.a", "title": "first", "probe": "echo a",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"},
	    {"id": "t.b", "title": "second", "probe": "echo b",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"}
	  ]
	}`)

	want := []string{"t.c", "t.a", "t.b"}
	for i, id := range want {
		if ev.Records[i].ID != id {
			t.Errorf("record %d = %s, want %s", i, ev.Records[i].ID, id)
		}
	}
}

func TestToolAbsentHeuristic(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "t.absent", "title": "Missing tool",
	     "probe": "definitely-not-a-real-command-xyzzy",
	     "expect": {"type": "non_empty", "actual": "$probe"}, "severity": "info"}
	  ]
	}`)

	rec := recordFor(t, ev, "t.absent")
	if rec.Error == nil || rec.Error.Kind != errToolAbsent {
		t.Fatalf("want tool_absent, got %+v", rec.Error)
	}
}

// AGENTS.md § "Never read a secret". The guarantee is that the value is never
// read -- not that it is redacted afterwards -- so the refusal must happen
// before execution.
func TestSecretReadingProbesAreRefused(t *testing.T) {
	forbidden := []string{
		"printenv GH_TOKEN",
		"printenv GITHUB_TOKEN || printenv GH_TOKEN",
		"echo $NPM_TOKEN",
		"echo \"$AWS_SECRET_ACCESS_KEY\"",
		"cat ~/.npmrc",
		"cat .env",
		"head -1 ~/.netrc",
		"cat ~/.ssh/id_rsa",
		"cat /etc/ssl/private/server.key",
		"grep '_authToken=' .npmrc",
		"awk -F= '{print $2}' .npmrc",
	}
	for _, script := range forbidden {
		if got := readsSecret(script); got == "" {
			t.Errorf("readsSecret(%q) = allowed; this reads a secret value", script)
		}
	}

	allowed := []string{
		`if [ -n "${GH_TOKEN:-}" ]; then echo set; fi`,
		`test -n "$GH_TOKEN" && echo set`,
		"test -f ~/.npmrc",
		"stat -f '%Sp' ~/.npmrc",
		"grep -c '_authToken' ~/.npmrc",
		"grep -o '^registry=.*' .npmrc",
		"printenv PATH",
		"printenv HOME",
		"echo $PATH",
		"git remote -v",
	}
	for _, script := range allowed {
		if got := readsSecret(script); got != "" {
			t.Errorf("readsSecret(%q) = refused on %q; this does not read a value",
				script, got)
		}
	}
}

// The refusal must be structural: no execution, and no override to waive it.
func TestSecretReadRefusalHappensBeforeExecution(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "s.reads", "title": "Would read a token",
	     "probe": "printenv GH_TOKEN",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "warning", "remedy": "n/a", "redact": "secret"}
	  ]
	}`)

	rec := recordFor(t, ev, "s.reads")
	if rec.Error == nil {
		t.Fatal("a secret-reading probe must be refused")
	}
	if rec.Error.Kind != errUnverifiable {
		t.Errorf("kind = %q, want %q", rec.Error.Kind, errUnverifiable)
	}
	if rec.Probe.DurationMs > 100 {
		t.Error("the probe executed; the refusal must come first")
	}
	if rec.Probe.Exit != -1 {
		t.Errorf("exit = %d, want -1: the probe never ran", rec.Probe.Exit)
	}
	if rec.Probe.Stdout != "" {
		t.Errorf("stdout is non-empty (%q); nothing should have been captured",
			rec.Probe.Stdout)
	}
}

// Unlike the mutation denylist, this has no escape hatch. mutating: false must
// not waive it -- the two rules are independent, and a probe that reads a
// secret is forbidden however read-only it is.
func TestMutatingOverrideDoesNotWaiveSecretRule(t *testing.T) {
	ev := runManifest(t, `{
	  "layer": 0,
	  "checks": [
	    {"id": "s.override", "title": "Claims read-only, still reads a secret",
	     "probe": "cat ~/.npmrc",
	     "expect": {"type": "non_empty", "actual": "$probe"},
	     "severity": "info",
	     "mutating": false,
	     "mutating_why": "cat only reads"}
	  ]
	}`)

	rec := recordFor(t, ev, "s.override")
	if rec.Error == nil || rec.Error.Kind != errUnverifiable {
		t.Fatalf("mutating: false waived the secret rule; got %+v", rec.Error)
	}
}

// The committed manifest fixtures must model the rule, since they double as
// the worked reference for anyone adding checks.
func TestFixtureProbesDoNotReadSecrets(t *testing.T) {
	m, _, err := LoadManifest(filepath.Join(fixtures, "manifest-valid"))
	if err != nil {
		t.Fatalf("load: %v", problemsOf(err))
	}
	for i := range m.Checks {
		c := &m.Checks[i]
		for _, cmd := range []struct {
			name string
			s    string
		}{
			{"probe", c.Probe.Script},
			{"declared", c.Declared.Script},
			{"applies_if", c.AppliesIf.Script},
		} {
			if got := readsSecret(cmd.s); got != "" {
				t.Errorf("%s: %s reads a secret via %q", c.ID, cmd.name, got)
			}
		}
	}
}

func TestDenylistAllowsReadOnlyRedirects(t *testing.T) {
	cases := []struct {
		script string
		denied bool
	}{
		{"echo hi", false},
		{"grep -q x file 2>/dev/null", false},
		{"echo err >&2", false},
		{"git fetch origin", true},
		{"npm install", true},
		{"echo x > /tmp/file", true},
		{"sudo something", true},
		{"cat file", false},
		{"git rev-parse --git-dir", false},
		{"df -k .", false},
	}
	for _, tc := range cases {
		got := deniedBy(tc.script) != ""
		if got != tc.denied {
			t.Errorf("deniedBy(%q) = %v (%q), want denied=%v",
				tc.script, got, deniedBy(tc.script), tc.denied)
		}
	}
}
