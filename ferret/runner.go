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
