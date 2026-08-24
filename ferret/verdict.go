// The verdict engine: applies expectations to captured evidence.
//
// Its ONLY input is evidence.json. It never probes, never shells out, never
// touches the machine. That is what makes a verdict re-runnable: change an
// `expect` in the manifest, re-run this against yesterday's evidence, and the
// verdict changes without re-measuring anything.
//
// The counterpart rule lives in runner.go, which must not interpret. Neither
// file imports the other's concerns; they meet only at evidence.json.
//
// THE INVARIANT: no code path converts UNKNOWN into GO. Every function here
// that could resolve a state either produces UNKNOWN with a reason or a
// definite verdict from evidence that exists. Asserted in verdict_test.go.
//
// Contract: docs/design/ARCHITECTURE.md §5-6, docs/design/MANIFEST_SCHEMA.md §4.
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// State is the four-state result. ARCHITECTURE.md §5.
type State string

const (
	StateGo      State = "GO"      // probe ran, expect satisfied
	StateNoGo    State = "NO-GO"   // probe ran, expect violated
	StateUnknown State = "UNKNOWN" // could not produce a trustworthy answer
	StateNA      State = "N/A"     // applies_if false
)

// UNKNOWN reasons. ARCHITECTURE.md §5 maps these to the framework's T3a-d.
const (
	ReasonTimeout      = "timeout"
	ReasonToolAbsent   = "tool_absent"
	ReasonPermission   = "permission"
	ReasonUnverifiable = "unverifiable"
	ReasonExpired      = "expired"
	ReasonTainted      = "tainted"
	ReasonProbeError   = "probe_error"
)

// Verdict is one check, resolved.
type Verdict struct {
	ID       string
	Title    string
	Layer    int
	Severity string
	Decisive bool
	State    State
	Reason   string // mandatory when State is UNKNOWN
	Detail   string // human-readable: what was found vs what was wanted
	Remedy   string

	// TaintedCount is the rollup for rendering: how many checks this one
	// invalidated. Set only on the cause, so the glance can say
	// "-> taints 38 checks below" instead of printing 38 lines.
	TaintedCount int

	// TaintedBy names the immediate prerequisite that caused a tainted
	// UNKNOWN. RootCause names the failure at the bottom of the chain, which
	// is the one worth showing a reader: an intermediate is itself a victim,
	// and pointing at it sends someone to fix the wrong thing.
	TaintedBy string
	RootCause string
}

// Report is the whole resolved sweep.
type Report struct {
	Verdicts []Verdict
	Counts   map[State]int
}

// Evaluate resolves every check against the evidence.
//
// Both inputs are required: the manifest supplies expectations, the evidence
// supplies observations. A check in one but not the other is an error, not a
// silent skip -- a manifest that grew a check since the evidence was captured
// must not report that check as anything, least of all GO.
func Evaluate(m *Manifest, ev *Evidence) (*Report, error) {
	if ev.SchemaVersion != evidenceSchemaVersion {
		return nil, fmt.Errorf(
			"evidence schema_version %d, this build understands %d; re-run the sweep",
			ev.SchemaVersion, evidenceSchemaVersion)
	}

	byID := make(map[string]*Record, len(ev.Records))
	for i := range ev.Records {
		byID[ev.Records[i].ID] = &ev.Records[i]
	}

	rep := &Report{Counts: map[State]int{}}
	resolved := make(map[string]*Verdict, len(m.Checks))

	// Pass 1: resolve each check against its own evidence, ignoring taint.
	for i := range m.Checks {
		c := &m.Checks[i]
		rec := byID[c.ID]

		var v Verdict
		if rec == nil {
			// The manifest and the evidence disagree about what exists.
			// UNKNOWN(expired) is the honest reading: whatever produced this
			// evidence did not know about this check.
			v = Verdict{
				State:  StateUnknown,
				Reason: ReasonExpired,
				Detail: "no evidence for this check; re-run the sweep",
			}
		} else {
			v = resolveOne(c, rec)
		}

		v.ID = c.ID
		v.Title = c.Title
		v.Layer = c.LayerOf(0)
		v.Severity = c.Severity
		v.Decisive = c.IsDecisive()
		if v.State == StateNoGo {
			v.Remedy = c.Remedy
		}

		rep.Verdicts = append(rep.Verdicts, v)
	}

	// Index AFTER the slice is complete. Taking pointers into a slice while
	// still appending to it means a reallocation leaves them addressing an
	// orphaned backing array, and every taint write lands there instead of on
	// the report. That bug made taint silently do nothing -- the exact "mostly
	// green, conclusion mostly fine" failure this tool exists to prevent.
	for i := range rep.Verdicts {
		resolved[rep.Verdicts[i].ID] = &rep.Verdicts[i]
	}

	// Pass 2: taint. Done after every check has its own state, because a
	// prerequisite must be fully resolved before its dependents can be judged.
	applyTaint(m, resolved)

	for i := range rep.Verdicts {
		rep.Counts[rep.Verdicts[i].State]++
	}
	return rep, nil
}

// resolveOne applies a single check's expectation to a single record.
func resolveOne(c *Check, rec *Record) Verdict {
	// N/A first: applies_if said the question is not asked here.
	if !rec.Applies && rec.Error == nil {
		return Verdict{State: StateNA, Detail: "does not apply here"}
	}

	// A runner-recorded error is UNKNOWN, full stop. The runner identified
	// something that makes the capture untrustworthy -- a timeout, a missing
	// tool, a denylist refusal -- and no expectation can be honestly applied
	// to output that was never produced.
	if rec.Error != nil {
		return Verdict{
			State:  StateUnknown,
			Reason: rec.Error.Kind,
			Detail: rec.Error.Detail,
		}
	}

	if rec.Probe == nil {
		return Verdict{
			State:  StateUnknown,
			Reason: ReasonProbeError,
			Detail: "no probe output was captured",
		}
	}

	ok, detail, err := evaluateExpect(c.Expect, rec)
	if err != nil {
		// An unevaluable predicate is UNKNOWN, never NO-GO. "The check is
		// broken" and "the machine is broken" are different findings, and
		// conflating them is the false positive that gets a tool ignored.
		return Verdict{
			State:  StateUnknown,
			Reason: ReasonProbeError,
			Detail: err.Error(),
		}
	}
	if ok {
		return Verdict{State: StateGo, Detail: detail}
	}
	return Verdict{State: StateNoGo, Detail: detail}
}

// applyTaint walks the dependency DAG and downgrades dependents of NO-GO
// checks to UNKNOWN(tainted).
//
// The manifest loader has already proven the graph is acyclic (§8 rule 4), so
// this does not need cycle detection -- but it must handle transitive taint:
// if A is NO-GO and B is tainted by A, then C tainted by B is also tainted.
func applyTaint(m *Manifest, resolved map[string]*Verdict) {
	// Iterate to a fixed point. The graph is small (tens of checks) and
	// acyclic, so this terminates in at most depth passes.
	for changed := true; changed; {
		changed = false
		for i := range m.Checks {
			c := &m.Checks[i]
			v := resolved[c.ID]
			if v == nil {
				continue
			}

			// Already tainted, or already unknown for its own reason: nothing
			// to downgrade. Note that a NO-GO check CAN be tainted -- its own
			// failure may be a consequence of the prerequisite's.
			if v.State == StateUnknown || v.State == StateNA {
				continue
			}

			for _, depID := range c.TaintedBy {
				dep := resolved[depID]
				if dep == nil {
					continue
				}
				// A prerequisite that is NO-GO, or itself tainted, makes this
				// check's own result untrustworthy regardless of what its
				// probe returned.
				if dep.State == StateNoGo || (dep.State == StateUnknown && dep.Reason == ReasonTainted) {
					v.State = StateUnknown
					v.Reason = ReasonTainted
					v.TaintedBy = depID
					// Name the ROOT cause, not the intermediate. "prerequisite
					// fx.node.version is UNKNOWN" tells the reader to chase a
					// check that is itself a victim; the actionable fact is
					// the failure at the bottom of the chain.
					v.Detail = fmt.Sprintf("prerequisite %s failed; this result cannot be trusted",
						rootCauseOf(depID, resolved))
					v.Remedy = "" // the remedy belongs to the prerequisite
					changed = true
					break
				}
			}
		}
	}

	// Rollup counts on the cause, for the glance.
	for i := range m.Checks {
		v := resolved[m.Checks[i].ID]
		if v == nil || v.TaintedBy == "" {
			continue
		}
		// The count lands on the original failure rather than on an
		// intermediate that was itself tainted, so the glance can say
		// "-> taints 38 checks below" against the thing to actually fix.
		root := rootCauseOf(v.TaintedBy, resolved)
		v.RootCause = root
		if cause := resolved[root]; cause != nil {
			cause.TaintedCount++
		}
	}
}

// rootCauseOf follows a taint chain to the check that actually failed.
//
// Bounded by the number of checks: the manifest loader has proven the graph
// acyclic (§8 rule 4), but this must not loop forever even if that guarantee
// is ever weakened, because a hang in the verdict engine is worse than a
// wrong verdict -- it produces no report at all.
func rootCauseOf(id string, resolved map[string]*Verdict) string {
	seen := map[string]bool{}
	for !seen[id] {
		seen[id] = true
		v := resolved[id]
		if v == nil || v.TaintedBy == "" {
			return id
		}
		id = v.TaintedBy
	}
	return id
}

// evaluateExpect applies one predicate. It returns (satisfied, detail, error),
// where a non-nil error means the predicate could not be evaluated at all.
func evaluateExpect(e Expect, rec *Record) (bool, string, error) {
	get := func(key string) (string, error) {
		raw, ok := e.Raw[key]
		if !ok {
			return "", fmt.Errorf("expect.%s is missing", key)
		}
		s, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("expect.%s must be a string", key)
		}
		return interpolate(s, rec)
	}

	switch e.Type {
	case "equals":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		want, err := get("value")
		if err != nil {
			return false, "", err
		}
		return actual == want, fmt.Sprintf("%q (want %q)", actual, want), nil

	case "matches":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		pat, ok := e.Raw["pattern"].(string)
		if !ok {
			return false, "", fmt.Errorf("expect.pattern must be a string")
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			// Should be unreachable: §8 rule 10 rejects this at load. Handled
			// anyway, because evidence can outlive the manifest that made it.
			return false, "", fmt.Errorf("expect.pattern is not a valid regexp: %v", err)
		}
		got := re.MatchString(actual)
		if inv, _ := e.Raw["invert"].(bool); inv {
			return !got, fmt.Sprintf("%q (want: not matching %s)", actual, pat), nil
		}
		return got, fmt.Sprintf("%q (want: matching %s)", actual, pat), nil

	case "one_of":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		raw, _ := e.Raw["values"].([]any)
		var vals []string
		for _, r := range raw {
			s, ok := r.(string)
			if !ok {
				return false, "", fmt.Errorf("expect.values must be strings")
			}
			vals = append(vals, s)
		}
		if len(vals) == 0 {
			return false, "", fmt.Errorf("expect.values is empty")
		}
		for _, v := range vals {
			if actual == v {
				return true, fmt.Sprintf("%q", actual), nil
			}
		}
		return false, fmt.Sprintf("%q (want one of: %s)", actual, strings.Join(vals, ", ")), nil

	case "non_empty":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		if actual == "" {
			return false, "empty (want: any value)", nil
		}
		return true, fmt.Sprintf("%q", actual), nil

	case "absent":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		if actual == "" {
			return true, "absent", nil
		}
		return false, fmt.Sprintf("%q (want: absent)", actual), nil

	case "numeric_lt", "numeric_gt":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(actual), 64)
		if err != nil {
			// §4: non-numeric input is UNKNOWN, not NO-GO. A probe returning
			// "command not found" on stdout is Ferret's bug, not a finding.
			return false, "", fmt.Errorf("%q is not numeric", truncateForMsg(actual))
		}
		want, ok := toFloat(e.Raw["value"])
		if !ok {
			return false, "", fmt.Errorf("expect.value must be a number")
		}
		unit, _ := e.Raw["unit"].(string)
		op := "<"
		got := n < want
		if e.Type == "numeric_gt" {
			op, got = ">", n > want
		}
		return got, fmt.Sprintf("%s%s (want %s %s%s)",
			formatNumber(n), unit, op, formatNumber(want), unit), nil

	case "semver_satisfies":
		actual, err := get("actual")
		if err != nil {
			return false, "", err
		}
		rangeSpec, err := get("range")
		if err != nil {
			return false, "", err
		}
		ok, serr := satisfies(actual, rangeSpec)
		if serr != nil {
			return false, "", fmt.Errorf("cannot compare %q against %q: %v",
				truncateForMsg(actual), truncateForMsg(rangeSpec), serr)
		}
		return ok, fmt.Sprintf("%s (want %s)", actual, rangeSpec), nil

	case "exit_code":
		of, _ := e.Raw["of"].(string)
		var res *RunResult
		switch of {
		case "probe":
			res = rec.Probe
		case "declared":
			res = rec.Declared
		default:
			return false, "", fmt.Errorf("expect.of must be probe or declared")
		}
		if res == nil {
			return false, "", fmt.Errorf("no %s output was captured", of)
		}
		if v, ok := toFloat(e.Raw["value"]); ok {
			return res.Exit == int(v), fmt.Sprintf("exit %d (want %d)", res.Exit, int(v)), nil
		}
		raw, _ := e.Raw["values"].([]any)
		var want []int
		for _, r := range raw {
			f, ok := toFloat(r)
			if !ok {
				return false, "", fmt.Errorf("expect.values must be integers")
			}
			want = append(want, int(f))
		}
		if len(want) == 0 {
			return false, "", fmt.Errorf("expect needs value or values")
		}
		for _, w := range want {
			if res.Exit == w {
				return true, fmt.Sprintf("exit %d", res.Exit), nil
			}
		}
		return false, fmt.Sprintf("exit %d (want one of: %v)", res.Exit, want), nil
	}

	return false, "", fmt.Errorf("unknown expect type %q", e.Type)
}

// interpolate expands §5's tokens against a record.
//
// This happens HERE, in the verdict engine, never in the shell. The runner
// passed `probe` to sh exactly as written.
func interpolate(s string, rec *Record) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		// Longest match first, so $probe_exit is not read as $probe + "_exit".
		matched := false
		for _, tok := range tokenOrder {
			if strings.HasPrefix(s[i+1:], tok) {
				val, err := tokenValue(tok, rec)
				if err != nil {
					return "", err
				}
				b.WriteString(val)
				i += 1 + len(tok)
				matched = true
				break
			}
		}
		if !matched {
			// Unreachable for a validated manifest (§8 rule 8), but evidence
			// can outlive the manifest that produced it.
			return "", fmt.Errorf("unknown interpolation token in %q", s)
		}
	}
	return b.String(), nil
}

// tokenOrder is longest-first so prefixes resolve correctly.
var tokenOrder = []string{
	"declared_stderr", "declared_exit", "probe_stderr", "probe_exit",
	"declared", "probe",
}

func tokenValue(tok string, rec *Record) (string, error) {
	switch tok {
	case "probe":
		if rec.Probe == nil {
			return "", fmt.Errorf("$probe referenced but no probe output was captured")
		}
		return strings.TrimSpace(rec.Probe.Stdout), nil
	case "probe_stderr":
		if rec.Probe == nil {
			return "", fmt.Errorf("$probe_stderr referenced but no probe ran")
		}
		return strings.TrimSpace(rec.Probe.Stderr), nil
	case "probe_exit":
		if rec.Probe == nil {
			return "", fmt.Errorf("$probe_exit referenced but no probe ran")
		}
		return strconv.Itoa(rec.Probe.Exit), nil
	case "declared":
		if rec.Declared == nil {
			return "", fmt.Errorf("$declared referenced but no declared command ran")
		}
		return strings.TrimSpace(rec.Declared.Stdout), nil
	case "declared_stderr":
		if rec.Declared == nil {
			return "", fmt.Errorf("$declared_stderr referenced but no declared command ran")
		}
		return strings.TrimSpace(rec.Declared.Stderr), nil
	case "declared_exit":
		if rec.Declared == nil {
			return "", fmt.Errorf("$declared_exit referenced but no declared command ran")
		}
		return strconv.Itoa(rec.Declared.Exit), nil
	}
	return "", fmt.Errorf("unknown token $%s", tok)
}

// toFloat accepts the numeric forms encoding/json produces.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// formatNumber renders a measurement for a human.
//
// %g gives "2.048576e+08" for a disk-space figure, which is unreadable in a
// report meant to be scanned in ten seconds. Whole numbers print as integers;
// fractions keep only the precision they have.
func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// truncateForMsg keeps an error message readable when a probe dumped a page of
// output where a version was expected.
func truncateForMsg(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "…"
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

// ExitCode maps a report to the process exit code. ARCHITECTURE.md §11.
//
//	0  GO      no blockers, no unknowns among decisive checks
//	1  NO-GO   one or more blockers
//	2  UNKNOWN no blockers, but decisive checks unresolved
//	3  Ferret itself failed (handled by the caller, not here)
//
// 2 is deliberately not 0. In CI, "we couldn't verify" must not pass silently
// -- that is the false green moved into automation where nobody will look.
func (r *Report) ExitCode() int {
	blocked, unresolved := false, false
	for i := range r.Verdicts {
		v := &r.Verdicts[i]
		if v.State == StateNoGo && v.Severity == "blocker" {
			blocked = true
		}
		if v.State == StateUnknown && v.Decisive {
			unresolved = true
		}
	}
	switch {
	case blocked:
		return exitNoGo
	case unresolved:
		return exitUnknown
	default:
		return exitGo
	}
}
