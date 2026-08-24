// Manifest loading and validation.
//
// Everything here runs BEFORE any probe executes. A manifest that does not
// validate exits 3: a malformed manifest is Ferret being broken, not a finding
// about the machine, and running half of a broken manifest produces a report
// that looks complete and is not.
//
// This file deliberately knows nothing about what any field MEANS to the
// verdict engine. It parses `expect` enough to validate its shape and its
// interpolation tokens, and no further. Evaluating a predicate is
// interpretation, which happens in a different process.
//
// Contract: docs/design/MANIFEST_SCHEMA.md.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Defaults, per MANIFEST_SCHEMA.md §2. Stated as constants rather than
// scattered literals so the doc and the code can be diffed by eye.
const (
	defaultTimeoutS   = 10.0
	defaultVolatility = "session"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9_]+)*$`)

// Command is a probe, declared, or applies_if: a string, or an array of
// strings joined with newlines so a long script stays readable in JSON.
//
// Custom unmarshalling rather than json.RawMessage at the use site, so the
// rest of the code never has to care which form the author chose.
type Command struct {
	Script string // the joined form, ready for sh -c
	Set    bool   // distinguishes absent from empty
}

func (c *Command) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		c.Script, c.Set = s, true
		return nil
	}
	var parts []string
	if err := json.Unmarshal(b, &parts); err != nil {
		return fmt.Errorf("must be a string or an array of strings")
	}
	if len(parts) == 0 {
		return fmt.Errorf("must not be an empty array")
	}
	c.Script, c.Set = strings.Join(parts, "\n"), true
	return nil
}

// Redact is boolean-or-string in the manifest; one type here.
//
// The levels must agree with scripts/lib/redact.sh, which is the tested shell
// precedent. false/identity map to its levels 0 and 1. "secret" has no shell
// equivalent because stage zero never captures secrets.
type Redact string

const (
	RedactNone     Redact = ""         // standard capture; global redaction still applies
	RedactIdentity Redact = "identity" // strip user, home, tmpdir, repo path
	RedactSecret   Redact = "secret"   // presence and shape only, never the value
)

func (r *Redact) UnmarshalJSON(b []byte) error {
	var flag bool
	if err := json.Unmarshal(b, &flag); err == nil {
		if flag {
			*r = RedactIdentity
		} else {
			*r = RedactNone
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("must be a boolean or one of: identity, secret")
	}
	switch Redact(s) {
	case RedactIdentity, RedactSecret:
		*r = Redact(s)
		return nil
	}
	return fmt.Errorf("unknown redact level %q (want: identity, secret, or a boolean)", s)
}

// Check is one manifest entry.
//
// Pointer fields are the ones whose default is not the zero value, so an
// absent field is distinguishable from an explicit one. `decisive` defaults
// to true for blockers and false otherwise, and `timeout_s: 0` must be an
// error rather than silently meaning 10.
type Check struct {
	ID          string   `json:"id"`
	Layer       *int     `json:"layer"`
	Title       string   `json:"title"`
	AppliesIf   Command  `json:"applies_if"`
	Probe       Command  `json:"probe"`
	Declared    Command  `json:"declared"`
	Expect      Expect   `json:"expect"`
	Severity    string   `json:"severity"`
	Decisive    *bool    `json:"decisive"`
	TaintedBy   []string `json:"tainted_by"`
	Remedy      string   `json:"remedy"`
	TimeoutS    *float64 `json:"timeout_s"`
	RedactLevel Redact   `json:"redact"`
	Mutating    *bool    `json:"mutating"`
	MutatingWhy string   `json:"mutating_why"`
	Isolate     *bool    `json:"isolate"`
	Volatility  string   `json:"volatility"`
	Notes       string   `json:"notes"`

	// Where this check came from, for error messages. Not a manifest field.
	sourceFile string
}

// Expect is held loosely on purpose.
//
// The runner must not interpret it, so it is validated for shape and then
// carried through untouched. The verdict engine unmarshals it into real
// predicate types in M2. Keeping it as a map here means the runner physically
// cannot evaluate it -- there is nothing to call.
type Expect struct {
	Type string
	Raw  map[string]any
}

func (e *Expect) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &e.Raw); err != nil {
		return fmt.Errorf("must be an object")
	}
	if t, ok := e.Raw["type"].(string); ok {
		e.Type = t
	}
	return nil
}

// IsDecisive applies the documented default: true for blockers, false
// otherwise, unless stated.
func (c *Check) IsDecisive() bool {
	if c.Decisive != nil {
		return *c.Decisive
	}
	return c.Severity == "blocker"
}

// Timeout applies the documented default.
func (c *Check) Timeout() float64 {
	if c.TimeoutS != nil {
		return *c.TimeoutS
	}
	return defaultTimeoutS
}

// VolatilityClass applies the documented default.
func (c *Check) VolatilityClass() string {
	if c.Volatility != "" {
		return c.Volatility
	}
	return defaultVolatility
}

// LayerOf resolves the check's layer, falling back to its file's.
func (c *Check) LayerOf(fileLayer int) int {
	if c.Layer != nil {
		return *c.Layer
	}
	return fileLayer
}

type manifestFile struct {
	Layer  int     `json:"layer"`
	Checks []Check `json:"checks"`
}

// Manifest is the whole loaded, validated check set.
type Manifest struct {
	Checks []Check
	Files  []string // relative paths, in load order, for the evidence header

	byID map[string]*Check
}

// Get returns a check by id, or nil.
func (m *Manifest) Get(id string) *Check {
	return m.byID[id]
}

// ValidationError collects every problem found, rather than stopping at the
// first. An author fixing a manifest wants the whole list; three round trips
// to see three typos is a bad tool.
type ValidationError struct {
	Problems []string
	Warnings []string
}

func (v *ValidationError) Error() string {
	return fmt.Sprintf("manifest invalid: %d problem(s)", len(v.Problems))
}

func (v *ValidationError) addf(format string, args ...any) {
	v.Problems = append(v.Problems, fmt.Sprintf(format, args...))
}

func (v *ValidationError) warnf(format string, args ...any) {
	v.Warnings = append(v.Warnings, fmt.Sprintf(format, args...))
}

// LoadManifest reads every *.json in dir except _schema.json, validates the
// result, and returns it. Any problem is fatal to the caller (exit 3).
//
// Warnings are returned alongside a nil error when nothing is actually wrong.
func LoadManifest(dir string) (*Manifest, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading manifest dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".json") || n == "_schema.json" {
			continue
		}
		names = append(names, n)
	}
	// Deterministic load order: evidence records appear in manifest order, and
	// "manifest order" has to mean something stable across machines.
	sort.Strings(names)

	if len(names) == 0 {
		return nil, nil, fmt.Errorf("no manifest files in %s", dir)
	}

	ve := &ValidationError{}
	m := &Manifest{byID: map[string]*Check{}}

	for _, name := range names {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", name, err)
		}

		// DisallowUnknownFields is rule 1, and it is the reason this is not
		// a plain json.Unmarshal. A typo silently defaulting is how timout_s
		// ends up meaning ten seconds forever.
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()

		var mf manifestFile
		if err := dec.Decode(&mf); err != nil {
			ve.addf("%s: %v", name, err)
			continue
		}

		if mf.Layer < 0 || mf.Layer > 10 {
			ve.addf("%s: layer %d outside 0-10", name, mf.Layer)
		}

		for i := range mf.Checks {
			c := &mf.Checks[i]
			c.sourceFile = name
			if c.Layer == nil {
				l := mf.Layer
				c.Layer = &l
			}
			m.Checks = append(m.Checks, *c)
		}
		m.Files = append(m.Files, name)
	}

	// A parse failure means the checks are not trustworthy enough to
	// cross-validate; report what we have and stop.
	if len(ve.Problems) > 0 {
		return nil, ve.Warnings, ve
	}

	m.validate(ve)

	if len(ve.Problems) > 0 {
		return nil, ve.Warnings, ve
	}

	for i := range m.Checks {
		m.byID[m.Checks[i].ID] = &m.Checks[i]
	}
	return m, ve.Warnings, nil
}

// validate applies MANIFEST_SCHEMA.md §8. Rules are checked in the order they
// are documented, so a reader can hold the two files side by side.
func (m *Manifest) validate(ve *ValidationError) {
	seen := map[string]string{} // id -> file that first defined it

	for i := range m.Checks {
		c := &m.Checks[i]
		where := fmt.Sprintf("%s: %s", c.sourceFile, c.ID)

		if c.ID == "" {
			ve.addf("%s: check %d has no id", c.sourceFile, i)
			continue
		}
		if !idPattern.MatchString(c.ID) {
			ve.addf("%s: id %q does not match %s", c.sourceFile, c.ID, idPattern)
		}

		// Rule 2: duplicate id, across all files.
		if first, dup := seen[c.ID]; dup {
			ve.addf("%s: duplicate id %q (already defined in %s)", c.sourceFile, c.ID, first)
		} else {
			seen[c.ID] = c.sourceFile
		}

		if strings.TrimSpace(c.Title) == "" {
			ve.addf("%s: title is required", where)
		}
		if !c.Probe.Set || strings.TrimSpace(c.Probe.Script) == "" {
			ve.addf("%s: probe is required", where)
		}

		// Rule 11: enums and timeout.
		switch c.Severity {
		case "blocker", "warning", "info":
		case "":
			ve.addf("%s: severity is required", where)
		default:
			ve.addf("%s: severity %q is not blocker|warning|info", where, c.Severity)
		}

		if c.Volatility != "" {
			switch c.Volatility {
			case "permanent", "stable", "session", "volatile", "expiring":
			default:
				ve.addf("%s: volatility %q is not a known class", where, c.Volatility)
			}
		}

		if c.TimeoutS != nil && *c.TimeoutS <= 0 {
			ve.addf("%s: timeout_s must be > 0, got %v", where, *c.TimeoutS)
		}

		if c.Layer != nil && (*c.Layer < 0 || *c.Layer > 10) {
			ve.addf("%s: layer %d outside 0-10", where, *c.Layer)
		}

		// Rule 5: remedy required unless info.
		//
		// A check that reports breakage without an action is half a tool.
		if c.Severity != "info" && c.Severity != "" && strings.TrimSpace(c.Remedy) == "" {
			ve.addf("%s: severity %q requires a remedy", where, c.Severity)
		}

		// Rule 6: the mutation override.
		if c.Mutating != nil {
			if *c.Mutating {
				ve.addf("%s: mutating must be false; there is no legal true "+
					"(a check that mutates does not belong in phase 1)", where)
			}
			if strings.TrimSpace(c.MutatingWhy) == "" {
				ve.addf("%s: mutating: false requires mutating_why "+
					"(an override without a stated reason is how a denylist rots)", where)
			}
		} else if c.MutatingWhy != "" {
			ve.addf("%s: mutating_why without mutating: false", where)
		}

		// Rule 6b: isolation is opt-in and only opt-in.
		//
		// `isolate: false` is the default, so writing it states nothing. It is
		// rejected rather than ignored because a reader who writes it plainly
		// believes it does something, and a field that silently means nothing
		// is worse than one that errors.
		if c.Isolate != nil && !*c.Isolate {
			ve.addf("%s: isolate must be true; false is the default and states nothing", where)
		}

		// Rules 7-9: expect shape and interpolation.
		m.validateExpect(ve, c, where)

		// Rule 7 again, for remedy, which interpolates by the same rules.
		for _, tok := range interpolationTokens(c.Remedy) {
			if err := checkToken(tok, c); err != nil {
				ve.addf("%s: remedy: %v", where, err)
			}
		}

		// Warning: $probe inside a probe. Legal shell, almost certainly a
		// misunderstanding of §5 -- interpolation happens in the verdict
		// engine, never in the shell.
		for _, f := range []struct {
			name string
			cmd  Command
		}{{"probe", c.Probe}, {"declared", c.Declared}, {"applies_if", c.AppliesIf}} {
			if !f.cmd.Set {
				continue
			}
			if strings.Contains(f.cmd.Script, "$probe") || strings.Contains(f.cmd.Script, "$declared") {
				ve.warnf("%s: %s contains $probe or $declared; interpolation "+
					"happens in the verdict engine, not the shell", where, f.name)
			}
		}

		// Warning: blocker that does not count.
		if c.Severity == "blocker" && c.Decisive != nil && !*c.Decisive {
			ve.warnf("%s: severity blocker with decisive: false", where)
		}
	}

	m.validateTaint(ve, seen)
}

// validateTaint covers rules 3 and 4: dangling references and cycles.
func (m *Manifest) validateTaint(ve *ValidationError, known map[string]string) {
	// Rule 3: every tainted_by target must exist. Checked here rather than at
	// verdict time so a rename fails loudly at load.
	edges := map[string][]string{}
	for i := range m.Checks {
		c := &m.Checks[i]
		for _, dep := range c.TaintedBy {
			if _, ok := known[dep]; !ok {
				ve.addf("%s: %s: tainted_by names %q, which does not exist",
					c.sourceFile, c.ID, dep)
				continue
			}
			if dep == c.ID {
				ve.addf("%s: %s: tainted_by names itself", c.sourceFile, c.ID)
				continue
			}
			edges[c.ID] = append(edges[c.ID], dep)
		}
	}

	// Rule 4: cycles. Iterative DFS with an explicit stack, reporting the
	// cycle by naming its members rather than resolving it arbitrarily.
	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // done
	)
	color := map[string]int{}
	var path []string

	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = grey
		path = append(path, id)
		for _, dep := range edges[id] {
			switch color[dep] {
			case grey:
				// Found it. Report from the point the cycle closes.
				start := 0
				for i, p := range path {
					if p == dep {
						start = i
						break
					}
				}
				cycle := append(append([]string{}, path[start:]...), dep)
				ve.addf("tainted_by cycle: %s", strings.Join(cycle, " -> "))
				return true
			case white:
				if visit(dep) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		color[id] = black
		return false
	}

	// Sorted for a deterministic error message.
	var ids []string
	for i := range m.Checks {
		ids = append(ids, m.Checks[i].ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			path = path[:0]
			if visit(id) {
				break // one cycle is enough; the manifest is already invalid
			}
		}
	}
}

// expectFields lists the required and optional keys per predicate type.
// Rule 9 is enforced from this table, so adding a predicate is one entry here
// plus the evaluator in M2.
var expectFields = map[string]struct {
	required []string
	optional []string
}{
	"equals":           {required: []string{"actual", "value"}},
	"matches":          {required: []string{"actual", "pattern"}, optional: []string{"invert"}},
	"one_of":           {required: []string{"actual", "values"}},
	"non_empty":        {required: []string{"actual"}},
	"absent":           {required: []string{"actual"}},
	"numeric_lt":       {required: []string{"actual", "value"}, optional: []string{"unit"}},
	"numeric_gt":       {required: []string{"actual", "value"}, optional: []string{"unit"}},
	"semver_satisfies": {required: []string{"actual", "range"}},
	"exit_code":        {required: []string{"of"}, optional: []string{"value", "values"}},
}

func (m *Manifest) validateExpect(ve *ValidationError, c *Check, where string) {
	if c.Expect.Raw == nil {
		ve.addf("%s: expect is required", where)
		return
	}
	if c.Expect.Type == "" {
		ve.addf("%s: expect.type is required", where)
		return
	}

	spec, ok := expectFields[c.Expect.Type]
	if !ok {
		var known []string
		for k := range expectFields {
			known = append(known, k)
		}
		sort.Strings(known)
		ve.addf("%s: unknown expect.type %q (known: %s)",
			where, c.Expect.Type, strings.Join(known, ", "))
		return
	}

	allowed := map[string]bool{"type": true}
	for _, f := range spec.required {
		allowed[f] = true
	}
	for _, f := range spec.optional {
		allowed[f] = true
	}

	for _, f := range spec.required {
		if _, present := c.Expect.Raw[f]; !present {
			ve.addf("%s: expect type %q requires field %q", where, c.Expect.Type, f)
		}
	}
	for k := range c.Expect.Raw {
		if !allowed[k] {
			ve.addf("%s: expect type %q has no field %q", where, c.Expect.Type, k)
		}
	}

	// exit_code needs exactly one of value/values.
	if c.Expect.Type == "exit_code" {
		_, hasOne := c.Expect.Raw["value"]
		_, hasMany := c.Expect.Raw["values"]
		if hasOne == hasMany {
			ve.addf("%s: expect type exit_code needs exactly one of value or values", where)
		}
		if of, _ := c.Expect.Raw["of"].(string); of != "probe" && of != "declared" {
			ve.addf("%s: expect.of must be probe or declared, got %q", where, of)
		}
	}

	// one_of needs a non-empty values array.
	if c.Expect.Type == "one_of" {
		vals, _ := c.Expect.Raw["values"].([]any)
		if len(vals) == 0 {
			ve.addf("%s: expect type one_of requires a non-empty values array", where)
		}
	}

	// Rule 10: an invalid regex is a load error, not a runtime UNKNOWN.
	if c.Expect.Type == "matches" {
		if pat, ok := c.Expect.Raw["pattern"].(string); ok {
			if _, err := regexp.Compile(pat); err != nil {
				ve.addf("%s: expect.pattern is not a valid regexp: %v", where, err)
			}
		} else if _, present := c.Expect.Raw["pattern"]; present {
			ve.addf("%s: expect.pattern must be a string", where)
		}
	}

	// Rules 7 and 8: interpolation tokens, everywhere a string appears.
	for k, v := range c.Expect.Raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		for _, tok := range interpolationTokens(s) {
			if err := checkToken(tok, c); err != nil {
				ve.addf("%s: expect.%s: %v", where, k, err)
			}
		}
	}
}

// tokenPattern finds $name occurrences, skipping the $$ escape.
var tokenPattern = regexp.MustCompile(`\$\$|\$([a-z_]+)`)

// interpolationTokens returns the token names used in s, per §5.
func interpolationTokens(s string) []string {
	var out []string
	for _, mm := range tokenPattern.FindAllStringSubmatch(s, -1) {
		if mm[0] == "$$" {
			continue // escaped literal
		}
		out = append(out, mm[1])
	}
	return out
}

// validTokens is §5's table. Anything else is rule 8.
var validTokens = map[string]bool{
	"probe":           true,
	"declared":        true,
	"probe_stderr":    true,
	"declared_stderr": true,
	"probe_exit":      true,
	"declared_exit":   true,
}

func checkToken(tok string, c *Check) error {
	if !validTokens[tok] {
		return fmt.Errorf("unknown interpolation token $%s", tok)
	}
	// Rule 7: referencing a capture that was never made.
	if !c.Declared.Set && strings.HasPrefix(tok, "declared") {
		return fmt.Errorf("$%s referenced but the check has no declared field", tok)
	}
	return nil
}
