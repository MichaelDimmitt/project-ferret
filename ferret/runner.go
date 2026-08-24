// The runner: executes probes and records what happened. Nothing else.
//
// THIS FILE MUST NOT INTERPRET. It records raw stdout, stderr, exit code, and
// duration. It does not read `expect`, `severity`, `tainted_by`, or `remedy`;
// it does not decide whether a check passed; it does not order records by
// result, because ordering by result requires knowing results.
//
// If this file ever imports a verdict package, the design is broken. The
// separation is structural -- capture and interpretation are different
// processes -- so that the runner physically cannot skip a check because an
// earlier one failed. It does not know what failure means.
//
// Contract: docs/design/MANIFEST_SCHEMA.md §7 and §10.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Evidence structure, per MANIFEST_SCHEMA.md §7.
//
// schema_version increments on any breaking change to this shape. A verdict
// engine reading a version it does not know exits 3.
const evidenceSchemaVersion = 1

// maxCapture bounds what enters the record. The untruncated text goes to
// .ferret/raw/, for debugging Ferret itself.
const maxCapture = 64 * 1024

type Evidence struct {
	SchemaVersion int      `json:"schema_version"`
	FerretVersion string   `json:"ferret_version"`
	StartedAt     string   `json:"started_at"`
	FinishedAt    string   `json:"finished_at"`
	RedactLevel   string   `json:"redact_level"`
	ManifestFiles []string `json:"manifest_files"`
	Records       []Record `json:"records"`
}

type Record struct {
	ID         string     `json:"id"`
	Layer      int        `json:"layer"`
	Applies    bool       `json:"applies"`
	AppliesIf  *RunResult `json:"applies_if"`
	Probe      *RunResult `json:"probe"`
	Declared   *RunResult `json:"declared"`
	Error      *RunError  `json:"error"`
	Volatility string     `json:"volatility"`
	Provenance string     `json:"provenance"`
	CapturedAt string     `json:"captured_at"`
}

type RunResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Exit       int    `json:"exit"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Truncated  bool   `json:"truncated"`
}

// RunError is the subset of UNKNOWN reasons the runner can determine on its
// own. It never emits `tainted` (M3's judgment), `unverifiable` (a property of
// the check, not the run), or `expired` (M10's).
type RunError struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

const (
	errTimeout    = "timeout"
	errToolAbsent = "tool_absent"
	errPermission = "permission"
	errProbeError = "probe_error"

	// errUnverifiable is emitted for one situation only: a probe was refused
	// because it would have read a secret value. That is unverifiable BY
	// DESIGN rather than by circumstance -- the answer is not unavailable, it
	// is one Ferret declines to obtain. See AGENTS.md.
	errUnverifiable = "unverifiable"
)

// denylistTokens is the mutation denylist, per MANIFEST_SCHEMA.md §6.
//
// It over-matches on purpose. The override makes a false alarm cheap and a
// miss expensive, so the list is tuned to catch the dangerous cases and accept
// false positives.
//
// This is NOT a sandbox. A probe determined to mutate can trivially evade a
// regex. The real guarantees are manifest review at authoring time and the
// byte-identical fixture test (M5). This catches accidents.
var denylistTokens = []string{
	"install", "fetch", "pull", "clone", "push", "commit",
	"checkout", "prune", "gc", "rm", "mv", "write", "set", "add",
	"init", "reset", "stash", "apply", "tee", "sudo",
	"chmod", "chown", "mkdir", "touch", "ln",
}

// Word-boundary matching, so `git config --get` is not caught by "set" inside
// some unrelated word, while `git config --set` is.
var denylistRe = func() *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^a-z0-9_-])(` + strings.Join(denylistTokens, "|") + `)($|[^a-z0-9_-])`)
}()

// redirectRe catches > and >> without matching 2>/dev/null, >&2, or the
// comparison operators in test expressions. Discarding output is not mutation.
var redirectRe = regexp.MustCompile(`>>?\s*(?:&\s*\d|/dev/(?:null|stderr|stdout)\b)?`)

// secretReadRe catches the common ways a probe would pull a secret VALUE into
// stdout. AGENTS.md § "Never read a secret" is the rule; this is its
// mechanical backstop, and like the mutation denylist it is not a sandbox.
//
// It matches reading, not mentioning. `printenv GH_TOKEN` is refused;
// `[ -n "${GH_TOKEN:-}" ]` is not, because the value never reaches stdout.
var secretReadRe = regexp.MustCompile(
	// printenv/echo/cat of a credential-named variable
	`(?i)\b(printenv|echo|print)\s+"?\$?\{?[A-Z_]*(TOKEN|SECRET|PASSWORD|PASSWD|APIKEY|API_KEY|CREDENTIAL|PRIVATE_KEY)[A-Z_]*\}?` +
		// or opening a file that is known to carry credentials
		`|\b(cat|head|tail|less|more|awk|cut)\b[^|;&]*\.(npmrc|netrc|pgpass|env)\b` +
		`|\b(cat|head|tail|less|more)\b[^|;&]*(id_rsa|id_ed25519|\.pem|\.key|credentials)\b`)

// secretAssignRe matches a credential key followed by '=', which is the shape
// of a line whose VALUE would be emitted. `_authToken=` in a grep pattern
// means the match includes the token; `_authToken` alone does not.
//
// Go's regexp is RE2, which has no negative lookahead by design, so "a grep
// that is not counting" cannot be one pattern. readsSecret composes it from
// two checks instead, which reads better than a lookahead would have.
var secretAssignRe = regexp.MustCompile(
	`(?i)(_authToken|_password|passwd|apikey|api_key|secret|token)\s*=`)

// countingGrepRe recognises the safe form: grep -c, which yields a number.
var countingGrepRe = regexp.MustCompile(`\bgrep\b[^|;&]*\s-[a-zA-Z]*c`)

// fieldExtractRe matches tools that print a selected field, which over a
// credential assignment means printing the value.
var fieldExtractRe = regexp.MustCompile(`\b(awk|cut|sed)\b`)

// readsSecret returns a description of the violation, or "" if the command is
// allowed.
func readsSecret(script string) string {
	if m := secretReadRe.FindString(script); m != "" {
		return strings.TrimSpace(m)
	}
	// A grep whose pattern includes a credential KEY followed by '=' would
	// print the value. The counting form is exempt: it yields a number.
	if strings.Contains(script, "grep") && !countingGrepRe.MatchString(script) {
		if m := secretAssignRe.FindString(script); m != "" {
			return strings.TrimSpace(m)
		}
	}

	// Field extraction from a credential file prints the value by definition.
	if secretAssignRe.MatchString(script) && fieldExtractRe.MatchString(script) {
		return "field extraction over a credential assignment"
	}
	return ""
}

// deniedBy returns the matched token, or "" if the command is allowed.
func deniedBy(script string) string {
	if m := denylistRe.FindStringSubmatch(script); m != nil {
		return m[2]
	}
	for _, m := range redirectRe.FindAllString(script, -1) {
		t := strings.TrimSpace(m)
		// Redirects to /dev/null, another fd, or the standard streams discard
		// rather than write. Anything else is a file write.
		if strings.Contains(t, "/dev/") || strings.Contains(t, "&") {
			continue
		}
		return strings.TrimSpace(strings.TrimRight(t, " "))
	}
	return ""
}

// Runner executes a manifest. It holds no verdict state because it has none.
type Runner struct {
	Manifest    *Manifest
	WorkDir     string
	RawDir      string // .ferret/raw, or "" to skip untruncated capture
	RedactLevel Redact
	Version     string

	// Now is injectable so tests get deterministic timestamps.
	Now func() time.Time
}

// Run executes every check in manifest order and returns the evidence.
//
// It returns an error only when it could not do its job at all -- never
// because a probe failed. A failing probe is data.
func (r *Runner) Run(ctx context.Context) (*Evidence, error) {
	now := r.Now
	if now == nil {
		now = time.Now
	}

	ev := &Evidence{
		SchemaVersion: evidenceSchemaVersion,
		FerretVersion: r.Version,
		StartedAt:     now().UTC().Format(time.RFC3339),
		RedactLevel:   string(r.RedactLevel),
		ManifestFiles: r.Manifest.Files,
		Records:       make([]Record, 0, len(r.Manifest.Checks)),
	}

	for i := range r.Manifest.Checks {
		c := &r.Manifest.Checks[i]
		ev.Records = append(ev.Records, r.runCheck(ctx, c, now))
	}

	ev.FinishedAt = now().UTC().Format(time.RFC3339)
	return ev, nil
}

func (r *Runner) runCheck(ctx context.Context, c *Check, now func() time.Time) Record {
	rec := Record{
		ID:         c.ID,
		Layer:      c.LayerOf(0),
		Applies:    true,
		Volatility: c.VolatilityClass(),
		Provenance: "observed", // everything the runner captures; M11 adds others
		CapturedAt: now().UTC().Format(time.RFC3339),
	}

	// §10 step 4a-4b: the gate.
	//
	// A clean non-zero exit means N/A. A dirty failure -- timeout, denylist
	// refusal, probe error -- is UNKNOWN(probe_error), NEVER N/A. Silently
	// converting a broken gate into "doesn't apply here" is a false green
	// wearing a different hat.
	if c.AppliesIf.Set {
		res, rerr := r.exec(ctx, c, c.AppliesIf.Script, c.Timeout(), "applies_if")
		rec.AppliesIf = res
		if rerr != nil {
			rec.Applies = false
			rec.Error = rerr
			return rec
		}
		if res.Exit != 0 {
			rec.Applies = false
			return rec // N/A: probe and declared stay nil, the commands did not run
		}
	}

	// §10 step 4c. Each command gets the full timeout budget separately; it is
	// not a total for the check.
	probeRes, probeErr := r.exec(ctx, c, c.Probe.Script, c.Timeout(), "probe")
	rec.Probe = probeRes
	if probeErr != nil {
		rec.Error = probeErr
	}

	if c.Declared.Set {
		declRes, declErr := r.exec(ctx, c, c.Declared.Script, c.Timeout(), "declared")
		rec.Declared = declRes
		// A probe error is the more informative one; do not overwrite it.
		if declErr != nil && rec.Error == nil {
			rec.Error = declErr
		}
	}

	return rec
}

// exec runs one command under a timeout and captures the result.
//
// The returned RunError is non-nil only for conditions the runner can identify
// itself. It does not judge the command's exit code -- a non-zero exit is not
// an error here, it is data, and `expect` decides what it means.
func (r *Runner) exec(ctx context.Context, c *Check, script string, timeout float64, phase string) (*RunResult, *RunError) {
	// Never read a secret. AGENTS.md makes this binding on probes, scripts,
	// and agents; this is the mechanical backstop.
	//
	// NOTE THE ABSENCE OF AN OVERRIDE. The mutation denylist has one, because
	// `git config --get` is genuinely read-only despite matching. This has
	// none: there is no probe that legitimately needs a secret's value, so an
	// escape hatch would only ever be used to do the forbidden thing. A check
	// that cannot be performed without reading is UNKNOWN(unverifiable), which
	// is a supported result, not a problem to route around.
	if m := readsSecret(script); m != "" {
		return &RunResult{Exit: -1}, &RunError{
			Kind: errUnverifiable,
			Detail: fmt.Sprintf("%s refused: %q would read a secret value. "+
				"Test for presence instead (see AGENTS.md: Never read a secret)", phase, m),
		}
	}

	// §6: the denylist runs immediately before execution, not at validation
	// time, so a denied check still appears in the evidence with its reason
	// rather than the sweep refusing to start.
	if c.Mutating == nil {
		if tok := deniedBy(script); tok != "" {
			// Exit -1, not 0. A refused probe did not run, and a zero here
			// would read as "ran, succeeded" to anything scanning exit codes.
			return &RunResult{Exit: -1}, &RunError{
				Kind: errProbeError,
				Detail: fmt.Sprintf("%s refused: matched mutation denylist on %q; "+
					"add \"mutating\": false with a mutating_why if this is read-only", phase, tok),
			}
		}
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout*float64(time.Second)))
	defer cancel()

	cmd := exec.CommandContext(cctx, "sh", "-c", script)
	cmd.Dir = r.WorkDir

	// Fresh-shell isolation, per MANIFEST_SCHEMA.md `isolate` and
	// ARCHITECTURE.md §5. Opt-in, because most checks legitimately want the
	// real environment -- "is the proxy set HERE" is a question about this
	// shell.
	//
	// The failure this prevents: a tool that is only on PATH because of the
	// user's .zshrc gets reported GO, and CI -- which never sources it --
	// cannot find the tool at all. That is a green that does not survive
	// contact with the build machine.
	//
	// THE GATE IS NEVER ISOLATED. applies_if decides RELEVANCE -- is this
	// machine in scope for the question -- while probe measures the ANSWER.
	// Isolating the gate makes a fresh-shell check self-cancelling: "does node
	// survive a clean environment" gates on finding node in a clean
	// environment, fails, and resolves N/A without ever asking. The check
	// could then never fire, which is a silent gap rather than a finding.
	// Observed exactly that way before this line existed.
	if phase != "applies_if" && c.Isolate != nil && *c.Isolate {
		cmd.Env = isolatedEnv()
	}

	// Kill the whole process group on timeout. A probe that spawns a child --
	// a curl inside a pipeline -- would otherwise outlive the timeout and hold
	// the pipe open, which is the hang this timeout exists to prevent.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	res := &RunResult{
		DurationMs: elapsed.Milliseconds(),
	}

	rawOut, rawErr := stdout.String(), stderr.String()
	res.Stdout, res.Truncated = truncate(rawOut)
	s, t := truncate(rawErr)
	res.Stderr = s
	res.Truncated = res.Truncated || t

	// §10 step 4d: redaction applies BEFORE the values enter the record. A
	// value that never enters evidence.json cannot leak out of it.
	res.Stdout = applyRedaction(res.Stdout, c.RedactLevel, r.RedactLevel)
	res.Stderr = applyRedaction(res.Stderr, c.RedactLevel, r.RedactLevel)

	r.writeRaw(c.ID, phase, rawOut, rawErr, c.RedactLevel)

	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Exit = -1
		return res, &RunError{
			Kind:   errTimeout,
			Detail: fmt.Sprintf("%s exceeded timeout_s=%g", phase, timeout),
		}
	}

	var ee *exec.ExitError
	switch {
	case err == nil:
		res.Exit = 0
	case errors.As(err, &ee):
		res.Exit = ee.ExitCode()
	default:
		res.Exit = -1
		return res, &RunError{
			Kind:   errProbeError,
			Detail: fmt.Sprintf("%s could not be executed: %v", phase, err),
		}
	}

	// tool_absent and permission are heuristics, and MANIFEST_SCHEMA.md §11
	// says so. Exit 127 is sh's "command not found"; 126 is "found but not
	// executable". Neither is reliable -- a probe that legitimately exits 127
	// will be misread -- which is why this is documented as a guess rather
	// than presented as a fact.
	//
	// These do NOT stop the check. The output is recorded either way; the
	// error only annotates why it looks the way it does.
	switch res.Exit {
	case 127:
		return res, &RunError{
			Kind:   errToolAbsent,
			Detail: strings.TrimSpace(firstLine(rawErr)),
		}
	case 126:
		return res, &RunError{
			Kind:   errPermission,
			Detail: strings.TrimSpace(firstLine(rawErr)),
		}
	}

	return res, nil
}

// isolationPath is the PATH an isolated probe gets: the conventional system
// directories, and nothing a version manager or dotfile has prepended.
//
// Set rather than cleared. A probe with no PATH at all cannot exec anything
// and would report tool_absent for every tool regardless of what is installed
// -- the same false answer as the one isolation exists to prevent, pointing
// the other way.
const isolationPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// isolatedEnv builds the environment for an isolated probe explicitly, rather
// than filtering the inherited one. A denylist of variables to strip would
// miss the next NODE_OPTIONS someone invents; an allowlist of two cannot.
//
// HOME survives because too much breaks without it -- git finds no config,
// version managers find no installs -- and it is not the variable that causes
// the false negative this exists to prevent.
func isolatedEnv() []string {
	env := []string{"PATH=" + isolationPath}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

func truncate(s string) (string, bool) {
	if len(s) <= maxCapture {
		return s, false
	}
	return s[:maxCapture], true
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// writeRaw stores untruncated output for debugging Ferret itself.
//
// Never for checks marked secret: the whole point of that level is that the
// value is not retained anywhere, and a debug directory is still somewhere.
func (r *Runner) writeRaw(id, phase, stdout, stderr string, level Redact) {
	if r.RawDir == "" || level == RedactSecret {
		return
	}
	for name, content := range map[string]string{
		id + "." + phase + ".out": stdout,
		id + "." + phase + ".err": stderr,
	} {
		if content == "" {
			continue
		}
		// Best effort. Failing to write a debug file is not a reason to lose
		// the sweep.
		_ = os.WriteFile(filepath.Join(r.RawDir, name), []byte(content), 0o600)
	}
}

// ReadEvidence loads a stored evidence file.
//
// This is the verdict engine's only input. Nothing here re-probes or consults
// the machine -- a stale evidence.json produces a stale verdict, honestly
// labelled by its captured_at, rather than a silent re-measurement.
func ReadEvidence(path string) (*Evidence, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading evidence: %w", err)
	}
	var ev Evidence
	if err := json.Unmarshal(b, &ev); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &ev, nil
}

// WriteEvidence serialises to disk.
func WriteEvidence(path string, ev *Evidence) error {
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding evidence: %w", err)
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("writing evidence: %w", err)
	}
	return nil
}
