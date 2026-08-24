// Secret fixtures, asserted absent from every output.
//
// ON THE FIXTURE VALUES. AGENTS.md rule 5 forbids writing a secret -- "a
// realistic-looking fake is still a string someone will grep for and mistake
// for real." So these fixtures carry a real credential's SHAPE (a recognised
// prefix, a plausible length) attached to a body that says, in words, that it
// is not a credential. That keeps the test exercising the same code path as a
// real token while leaving nothing in the repo that reads as one.
//
// The plan (M5) originally called for realistic fakes. AGENTS.md is newer and
// stronger, and it wins; PLAN.md records the divergence.
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sentinels. Shaped like credentials, worded so nobody mistakes them.
const (
	sentinelToken  = "ghp_FERRET_TEST_SENTINEL_NOT_A_REAL_TOKEN_00"
	sentinelNpm    = "npm_FERRET_TEST_SENTINEL_NOT_A_REAL_TOKEN_01"
	sentinelInURL  = "FERRET_TEST_SENTINEL_NOT_A_REAL_PASSWORD_02"
	sentinelPrefix = "FERRET_TEST_SENTINEL"
)

// secretLeakScan is the assertion every test in this file ends with: no
// sentinel, in any form, in any output Ferret produced.
func assertNoSentinel(t *testing.T, label, content string) {
	t.Helper()
	if strings.Contains(content, sentinelPrefix) {
		// Print the surrounding line only -- enough to locate the leak,
		// without dumping the whole artefact into the test log.
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, sentinelPrefix) {
				t.Errorf("%s leaked a secret-shaped value: %s", label, line)
			}
		}
	}
}

// A check whose probe would put a credential on stdout is refused before it
// runs, and nothing resembling the value reaches evidence.json.
func TestSecretProbeIsRefusedAndNothingLeaks(t *testing.T) {
	t.Setenv("FERRET_TEST_TOKEN", sentinelToken)

	r := &Runner{
		Manifest: &Manifest{Checks: []Check{{
			ID:    "secret.printenv",
			Title: "would read a token",
			Probe: Command{Set: true, Script: "printenv FERRET_TEST_TOKEN"},
		}}},
		WorkDir: t.TempDir(),
		Version: "test",
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	rec := ev.Records[0]
	if rec.Error == nil || rec.Error.Kind != errUnverifiable {
		t.Fatalf("want UNKNOWN(unverifiable), got %+v", rec.Error)
	}
	// The refusal must not have run the command.
	if rec.Probe != nil && rec.Probe.Exit == 0 {
		t.Error("a refused probe recorded a zero exit; that reads as ran-and-succeeded")
	}

	assertNoSentinel(t, "evidence record", mustMarshal(t, ev))
}

// The backstop: if a probe somehow captures a credential anyway -- an
// authoring mistake the refusal did not catch -- redaction must still keep the
// value out of the evidence, and presence must survive.
//
// This tests applyRedaction directly rather than through a probe, and
// deliberately so. Every probe script that produces a credential value which
// I could construct was caught by readsSecret first, so a runner-level version
// of this test passes with an EMPTY capture -- asserting nothing while looking
// thorough. Defence in depth is working; that is exactly what makes the second
// layer hard to reach from outside, so it gets tested where it lives.
func TestCapturedSecretIsShapedNotStored(t *testing.T) {
	shaped := applyRedaction(sentinelToken, RedactSecret, RedactNone)

	assertNoSentinel(t, "shaped capture", shaped)

	// Presence must survive, or a non_empty expectation silently flips to
	// NO-GO on a value that was in fact set.
	if !strings.HasPrefix(shaped, "<set,") {
		t.Errorf("shaped capture should record presence, got %q", shaped)
	}
	// The recognisable prefix is kept for diagnosis; the body is not.
	if !strings.Contains(shaped, "ghp_") {
		t.Errorf("the token prefix should survive for diagnosis, got %q", shaped)
	}
}

// And the same backstop over a multi-line capture, which is the realistic
// shape of an accidental .npmrc or `env` dump: structure survives, values do
// not.
func TestShapedLinesKeepNoSentinel(t *testing.T) {
	in := "registry=https://registry.npmjs.org/\n" +
		"//registry.npmjs.org/:_authToken=" + sentinelNpm + "\n" +
		"ignore-scripts=true\n" +
		"@myscope:registry=https://npm.pkg.github.com/\n"

	got := shapeLines(in)

	assertNoSentinel(t, "shaped lines", got)
	if !strings.Contains(got, "_authToken=") {
		t.Error("the key should survive, so the reader knows what was found")
	}

	// The non-secret settings must survive with their VALUES, not just their
	// keys. PLAN.md M7 checks each of these; shaped, no equals or matches
	// expectation could evaluate them and the checks become unimplementable.
	for _, want := range []string{
		"registry=https://registry.npmjs.org/",
		"ignore-scripts=true",
		"@myscope:registry=https://npm.pkg.github.com/",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a non-secret setting was shaped away; want %q in:\n%s", want, got)
		}
	}
}

// The other half of that rule: a credential-keyed value is still shaped, under
// whatever spelling. Loosening shapeLines to protect non-secret settings must
// not loosen it for secrets.
func TestCredentialKeysAreStillShaped(t *testing.T) {
	for _, key := range []string{
		"_authToken", "NPM_TOKEN", "GITHUB_API_KEY", "AWS_SECRET_ACCESS_KEY",
		"password", "MY_PRIVATE_KEY", "session_cookie",
	} {
		got := shapeLines(key + "=" + sentinelToken)
		if strings.Contains(got, sentinelPrefix) {
			t.Errorf("a credential-keyed value survived shaping: %s", got)
		}
		if !strings.Contains(got, key+"=") {
			t.Errorf("the key itself should survive: %s", got)
		}
	}
}

// An .npmrc: the file is never opened, only counted. This is the one-character
// difference PLAN.md M7 calls out -- grep -c versus grep.
func TestNpmrcIsCountedNeverRead(t *testing.T) {
	dir := t.TempDir()
	npmrc := filepath.Join(dir, ".npmrc")
	content := "registry=https://registry.npmjs.org/\n" +
		"//registry.npmjs.org/:_authToken=" + sentinelNpm + "\n"
	if err := os.WriteFile(npmrc, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		Manifest: &Manifest{Checks: []Check{
			{
				ID:    "npm.auth.present",
				Title: "auth line present",
				Probe: Command{Set: true, Script: "grep -c '_authToken' .npmrc"},
			},
			{
				ID:    "npm.registry",
				Title: "registry setting",
				Probe: Command{Set: true, Script: "grep -o '^registry=.*' .npmrc"},
			},
			{
				ID:    "npm.leak",
				Title: "would read the auth line",
				Probe: Command{Set: true, Script: "grep '_authToken=.*' .npmrc"},
			},
		}},
		WorkDir: dir,
		Version: "test",
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	byID := map[string]Record{}
	for _, rec := range ev.Records {
		byID[rec.ID] = rec
	}

	// The counting form is allowed and answers the question.
	if rec := byID["npm.auth.present"]; rec.Error != nil {
		t.Errorf("grep -c should be permitted: %+v", rec.Error)
	} else if !strings.Contains(rec.Probe.Stdout, "1") {
		t.Errorf("want a count of 1, got %q", rec.Probe.Stdout)
	}

	// The named non-secret line is allowed.
	if rec := byID["npm.registry"]; rec.Error != nil {
		t.Errorf("an explicitly named non-secret line should be permitted: %+v", rec.Error)
	}

	// The value-bearing form is refused.
	if rec := byID["npm.leak"]; rec.Error == nil || rec.Error.Kind != errUnverifiable {
		t.Errorf("a grep whose match includes the token must be refused, got %+v", rec.Error)
	}

	assertNoSentinel(t, "evidence.json", mustMarshal(t, ev))
}

// End to end, through the renderer: the sweep's own artefacts -- evidence.json
// AND status.md -- must both be clean. The plan says "both outputs", and
// status.md is the one a user pastes into a chat.
func TestSecretsReachNeitherEvidenceNorStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FERRET_TEST_TOKEN", sentinelToken)

	// A git remote with embedded credentials, per the plan's third fixture.
	if err := os.WriteFile(filepath.Join(dir, "remotes.txt"),
		[]byte("origin https://user:"+sentinelInURL+"@example.invalid/repo.git\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{Checks: []Check{
		{
			ID:       "secret.env",
			Title:    "token is set",
			Probe:    Command{Set: true, Script: `[ -n "${FERRET_TEST_TOKEN:-}" ] && echo set || echo unset`},
			Expect:   Expect{Type: "equals", Raw: map[string]any{"type": "equals", "value": "set"}},
			Severity: "info",
		},
		{
			ID:    "secret.remote",
			Title: "remote credentials stripped at capture",
			// Strips before capture, per AGENTS.md's permitted list.
			Probe:       Command{Set: true, Script: `sed 's|://[^@]*@|://<redacted>@|' remotes.txt`},
			Expect:      Expect{Type: "non_empty", Raw: map[string]any{"type": "non_empty"}},
			Severity:    "info",
			RedactLevel: RedactSecret,
		},
	}}

	r := &Runner{Manifest: m, WorkDir: dir, Version: "test"}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	evidenceBlob := mustMarshal(t, ev)
	assertNoSentinel(t, "evidence.json", evidenceBlob)

	rep, err := Evaluate(m, ev)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var stdout, status bytes.Buffer
	// Constructed directly rather than via NewRenderer, which wants an
	// *os.File to sniff for a TTY. Colour off keeps the assertion independent
	// of where the suite runs.
	(&Renderer{Width: 78}).Render(&stdout, rep)
	RenderMarkdown(&status, rep, ev.StartedAt)

	assertNoSentinel(t, "stdout glance", stdout.String())
	assertNoSentinel(t, "status.md", status.String())
}

// TestSecretRedactionKeepsMeasurements pins the M7 dispatch bug.
//
// applyRedaction called shapeOf directly, so shapeLines -- the function
// written precisely so that `registry=` and `ignore-scripts=` survive
// redaction -- was unreachable from the only path that reaches it in
// production. Every secret-level capture became `<set, N chars, hash …>`,
// including a bare count. `grep -c '_authToken' .npmrc` returned `1`, which
// was shaped, so an `equals "0"` expectation could not evaluate: the check
// could neither pass nor honestly fail.
//
// It was found by dumping evidence.json for a real repo and reading the
// captured values, not by any test -- which is why there is now a test.
//
// Both directions are asserted. A measurement that gets shaped is a broken
// check; a credential that does not is a leak.
func TestSecretRedactionKeepsMeasurements(t *testing.T) {
	keep := []string{
		"0",
		"1",
		"present",
		"absent",
		"v24.13.1",
		"registry=https://registry.npmjs.org/",
		"ignore-scripts=true",
	}
	for _, in := range keep {
		if got := applyRedaction(in, RedactSecret, RedactNone); got != in {
			t.Errorf("applyRedaction(%q) = %q; a measurement must survive "+
				"redaction or its expectation can never evaluate", in, got)
		}
	}

	shaped := []string{
		sentinelNpm,
		"//registry.npmjs.org/:_authToken=" + sentinelNpm,
		"NPM_TOKEN=" + sentinelNpm,
	}
	for _, in := range shaped {
		got := applyRedaction(in, RedactSecret, RedactNone)
		if got == in {
			t.Errorf("applyRedaction(%q) returned it unchanged; a credential "+
				"must be shaped", in)
		}
		assertNoSentinel(t, "shaped credential", got)
	}
}

func mustMarshal(t *testing.T, ev *Evidence) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.json")
	if err := WriteEvidence(path, ev); err != nil {
		t.Fatalf("writing evidence: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading evidence: %v", err)
	}
	return string(b)
}
