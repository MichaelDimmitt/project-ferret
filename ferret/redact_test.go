package main

import (
	"os"
	"strings"
	"testing"
)

// Presence must survive redaction by construction. MANIFEST_SCHEMA.md §9 has
// a non_empty check on a secret-redacted capture, and it only works because
// the shape string is non-empty whenever the value was.
func TestSecretRedactionPreservesPresence(t *testing.T) {
	got := shapeOf("ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	if got == "" {
		t.Fatal("a set value must not redact to empty; non_empty would flip to NO-GO")
	}
	if !strings.HasPrefix(got, "<set,") {
		t.Errorf("shape should lead with <set, got %q", got)
	}
	if shapeOf("") != "" {
		t.Error("an unset value must stay empty")
	}
	if shapeOf("   ") != "" {
		t.Error("a whitespace-only value must read as unset")
	}
}

// The value itself must never survive.
func TestSecretRedactionDropsTheValue(t *testing.T) {
	secret := "ghp_verysecrettokenvalue0123456789abcd"
	got := shapeOf(secret)
	if strings.Contains(got, secret) {
		t.Fatal("the raw token survived redaction")
	}
	if strings.Contains(got, "verysecret") {
		t.Fatal("part of the token survived redaction")
	}
	if !strings.Contains(got, "ghp_") {
		t.Errorf("the recognisable prefix should be kept for diagnosis: %q", got)
	}
}

// Same value, same hash: "is this the same token as before" is answerable
// without exposing it.
func TestSecretHashIsStable(t *testing.T) {
	a := shapeOf("sk-1234567890abcdefghij")
	b := shapeOf("sk-1234567890abcdefghij")
	c := shapeOf("sk-0987654321zyxwvutsrq")
	if a != b {
		t.Errorf("same value hashed differently:\n%s\n%s", a, b)
	}
	if a == c {
		t.Error("different values hashed identically")
	}
}

// Identity redaction must agree with scripts/lib/redact.sh: strip who you are,
// keep what the machine is.
func TestIdentityRedaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FERRET_REPO", home+"/work/project")

	in := home + "/work/project/node_modules/.bin/tsc"
	got := redactIdentity(in)

	if strings.Contains(got, home) {
		t.Errorf("home survived redaction: %q", got)
	}
	if !strings.Contains(got, "<repo>") {
		t.Errorf("repo path should become <repo>: %q", got)
	}
	// The tail is the diagnostic part and must survive.
	if !strings.Contains(got, "node_modules/.bin/tsc") {
		t.Errorf("the actionable part of the path was lost: %q", got)
	}
}

// Longest-first ordering: the repo usually sits under HOME, and substituting
// the shorter one first leaves a half-redacted path.
func TestRepoBeatsHomeWhenNested(t *testing.T) {
	home := t.TempDir()
	repo := home + "/src/ferret"
	t.Setenv("HOME", home)
	t.Setenv("FERRET_REPO", repo)

	got := redactIdentity(repo + "/manifest/00-machine.json")
	if !strings.HasPrefix(got, "<repo>") {
		t.Errorf("want <repo> prefix, got %q", got)
	}
	if strings.Contains(got, "~/src/ferret") {
		t.Errorf("half-redacted: %q", got)
	}
}

// The stricter level wins, in both directions. A run-wide identity setting
// must not weaken a check that declared itself secret.
func TestStricterLevelWins(t *testing.T) {
	secret := "ghp_abcdefghijklmnopqrstuvwxyz012345"

	got := applyRedaction(secret, RedactSecret, RedactIdentity)
	if strings.Contains(got, secret) {
		t.Error("check-level secret was weakened by a run-wide identity setting")
	}

	got = applyRedaction(secret, RedactNone, RedactSecret)
	if strings.Contains(got, secret) {
		t.Error("run-wide secret did not apply to a check that declared nothing")
	}

	// And with nothing set, capture is verbatim.
	if applyRedaction("plain value", RedactNone, RedactNone) != "plain value" {
		t.Error("unredacted capture should be verbatim")
	}
}

// An .npmrc or env dump: structure survives, values do not.
func TestShapeLinesKeepsStructure(t *testing.T) {
	in := "//registry.npmjs.org/:_authToken=npm_abcdefghijklmnopqrstuvwxyz012345\n" +
		"registry=https://registry.npmjs.org/"
	got := shapeLines(in)

	if strings.Contains(got, "npm_abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatal("the auth token survived")
	}
	if !strings.Contains(got, "_authToken=") {
		t.Error("the key should survive so the reader knows what was found")
	}
}

// Whatever the salt is derived from, it must be stable within a run.
func TestSaltIsStable(t *testing.T) {
	a := string(redactionSalt())
	b := string(redactionSalt())
	if a != b {
		t.Error("salt is not stable; hashes would not compare across checks")
	}
	if a == "" {
		t.Error("empty salt")
	}
}

func TestRedactLevelUnmarshal(t *testing.T) {
	cases := []struct {
		in      string
		want    Redact
		wantErr bool
	}{
		{`false`, RedactNone, false},
		{`true`, RedactIdentity, false},
		{`"identity"`, RedactIdentity, false},
		{`"secret"`, RedactSecret, false},
		{`"paranoid"`, "", true}, // shell has this level; the schema does not
		{`3`, "", true},
	}
	for _, tc := range cases {
		var r Redact
		err := r.UnmarshalJSON([]byte(tc.in))
		if tc.wantErr {
			if err == nil {
				t.Errorf("UnmarshalJSON(%s) should have failed", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", tc.in, err)
			continue
		}
		if r != tc.want {
			t.Errorf("UnmarshalJSON(%s) = %q, want %q", tc.in, r, tc.want)
		}
	}
}

// Secrets must not reach the raw debug directory either. A debug dir is still
// somewhere.
func TestRawDirSkipsSecrets(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{RawDir: dir}
	r.writeRaw("some.check", "probe", "ghp_secretvalue", "", RedactSecret)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("secret-level output was written to the raw dir: %v", entries)
	}
}
