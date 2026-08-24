package main

import (
	"strings"
	"testing"
)

// renderTo renders a report with colour off, as a pipe would see it.
func renderTo(rep *Report) string {
	var b strings.Builder
	(&Renderer{Color: false, Width: 78}).Render(&b, rep)
	return b.String()
}

func rep(vs ...Verdict) *Report {
	r := &Report{Verdicts: vs, Counts: map[State]int{}}
	for i := range vs {
		r.Counts[vs[i].State]++
	}
	return r
}

// ============================================================
// The invariants
// ============================================================

// UNKNOWN is never rendered the same as GO. Different glyph, its own section,
// and language that says so -- a silent omission reading as green converts
// "I don't know" into "I verified".
func TestUnknownIsVisuallyDistinctFromGo(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "a", Title: "Passed", State: StateGo},
		Verdict{ID: "b", Title: "Could not check", State: StateUnknown,
			Reason: ReasonToolAbsent, Detail: "not installed", Decisive: true},
	))

	if glyphGo == glyphUnknown {
		t.Fatal("GO and UNKNOWN share a glyph")
	}
	if !strings.Contains(out, glyphUnknown+" Could not check") {
		t.Errorf("the unknown check is not rendered with its own glyph:\n%s", out)
	}
	if !strings.Contains(out, "UNKNOWN") {
		t.Error("no UNKNOWN section")
	}
	// It must say what unknown means, not leave the reader to assume.
	if !strings.Contains(out, "not the same as verified-good") {
		t.Error("the UNKNOWN section does not state that it is not a pass")
	}
	// A decisive unknown must not produce a bare GO headline.
	if strings.Contains(out, "VERDICT  GO\n") {
		t.Errorf("a decisive UNKNOWN rendered as a clean GO:\n%s", out)
	}
}

// A non-decisive check that FAILED must still appear. Collapsing it into a
// count would silently drop a finding.
func TestNonDecisiveFailureIsStillShown(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "a", Title: "Passed", State: StateGo},
		Verdict{ID: "b", Title: "PATH has duplicates", State: StateNoGo,
			Severity: "info", Decisive: false, Detail: `"9" (want "0")`},
	))

	if !strings.Contains(out, "PATH has duplicates") {
		t.Errorf("a non-decisive failure was silently collapsed:\n%s", out)
	}
	if !strings.Contains(out, "NOTED") {
		t.Error("no NOTED section for non-decisive findings")
	}
	// But it must not change the verdict.
	if !strings.Contains(out, "VERDICT  GO") {
		t.Errorf("a non-decisive failure changed the verdict:\n%s", out)
	}
}

// Severity order, not layer order. A blocker in layer 9 outranks a warning in
// layer 0.
func TestOrderedBySeverityNotLayer(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "low", Title: "Layer zero warning", Layer: 0,
			State: StateNoGo, Severity: "warning", Detail: "d"},
		Verdict{ID: "high", Title: "Layer nine blocker", Layer: 9,
			State: StateNoGo, Severity: "blocker", Decisive: true, Remedy: "fix"},
	))

	blocker := strings.Index(out, "Layer nine blocker")
	warning := strings.Index(out, "Layer zero warning")
	if blocker < 0 || warning < 0 {
		t.Fatalf("both findings should appear:\n%s", out)
	}
	if blocker > warning {
		t.Errorf("the layer-9 blocker rendered below the layer-0 warning:\n%s", out)
	}
}

// Every NO-GO carries its remedy. A report of breakage with no action is half
// a tool.
func TestEveryNoGoCarriesItsRemedy(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "a", Title: "Broken", State: StateNoGo, Severity: "blocker",
			Decisive: true, Detail: "d", Remedy: "run the fix command"},
	))
	if !strings.Contains(out, "run the fix command") {
		t.Errorf("the remedy is missing:\n%s", out)
	}
	if !strings.Contains(out, "→") {
		t.Error("the remedy is not marked as an action")
	}
}

// Taint renders as a rollup on the cause, not as N separate lines.
func TestTaintRendersAsRollup(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "cause", Title: "Clock skew", State: StateNoGo,
			Severity: "blocker", Decisive: true, Detail: "702s",
			Remedy: "sync", TaintedCount: 38},
	))
	if !strings.Contains(out, "taints 38 checks below") {
		t.Errorf("no taint rollup:\n%s", out)
	}
}

// A tainted check names the ROOT cause, not the intermediate that is itself a
// victim -- otherwise the reader is sent to fix a symptom.
func TestTaintedLineNamesRootCause(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "dep", Title: "Registry reachable", State: StateUnknown,
			Reason: ReasonTainted, TaintedBy: "mid.check", RootCause: "clock.skew",
			Decisive: true},
	))
	if !strings.Contains(out, "clock.skew") {
		t.Errorf("the root cause is not named:\n%s", out)
	}
	if strings.Contains(out, "mid.check") {
		t.Errorf("the intermediate is named instead of the root cause:\n%s", out)
	}
}

// ============================================================
// Degradation
// ============================================================

func TestNoColorProducesNoEscapes(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "a", Title: "Broken", State: StateNoGo, Severity: "blocker",
			Decisive: true, Remedy: "fix"},
		Verdict{ID: "b", Title: "Unknown", State: StateUnknown,
			Reason: ReasonTimeout, Decisive: true},
		Verdict{ID: "c", Title: "Fine", State: StateGo},
	))
	if strings.Contains(out, "\033[") {
		t.Errorf("ANSI escapes leaked into non-colour output:\n%q", out)
	}
}

func TestColorProducesEscapes(t *testing.T) {
	var b strings.Builder
	(&Renderer{Color: true, Width: 78}).Render(&b, rep(
		Verdict{ID: "a", Title: "Broken", State: StateNoGo, Severity: "blocker",
			Decisive: true, Remedy: "fix"},
	))
	if !strings.Contains(b.String(), "\033[") {
		t.Error("colour was requested but no escapes were emitted")
	}
}

// The glyphs must differ from each other in shape, so the output survives a
// colourblind reader and a log with colour stripped.
func TestGlyphsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, pair := range []struct{ name, glyph string }{
		{"GO", glyphGo}, {"NO-GO", glyphNoGo},
		{"UNKNOWN", glyphUnknown}, {"N/A", glyphNA},
	} {
		if prev, dup := seen[pair.glyph]; dup {
			t.Errorf("%s and %s share the glyph %q", prev, pair.name, pair.glyph)
		}
		seen[pair.glyph] = pair.name
	}
}

// ============================================================
// The headline
// ============================================================

func TestVerdictHeadline(t *testing.T) {
	cases := []struct {
		name string
		vs   []Verdict
		want string
	}{
		{"clean", []Verdict{{State: StateGo}}, "GO"},
		{"blocker", []Verdict{
			{State: StateNoGo, Severity: "blocker", Decisive: true, Remedy: "r"},
		}, "NO-GO"},
		{"decisive unknown", []Verdict{
			{State: StateUnknown, Decisive: true, Reason: ReasonTimeout},
		}, "GO-WITH-UNKNOWNS"},
		{"non-decisive unknown stays GO", []Verdict{
			{State: StateUnknown, Decisive: false, Reason: ReasonTimeout, Severity: "info"},
		}, "GO"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderTo(rep(tc.vs...))
			line := firstLineContaining(out, "VERDICT")
			if !strings.Contains(line, tc.want) {
				t.Errorf("headline = %q, want it to contain %q", line, tc.want)
			}
		})
	}
}

// The unbuilt sections must say they are unbuilt. An empty STACK or PROPOSED
// that could read as "nothing needed" would be a false green in the most
// prominent position in the output.
func TestPlaceholderSectionsStateWhyTheyAreEmpty(t *testing.T) {
	out := renderTo(rep(Verdict{ID: "a", Title: "Fine", State: StateGo}))

	for _, want := range []string{
		"STACK", "not yet derived",
		"PROPOSED", "remediation lands at",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

// A blocked golden path says so rather than implying it was tried and passed.
func TestGoldenPathBlocked(t *testing.T) {
	out := renderTo(rep(
		Verdict{ID: "a", Title: "Broken", State: StateNoGo,
			Severity: "blocker", Decisive: true, Remedy: "fix"},
	))
	if !strings.Contains(out, "NOT ATTEMPTED (blocked)") {
		t.Errorf("a blocked golden path is not declared:\n%s", out)
	}
}

// ============================================================
// Markdown
// ============================================================

func TestMarkdownMatchesTerminalContent(t *testing.T) {
	r := rep(
		Verdict{ID: "a", Title: "Clock skew", State: StateNoGo, Severity: "blocker",
			Decisive: true, Detail: "702s", Remedy: "sync the clock", TaintedCount: 2},
		Verdict{ID: "b", Title: "Node version", State: StateUnknown,
			Reason: ReasonTainted, RootCause: "a", Decisive: true},
		Verdict{ID: "c", Title: "Fine", State: StateGo},
	)

	var b strings.Builder
	RenderMarkdown(&b, r, "2026-08-24T14:02:11Z")
	md := b.String()

	for _, want := range []string{
		"# Ferret status",
		"**NO-GO**",
		"Clock skew",
		"sync the clock",
		"taints 2 checks below",
		"Node version",
		"not the same as verified-good",
		"2026-08-24T14:02:11Z",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("status.md is missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "\033[") {
		t.Error("ANSI escapes leaked into status.md")
	}
}

// ============================================================
// Helpers
// ============================================================

func TestWrapKeepsCommandsIntact(t *testing.T) {
	// A remedy is a command. Breaking it mid-token would produce something a
	// reader could paste and run wrongly, so a single long token overflows
	// rather than being cut.
	long := "someverylongcommandwithoutspacesatallthatcannotbebroken"
	got := wrap(long, 20)
	if len(got) != 1 || got[0] != long {
		t.Errorf("an unbreakable token was split: %q", got)
	}

	got = wrap("git config --global user.email you@example.com", 20)
	if len(got) < 2 {
		t.Errorf("a breakable line was not wrapped: %q", got)
	}
	for _, l := range got {
		if strings.HasPrefix(l, " ") {
			t.Errorf("wrapped line has leading space: %q", l)
		}
	}
}

func firstLineContaining(s, want string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, want) {
			return l
		}
	}
	return ""
}
