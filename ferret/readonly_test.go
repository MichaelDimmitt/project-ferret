// Phase 1 is read-only. This file asserts it against a real repository rather
// than against the denylist that is supposed to enforce it.
//
// The distinction matters. runner.go's denylist is a regex, and its own
// comment says it is not a sandbox: a probe determined to mutate can trivially
// evade it. Testing the denylist tests the regex. Testing the repository tests
// the guarantee -- and the guarantee is what the user was promised.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureRepo builds a small git repository in a temp dir.
//
// Built at test time rather than committed: a nested .git in the tree is
// awkward to commit, easy to corrupt, and this is a few lines.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# fixture\n")
	write("package.json", `{"name":"fixture","version":"1.0.0"}`+"\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "Ferret Fixture"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-q", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// A fresh environment, so the developer's own git config -- hooks,
		// templates, signing -- cannot alter the fixture out from under us.
		cmd.Env = append(isolatedEnv(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable or unusable (%v): %s", err, out)
		}
	}
	return dir
}

// fingerprint hashes every file under dir, path and content together.
//
// Paths are included so that a probe which deletes one file and creates
// another of the same size is still caught. .git is included deliberately:
// the index and config are exactly where a mutating probe would show up, and
// excluding them would exempt the most likely failure.
func fingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// Volatile git bookkeeping that changes on read, not on mutation.
		// Excluding these keeps the test honest rather than flaky: a false
		// failure here would get the whole test disabled, which is worse than
		// the narrower assertion.
		for _, skip := range []string{".git/logs", ".git/FETCH_HEAD", ".git/gc.log"} {
			if strings.HasPrefix(rel, skip) {
				return nil
			}
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		out[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprinting %s: %v", dir, err)
	}
	return out
}

func diffFingerprints(before, after map[string]string) []string {
	var problems []string
	for path, sum := range before {
		switch got, ok := after[path]; {
		case !ok:
			problems = append(problems, "deleted: "+path)
		case got != sum:
			problems = append(problems, "modified: "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			problems = append(problems, "created: "+path)
		}
	}
	sort.Strings(problems)
	return problems
}

// The invariant: a full sweep leaves the repository byte-identical.
//
// The manifest here is deliberately aggressive -- it reads git state, lists
// files, stats things -- because a read-only test over probes that touch
// nothing proves nothing.
func TestSweepLeavesRepoByteIdentical(t *testing.T) {
	repo := fixtureRepo(t)

	checks := []Check{
		{ID: "git.head", Title: "HEAD", Probe: Command{Set: true, Script: "git rev-parse --abbrev-ref HEAD"}},
		{ID: "git.dirty", Title: "dirty", Probe: Command{Set: true, Script: "git status --porcelain"}},
		{ID: "git.config", Title: "config", Probe: Command{Set: true, Script: "git config --get user.email"}},
		{ID: "git.log", Title: "log", Probe: Command{Set: true, Script: "git log --oneline -1"}},
		{ID: "fs.list", Title: "list", Probe: Command{Set: true, Script: "ls -la"}},
		{ID: "fs.stat", Title: "stat", Probe: Command{Set: true, Script: "stat package.json 2>/dev/null || true"}},
		{ID: "fs.pkg", Title: "pkg", Probe: Command{Set: true, Script: "cat package.json"}},
	}

	before := fingerprint(t, repo)

	r := &Runner{
		Manifest: &Manifest{Checks: checks},
		WorkDir:  repo,
		Version:  "test",
		// RawDir deliberately empty: Ferret's own debug output belongs
		// outside the repo it is measuring.
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if problems := diffFingerprints(before, fingerprint(t, repo)); len(problems) > 0 {
		t.Errorf("phase 1 must be read-only, but the sweep changed the repo:\n  %s",
			strings.Join(problems, "\n  "))
	}
}

// And the test's own teeth: a probe that DOES mutate must be caught by the
// fingerprint. Without this, TestSweepLeavesRepoByteIdentical passing could
// mean the invariant holds or that fingerprint() cannot see anything.
func TestFingerprintDetectsMutation(t *testing.T) {
	repo := fixtureRepo(t)
	before := fingerprint(t, repo)

	// Written directly rather than through the runner: the denylist would
	// refuse this, and what is under test here is the detector, not the
	// denylist.
	if err := os.WriteFile(filepath.Join(repo, "new-file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	problems := diffFingerprints(before, fingerprint(t, repo))
	if len(problems) == 0 {
		t.Fatal("fingerprint did not notice a created file; the read-only test has no teeth")
	}
}

// The denylist is the first line of defence, and it should hold on the obvious
// cases even though it is not a sandbox.
func TestMutatingProbeIsRefusedNotRun(t *testing.T) {
	repo := fixtureRepo(t)
	before := fingerprint(t, repo)

	r := &Runner{
		Manifest: &Manifest{Checks: []Check{{
			ID:    "bad.write",
			Title: "writes a file",
			Probe: Command{Set: true, Script: "touch mutated.txt"},
		}}},
		WorkDir: repo,
		Version: "test",
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	rec := ev.Records[0]
	if rec.Error == nil {
		t.Fatal("a mutating probe should have been refused")
	}
	if rec.Probe != nil && rec.Probe.Exit == 0 {
		t.Error("a refused probe must not record a zero exit; that reads as ran-and-succeeded")
	}
	if problems := diffFingerprints(before, fingerprint(t, repo)); len(problems) > 0 {
		t.Errorf("the refused probe still changed the repo: %v", problems)
	}
}
