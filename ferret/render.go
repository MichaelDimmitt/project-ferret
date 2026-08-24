// The glance: the product. Everything before this is plumbing.
//
// One question, answered in ten seconds: can I proceed here right now, and if
// not, what is broken and what do I do about it.
//
// Two rules shape every decision in this file:
//
//   - ORDERED BY SEVERITY, NEVER BY LAYER. A reader scanning for "what stops
//     me" must not have to read past layer 0 trivia to find it. Layer is an
//     authoring concern; it does not survive into the output.
//   - UNKNOWN IS NEVER GREEN. Different glyph, different colour, its own
//     section. Every silent omission is a false green, and a false green
//     converts "I don't know" into "I verified" for an agent about to change
//     the machine.
//
// Contract: docs/plan/PLAN.md M4, docs/design/ARCHITECTURE.md §11.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Glyphs. Deliberately distinct in SHAPE, not only in colour -- the output has
// to survive a pipe, a CI log, a paste into a ticket, and a colourblind
// reader. Colour is an enhancement; the glyph is the signal.
const (
	glyphGo      = "✓"
	glyphNoGo    = "✗"
	glyphUnknown = "?"
	glyphNA      = "–"
)

// ANSI colours, applied only when stdout is a terminal.
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
)

// Renderer writes the glance.
type Renderer struct {
	Color bool // ANSI escapes; off in a pipe, off when NO_COLOR is set
	Width int  // wrap width for remedies; 0 means no wrapping
}

// NewRenderer configures from the environment, per the rule that output must
// degrade in a pipe and in a terminal without colour.
func NewRenderer(out *os.File) *Renderer {
	return &Renderer{
		Color: isTerminal(out) && os.Getenv("NO_COLOR") == "",
		Width: 78,
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (r *Renderer) paint(s, color string) string {
	if !r.Color || color == "" {
		return s
	}
	return color + s + ansiReset
}

// glyphFor returns the glyph and its colour for a state.
func (r *Renderer) glyphFor(s State) (string, string) {
	switch s {
	case StateGo:
		return glyphGo, ansiGreen
	case StateNoGo:
		return glyphNoGo, ansiRed
	case StateUnknown:
		return glyphUnknown, ansiYellow
	default:
		return glyphNA, ansiDim
	}
}

// Render writes the full glance.
func (r *Renderer) Render(w io.Writer, rep *Report) {
	// Severity order throughout: the answer, then what stops you, then what
	// could not be determined, then what merely bothers you. The golden path
	// and the counts close it out, because they summarise rather than report.
	r.renderStack(w)
	r.renderVerdict(w, rep)
	r.renderBlockers(w, rep)
	r.renderUnknowns(w, rep)
	r.renderWarnings(w, rep)
	r.renderNoted(w, rep)
	r.renderProposed(w)
	r.renderGoldenPath(w, rep)
	r.renderCounts(w, rep)
}

// renderStack is a placeholder for M10.5.
//
// It is shown rather than omitted so the format's final shape is visible from
// the start, and it states WHY it is empty. "Not yet derived" and "nothing
// needed" are different facts, and a section that could be confused for the
// second would be a false green in the tool's most prominent position.
func (r *Renderer) renderStack(w io.Writer) {
	fmt.Fprintf(w, "%s\n", r.paint("STACK — derived from this repo", ansiBold))
	fmt.Fprintf(w, "  %s\n\n",
		r.paint("not yet derived — stack derivation lands at M10.5", ansiDim))
}

// renderProposed is a placeholder for M11.
//
// Same reasoning: an empty PROPOSED must never read as "nothing to install."
// Nothing installs before this list is approved, so a list that is absent
// because the feature is unbuilt has to say so.
func (r *Renderer) renderProposed(w io.Writer) {
	fmt.Fprintf(w, "%s\n", r.paint("PROPOSED", ansiBold))
	fmt.Fprintf(w, "  %s\n\n",
		r.paint("nothing proposed — remediation lands at M11", ansiDim))
}

// renderVerdict is the top line: the answer, before any detail.
func (r *Renderer) renderVerdict(w io.Writer, rep *Report) {
	blockers := rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "blocker"
	})
	unknowns := rep.filter(func(v *Verdict) bool {
		return v.State == StateUnknown && v.Decisive
	})

	var headline, color string
	switch {
	case len(blockers) > 0:
		headline, color = "NO-GO", ansiRed
	case len(unknowns) > 0:
		headline, color = "GO-WITH-UNKNOWNS", ansiYellow
	default:
		headline, color = "GO", ansiGreen
	}

	var parts []string
	if n := len(blockers); n > 0 {
		parts = append(parts, fmt.Sprintf("%d blocker%s", n, plural(n)))
	}
	if n := len(unknowns); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown%s", n, plural(n)))
	}

	line := r.paint(headline, color+ansiBold)
	if len(parts) > 0 {
		line += " — " + strings.Join(parts, ", ")
	}
	fmt.Fprintf(w, "%s  %s\n\n", r.paint("VERDICT", ansiBold), line)
}

func (r *Renderer) renderBlockers(w io.Writer, rep *Report) {
	// Warnings that failed appear here too, below the blockers -- a NO-GO is a
	// NO-GO, and burying a failing warning in a separate section further down
	// makes the reader hunt for it.
	blockers := rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "blocker"
	})
	if len(blockers) == 0 {
		return
	}

	fmt.Fprintf(w, "%s\n", r.paint("BLOCKERS", ansiBold+ansiRed))
	r.renderAligned(w, blockers)
	fmt.Fprintln(w)
}

func (r *Renderer) renderUnknowns(w io.Writer, rep *Report) {
	unknowns := rep.filter(func(v *Verdict) bool { return v.State == StateUnknown })
	if len(unknowns) == 0 {
		return
	}

	// Tainted unknowns are consequences, not independent findings. They sort
	// last so the reader sees the things that need a decision first.
	sort.SliceStable(unknowns, func(i, j int) bool {
		ti := unknowns[i].Reason == ReasonTainted
		tj := unknowns[j].Reason == ReasonTainted
		if ti != tj {
			return !ti
		}
		return false
	})

	// Say how many of these actually hold up the verdict. "3 unknown" in one
	// place and "2 unknowns" in the headline reads as a bug unless the
	// difference is stated: non-decisive unknowns are context, not blockers.
	decisive := 0
	for i := range unknowns {
		if unknowns[i].Decisive {
			decisive++
		}
	}
	header := fmt.Sprintf("UNKNOWN  (%d)", len(unknowns))
	if decisive != len(unknowns) {
		header = fmt.Sprintf("UNKNOWN  (%d — %d decisive)", len(unknowns), decisive)
	}

	fmt.Fprintf(w, "%s\n", r.paint(header, ansiBold+ansiYellow))
	fmt.Fprintf(w, "  %s\n",
		r.paint("could not verify — not the same as verified-good", ansiDim))
	r.renderAligned(w, unknowns)
	fmt.Fprintln(w)
}

func (r *Renderer) renderWarnings(w io.Writer, rep *Report) {
	warnings := rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "warning"
	})
	if len(warnings) == 0 {
		return
	}

	fmt.Fprintf(w, "%s\n", r.paint("WARNINGS", ansiBold))
	r.renderAligned(w, warnings)
	fmt.Fprintln(w)
}

// renderNoted surfaces non-decisive findings.
//
// A non-decisive check that FAILED is still a finding. Collapsing it into
// "n passed" would silently drop it, which is the tool's core sin -- context
// means "does not change the verdict", not "not worth mentioning".
func (r *Renderer) renderNoted(w io.Writer, rep *Report) {
	noted := rep.filter(func(v *Verdict) bool {
		return !v.Decisive && (v.State == StateNoGo || v.State == StateUnknown) &&
			v.Severity == "info"
	})
	if len(noted) == 0 {
		return
	}
	fmt.Fprintf(w, "%s\n", r.paint("NOTED  (does not affect the verdict)", ansiDim))
	r.renderAligned(w, noted)
	fmt.Fprintln(w)
}

// renderCounts collapses everything that needs no action into one line.
// Non-decisive passing checks are captured in full in evidence.json; putting
// them on screen would bury the lines that matter.
func (r *Renderer) renderCounts(w io.Writer, rep *Report) {
	passed := rep.Counts[StateGo]
	na := rep.Counts[StateNA]

	var parts []string
	if passed > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", passed))
	}
	if na > 0 {
		parts = append(parts, fmt.Sprintf("%d not applicable", na))
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(w, "%s\n", r.paint(strings.Join(parts, ", "), ansiDim))
}

// renderGoldenPath states the real verdict, or why it was not attempted.
//
// Watching a build fail for a reason you already know is the exact waste this
// tool exists to prevent, so a blocked golden path says so rather than running.
func (r *Renderer) renderGoldenPath(w io.Writer, rep *Report) {
	blocked := len(rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "blocker"
	})) > 0

	label := r.paint("GOLDEN PATH", ansiBold)
	switch {
	case blocked:
		fmt.Fprintf(w, "%s  %s\n", label,
			r.paint("NOT ATTEMPTED (blocked)", ansiRed))
	default:
		fmt.Fprintf(w, "%s  %s\n", label,
			r.paint("not attempted — opt in with --golden (lands at M13)", ansiDim))
	}
	fmt.Fprintln(w)
}

// renderAligned prints a group with the detail column lined up, so a reader
// scans down one edge instead of chasing ragged text.
func (r *Renderer) renderAligned(w io.Writer, vs []Verdict) {
	width := 0
	for i := range vs {
		if n := len(titleOf(&vs[i])); n > width {
			width = n
		}
	}
	// Past this, alignment costs more than it buys: the detail gets pushed off
	// the right edge on a narrow terminal.
	if width > 34 {
		width = 34
	}
	for i := range vs {
		r.renderLine(w, vs[i], width)
	}
}

func titleOf(v *Verdict) string {
	if v.Title != "" {
		return v.Title
	}
	return v.ID
}

// shortReason turns a verdict into the phrase a reader needs, not the sentence
// the engine produced. A tainted check's detail explains the mechanism; on
// screen, the useful fact is which check to go fix.
func shortReason(v *Verdict) string {
	if v.State == StateUnknown && v.Reason == ReasonTainted {
		// The ROOT cause, never the immediate parent: an intermediate is
		// itself a victim, and naming it sends the reader to fix a symptom.
		if cause := v.RootCause; cause != "" {
			return "untrustworthy while " + cause + " fails"
		}
		if v.TaintedBy != "" {
			return "untrustworthy while " + v.TaintedBy + " fails"
		}
	}
	return v.Detail
}

// renderLine is one finding: glyph, title, what was found, and what to do.
func (r *Renderer) renderLine(w io.Writer, v Verdict, pad int) {
	glyph, color := r.glyphFor(v.State)
	title := titleOf(&v)

	// The detail is what makes a line actionable -- "Node 18.19, want 20"
	// rather than "Node version wrong". Kept on the same line so the finding
	// is one scannable row.
	line := fmt.Sprintf("  %s %s", r.paint(glyph, color), title)
	if detail := shortReason(&v); detail != "" {
		if gap := pad - len(title); gap > 0 {
			line += strings.Repeat(" ", gap)
		}
		line += r.paint("  "+detail, ansiDim)
	}
	fmt.Fprintln(w, line)

	// A taint rollup on the cause, so forty consequences render as one number
	// rather than forty lines.
	if v.TaintedCount > 0 {
		fmt.Fprintf(w, "      %s\n",
			r.paint(fmt.Sprintf("→ taints %d check%s below",
				v.TaintedCount, plural(v.TaintedCount)), ansiDim))
	}

	// Every NO-GO carries its remedy. A status check that reports breakage
	// without an action is half a tool.
	if v.Remedy != "" {
		for _, l := range wrap(v.Remedy, r.Width-6) {
			fmt.Fprintf(w, "      %s\n", r.paint("→ "+l, ""))
		}
	}
}

// filter returns verdicts matching a predicate, in report order.
func (rep *Report) filter(keep func(*Verdict) bool) []Verdict {
	var out []Verdict
	for i := range rep.Verdicts {
		if keep(&rep.Verdicts[i]) {
			out = append(out, rep.Verdicts[i])
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// wrap breaks a string at word boundaries. Remedies are commands, so a
// too-narrow width is worse than a long line -- never break below 20.
func wrap(s string, width int) []string {
	if width < 20 || len(s) <= width {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		cut := strings.LastIndex(s[:width], " ")
		if cut <= 0 {
			break // one long token: let it overflow rather than corrupt it
		}
		lines = append(lines, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	return append(lines, s)
}

// RenderMarkdown writes status.md.
//
// Same content and same ordering as the terminal glance, without ANSI. It is
// regenerated every run and gitignored -- it describes this machine at this
// moment, and it is the artifact most likely to be pasted into a ticket.
func RenderMarkdown(w io.Writer, rep *Report, capturedAt string) {
	plain := &Renderer{Color: false, Width: 0}

	fmt.Fprintf(w, "# Ferret status\n\n")
	if capturedAt != "" {
		fmt.Fprintf(w, "Evidence captured %s.\n\n", capturedAt)
	}

	blockers := rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "blocker"
	})
	unknowns := rep.filter(func(v *Verdict) bool { return v.State == StateUnknown })
	warnings := rep.filter(func(v *Verdict) bool {
		return v.State == StateNoGo && v.Severity == "warning"
	})

	switch {
	case len(blockers) > 0:
		fmt.Fprintf(w, "**NO-GO** — %d blocker%s.\n\n", len(blockers), plural(len(blockers)))
	case len(unknowns) > 0:
		fmt.Fprintf(w, "**GO-WITH-UNKNOWNS** — %d unresolved.\n\n", len(unknowns))
	default:
		fmt.Fprintf(w, "**GO**\n\n")
	}

	section := func(title string, vs []Verdict, note string) {
		if len(vs) == 0 {
			return
		}
		fmt.Fprintf(w, "## %s\n\n", title)
		if note != "" {
			fmt.Fprintf(w, "*%s*\n\n", note)
		}
		for _, v := range vs {
			glyph, _ := plain.glyphFor(v.State)
			fmt.Fprintf(w, "- %s **%s**", glyph, v.Title)
			if v.Detail != "" {
				fmt.Fprintf(w, " — %s", v.Detail)
			}
			fmt.Fprintln(w)
			if v.TaintedCount > 0 {
				fmt.Fprintf(w, "  - taints %d check%s below\n",
					v.TaintedCount, plural(v.TaintedCount))
			}
			if v.Remedy != "" {
				fmt.Fprintf(w, "  - remedy: `%s`\n", v.Remedy)
			}
		}
		fmt.Fprintln(w)
	}

	section("Blockers", blockers, "")
	section("Unknown", unknowns, "Could not verify. This is not the same as verified-good.")
	section("Warnings", warnings, "")

	fmt.Fprintf(w, "## Context\n\n%d passed, %d not applicable.\n\n",
		rep.Counts[StateGo], rep.Counts[StateNA])

	fmt.Fprintf(w, "---\n\nNot yet implemented: stack derivation (M10.5), "+
		"remediation proposals (M11), golden path (M13).\n")
}
