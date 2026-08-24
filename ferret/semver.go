// Semver comparison, scoped to what the manifest actually needs.
//
// This is deliberately NOT a general semver library. MANIFEST_SCHEMA.md §4
// documents a small table of supported range forms, and anything outside it
// resolves UNKNOWN(probe_error) with the unparsed range in the reason rather
// than being guessed at.
//
// Guessing is the failure mode that matters here. A range parser that quietly
// approximates `>=1.2 <2.0.0-0 || ^3` produces a verdict nobody can trace, and
// a wrong GO is worse than an honest UNKNOWN.
//
// Contract: docs/design/MANIFEST_SCHEMA.md §4.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semver. Build metadata is captured but ignored in
// comparison, per the semver spec.
type Version struct {
	Major, Minor, Patch int
	Prerelease          string
	raw                 string
}

// parseVersion accepts a version string, tolerating a leading v and trailing
// junk that real tools emit ("go version go1.23.12 darwin/arm64" is handled by
// the probe, not here, but "v20.11.0" and "20.11" are common).
//
// Missing components default to 0, so "20" parses as 20.0.0. That is the right
// reading for an ACTUAL version; for a RANGE, "20" means a prefix match, which
// is handled separately in satisfies.
func parseVersion(s string) (Version, error) {
	v := Version{raw: s}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return v, fmt.Errorf("empty version")
	}

	// Build metadata is ignored in precedence comparison per semver §10.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.Prerelease = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, fmt.Errorf("too many components in %q", v.raw)
	}
	dst := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		if p == "" {
			return v, fmt.Errorf("empty component in %q", v.raw)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, fmt.Errorf("%q is not a version", v.raw)
		}
		if n < 0 {
			return v, fmt.Errorf("negative component in %q", v.raw)
		}
		*dst[i] = n
	}
	return v, nil
}

// compare returns -1, 0, or 1.
//
// Prerelease handling per semver §11: a prerelease sorts BEFORE its release,
// so 20.0.0-rc1 < 20.0.0. Getting this backwards would let a release candidate
// satisfy a range meant to exclude it.
func (a Version) compare(b Version) int {
	for _, pair := range [][2]int{
		{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch},
	} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return comparePrerelease(a.Prerelease, b.Prerelease)
}

func comparePrerelease(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1 // no prerelease outranks any prerelease
	case b == "":
		return -1
	}

	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		an, aerr := strconv.Atoi(ap[i])
		bn, berr := strconv.Atoi(bp[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aerr == nil:
			return -1 // numeric identifiers sort below alphanumeric
		case berr == nil:
			return 1
		default:
			if c := strings.Compare(ap[i], bp[i]); c != 0 {
				return c
			}
		}
	}
	// A longer prerelease with an equal prefix sorts higher.
	switch {
	case len(ap) < len(bp):
		return -1
	case len(ap) > len(bp):
		return 1
	}
	return 0
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	return s
}

// satisfies reports whether actual meets the range.
//
// The error is the important return value: it means "this range is outside the
// documented table," and the caller turns it into UNKNOWN(probe_error) rather
// than a verdict. An unparseable range must never resolve to NO-GO, because
// "the check is broken" and "the machine is wrong" are different findings.
func satisfies(actual string, rangeSpec string) (bool, error) {
	av, err := parseVersion(actual)
	if err != nil {
		return false, fmt.Errorf("actual: %w", err)
	}

	spec := strings.TrimSpace(rangeSpec)

	// §4: an empty range is UNKNOWN, not "anything goes". Nothing was
	// declared, so nothing can be satisfied -- a different fact from a wrong
	// version, and it must not render as NO-GO.
	if spec == "" {
		return false, fmt.Errorf("no version was declared")
	}
	if spec == "*" {
		return true, nil
	}

	// A || B: either alternative satisfying is enough. An unparseable
	// alternative still fails the whole range, because a range half of which
	// is gibberish is not one this parser can honestly evaluate.
	for _, alt := range strings.Split(spec, "||") {
		ok, err := satisfiesOne(av, strings.TrimSpace(alt))
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func satisfiesOne(av Version, spec string) (bool, error) {
	if spec == "" {
		return false, fmt.Errorf("empty range alternative")
	}
	if spec == "*" {
		return true, nil
	}

	// Hyphen ranges are checked BEFORE the conjunction split, because
	// "1.0.0 - 2.0.0" is space-separated and would otherwise be torn into
	// three clauses and misread. Outside the documented table either way.
	if strings.Contains(spec, " - ") {
		return false, fmt.Errorf("hyphen ranges are not supported: %q "+
			"(see MANIFEST_SCHEMA.md §4; use >= and < instead)", spec)
	}

	// A space-separated conjunction: ">=20.11.0 <21.0.0". Every clause must
	// hold. Checked before the single-clause forms so the parts are handled
	// individually.
	if fields := strings.Fields(spec); len(fields) > 1 {
		for _, f := range fields {
			ok, err := satisfiesOne(av, f)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}

	switch {
	case strings.HasPrefix(spec, "^"):
		return caretRange(av, spec[1:])
	case strings.HasPrefix(spec, "~"):
		return tildeRange(av, spec[1:])
	case strings.HasPrefix(spec, ">="):
		return comparator(av, spec[2:], func(c int) bool { return c >= 0 })
	case strings.HasPrefix(spec, "<="):
		return comparator(av, spec[2:], func(c int) bool { return c <= 0 })
	case strings.HasPrefix(spec, ">"):
		return comparator(av, spec[1:], func(c int) bool { return c > 0 })
	case strings.HasPrefix(spec, "<"):
		return comparator(av, spec[1:], func(c int) bool { return c < 0 })
	case strings.HasPrefix(spec, "="):
		return comparator(av, spec[1:], func(c int) bool { return c == 0 })
	}

	// Anything with an x placeholder or a hyphen range is outside the
	// documented table. Rejected explicitly rather than mis-parsed: §4 says
	// the escape hatch's job, not the parser's.
	if strings.ContainsAny(spec, "xX*") {
		return false, fmt.Errorf("x-ranges are not supported: %q "+
			"(see MANIFEST_SCHEMA.md §4; use a comparator or exit_code)", spec)
	}
	return prefixRange(av, spec)
}

// prefixRange implements the bare form: "20" accepts any 20.x.y, "20.11"
// accepts any 20.11.x, "20.11.0" is exact.
//
// This is the form most .nvmrc files use, so it is the one worth getting
// right. Note that it is NOT the same as parsing "20" as 20.0.0 and comparing
// for equality -- that reading would fail against an actual of 20.11.0, which
// is exactly what a .nvmrc saying "20" means to accept.
func prefixRange(av Version, spec string) (bool, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(spec), "v")
	parts := strings.Split(trimmed, ".")

	bv, err := parseVersion(spec)
	if err != nil {
		return false, err
	}

	// A prerelease in the range means an exact comparison; there is no
	// sensible prefix reading of "20.0.0-rc1".
	if bv.Prerelease != "" {
		return av.compare(bv) == 0, nil
	}

	// An actual that is a prerelease never satisfies a plain prefix range.
	// 20.0.0-rc1 is not "a 20", because it precedes 20.0.0.
	if av.Prerelease != "" {
		return false, nil
	}

	switch len(parts) {
	case 1:
		return av.Major == bv.Major, nil
	case 2:
		return av.Major == bv.Major && av.Minor == bv.Minor, nil
	default:
		return av.compare(bv) == 0, nil
	}
}

func comparator(av Version, spec string, ok func(int) bool) (bool, error) {
	bv, err := parseVersion(spec)
	if err != nil {
		return false, err
	}
	return ok(av.compare(bv)), nil
}

// caretRange: compatible-within-major, with the npm rule that below 1.0.0 the
// minor acts as the major, since 0.x releases break compatibility freely.
func caretRange(av Version, spec string) (bool, error) {
	bv, err := parseVersion(spec)
	if err != nil {
		return false, err
	}
	if av.compare(bv) < 0 {
		return false, nil
	}
	switch {
	case bv.Major > 0:
		return av.Major == bv.Major, nil
	case bv.Minor > 0:
		return av.Major == 0 && av.Minor == bv.Minor, nil
	default:
		return av.Major == 0 && av.Minor == 0 && av.Patch == bv.Patch, nil
	}
}

// tildeRange: patch-level changes only.
func tildeRange(av Version, spec string) (bool, error) {
	bv, err := parseVersion(spec)
	if err != nil {
		return false, err
	}
	if av.compare(bv) < 0 {
		return false, nil
	}
	return av.Major == bv.Major && av.Minor == bv.Minor, nil
}
