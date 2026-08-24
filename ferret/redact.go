// Redaction, applied at capture time in the runner.
//
// This is the Go counterpart to scripts/lib/redact.sh, and it must agree with
// it on what "redacted" means. The shell version is the precedent: it exists
// because stage zero produces machine-identifying data before this binary can
// run, and it is already covered by tests/redaction-test.sh.
//
// ALLOWLIST, AT CAPTURE TIME. Blocklisting at render time fails the moment
// someone invents a new secret-shaped variable, and it fails silently. A value
// that never enters evidence.json cannot leak out of it.
//
// Contract: docs/design/ARCHITECTURE.md §7, docs/design/MANIFEST_SCHEMA.md §3.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"sort"
	"strings"
)

// identitySubstitutions mirrors redact.sh's level 1: who you are. Machine
// details stay, so a reader can still tell you your Docker is stale.
//
// Longest-first, because the repo usually sits under HOME and TMPDIR can too.
// Substituting the shorter one first leaves a half-redacted path like
// "~/new_c/project-ferret" where "<repo>" was meant. redact.sh has the same
// ordering and the same reason.
func identitySubstitutions() [][2]string {
	var subs [][2]string
	add := func(from, to string) {
		from = strings.TrimRight(from, "/")
		if from != "" && from != "/" {
			subs = append(subs, [2]string{from, to})
		}
	}

	add(os.Getenv("FERRET_REPO"), "<repo>")
	add(os.Getenv("TMPDIR"), "<tmpdir>")
	add(os.Getenv("HOME"), "~")

	sort.SliceStable(subs, func(i, j int) bool {
		return len(subs[i][0]) > len(subs[j][0])
	})

	// The username goes last: it is a bare word rather than a path, so it can
	// appear inside strings the path substitutions already handled.
	if u, err := user.Current(); err == nil && u.Username != "" {
		subs = append(subs, [2]string{u.Username, "<user>"})
	}
	return subs
}

// applyRedaction filters one captured value.
//
// checkLevel is the check's own `redact` field; globalLevel is the run-wide
// setting. The stricter of the two wins -- a run-wide identity setting must
// not weaken a check that declared itself secret, and vice versa.
func applyRedaction(s string, checkLevel, globalLevel Redact) string {
	level := stricter(checkLevel, globalLevel)
	switch level {
	case RedactSecret:
		return shapeOf(s)
	case RedactIdentity:
		return redactIdentity(s)
	default:
		return s
	}
}

func rank(r Redact) int {
	switch r {
	case RedactSecret:
		return 2
	case RedactIdentity:
		return 1
	default:
		return 0
	}
}

func stricter(a, b Redact) Redact {
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

func redactIdentity(s string) string {
	if s == "" {
		return s
	}
	for _, sub := range identitySubstitutions() {
		s = strings.ReplaceAll(s, sub[0], sub[1])
	}
	return s
}

// credentialPrefixes are the recognisable token shapes worth naming in the
// output. An unrecognised secret still gets its length and a hash -- the
// prefix is a convenience, not the mechanism.
var credentialPrefixes = []string{
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"glpat-", "xoxb-", "xoxp-", "xoxa-", "sk-", "sk_live_", "sk_test_",
	"pk_live_", "AKIA", "ASIA", "npm_", "dop_v1_", "AIza",
}

// shapeOf records presence and shape, never the value.
//
// The format leads with "<set," so that a non_empty expectation still
// resolves correctly against a redacted capture -- presence survives
// redaction by construction. MANIFEST_SCHEMA.md §9 depends on this.
func shapeOf(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}

	var prefix string
	for _, p := range credentialPrefixes {
		if strings.HasPrefix(trimmed, p) {
			prefix = p
			break
		}
	}

	// A stable salted hash, so "same token as before" is answerable without
	// exposing it. Salted per-machine: the hash is comparable across runs on
	// one box, and not across boxes, which is the comparison anyone actually
	// wants and the one that leaks least.
	sum := hmac.New(sha256.New, redactionSalt())
	sum.Write([]byte(trimmed))
	digest := hex.EncodeToString(sum.Sum(nil))[:12]

	if prefix != "" {
		return fmt.Sprintf("<set, %d chars, %s…, hash %s>", len(trimmed), prefix, digest)
	}
	return fmt.Sprintf("<set, %d chars, hash %s>", len(trimmed), digest)
}

// shapeLines handles captures with several lines -- an `env` dump, an
// .npmrc. Each line is shaped independently so structure survives while values
// do not.
//
// The key pattern is deliberately permissive about leading punctuation,
// because a real .npmrc key is not an identifier:
//
//	//registry.npmjs.org/:_authToken=npm_...
//
// Requiring a leading letter left that line untouched and the token exposed --
// found by TestShapeLinesKeepsStructure, which is exactly the shape of the
// thing this function exists to catch.
var assignmentRe = regexp.MustCompile(`^([A-Za-z0-9_./:@-]*[A-Za-z0-9_])\s*([=:])\s*(.*)$`)

func shapeLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		m := assignmentRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lines[i] = m[1] + m[2] + shapeOf(m[3])
	}
	return strings.Join(lines, "\n")
}

// redactionSalt is per-machine and per-user, derived rather than stored.
//
// Deriving it means there is no salt file to leak and no state to manage. It
// is not a security boundary -- an attacker who knows the username can
// recompute it -- it exists to stop a hash being trivially reversible from a
// rainbow table of common tokens.
func redactionSalt() []byte {
	h := sha256.New()
	if u, err := user.Current(); err == nil {
		h.Write([]byte(u.Username))
		h.Write([]byte(u.Uid))
	}
	host, _ := os.Hostname()
	h.Write([]byte(host))
	h.Write([]byte("ferret-redaction-v1"))
	return h.Sum(nil)
}
