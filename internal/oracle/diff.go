package oracle

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// SortSets sorts sets canonically: by size, then lexicographically by item
// (ItemSet.Compare). Sets with equal items keep their relative order.
func SortSets(sets []Set) {
	slices.SortStableFunc(sets, func(a, b Set) int { return a.Items.Compare(b.Items) })
}

// SortRules sorts rules canonically: by the item set Body ∪ {Head}
// (ItemSet.Compare), then by head. Rules with equal keys keep their
// relative order.
func SortRules(rules []Rule) {
	slices.SortStableFunc(rules, func(a, b Rule) int {
		if c := a.Items().Compare(b.Items()); c != 0 {
			return c
		}
		return cmp.Compare(a.Head, b.Head)
	})
}

// RuleKey returns a canonical, injective key of the rule's head and body.
func RuleKey(head string, body ItemSet) string {
	return strconv.Quote(head) + " <- " + body.Key()
}

// DiffOptions configures DiffSets and DiffRules.
type DiffOptions struct {
	// MaxDiffs is the maximum number of differences listed; <= 0 means 20.
	MaxDiffs int
	// EvalTol is the tolerance for Eval values: a and b match if
	// |a-b| <= EvalTol*max(1, |a|, |b|). With 0 (the default) the values
	// must be equal. NaN matches NaN in either case; equal infinities match.
	// A wanted set or rule with a non-zero EvalTol (a reference value)
	// also matches within that absolute tolerance.
	EvalTol float64
	// IgnoreEval skips the comparison of Eval values.
	IgnoreEval bool
}

// DiffSets compares two item set results irrespective of order and
// returns "" if they are equal (same sets, supports and, unless ignored,
// evaluation values, and no set reported twice). A wanted set marked
// Optional may be missing from got. Otherwise it returns a
// readable report: a summary line and the first differences in canonical
// order. Use it in test failures, e.g. t.Error(DiffSets(want, got, opts)).
func DiffSets(want, got []Set, opts DiffOptions) string {
	return diff(want, got, opts, "item sets",
		func(s Set) string { return s.Items.Key() },
		func(s Set) string { return s.String() },
		func(w, g Set) string {
			var parts []string
			if w.Supp != g.Supp {
				parts = append(parts, fmt.Sprintf("supp want %d, got %d", w.Supp, g.Supp))
			}
			if !opts.IgnoreEval && !evalMatches(w.Eval, g.Eval, opts.EvalTol, w.EvalTol) {
				parts = append(parts, fmt.Sprintf("eval want %v%s, got %v", w.Eval, tolText(w.EvalTol), g.Eval))
			}
			if len(parts) == 0 {
				return ""
			}
			return w.Items.String() + ": " + strings.Join(parts, "; ")
		},
		func(a, b Set) int { return a.Items.Compare(b.Items) },
		func(s Set) bool { return s.Optional })
}

// DiffRules is DiffSets for rules. Rules are identified by head and body;
// Supp, BodySupp, HeadSupp and (unless ignored) Eval are compared. Conf
// and Lift are derived from the supports and not compared. A wanted rule
// marked Optional may be missing from got.
func DiffRules(want, got []Rule, opts DiffOptions) string {
	return diff(want, got, opts, "rules",
		func(r Rule) string { return RuleKey(r.Head, r.Body) },
		func(r Rule) string { return r.String() },
		func(w, g Rule) string {
			var parts []string
			if w.Supp != g.Supp {
				parts = append(parts, fmt.Sprintf("supp want %d, got %d", w.Supp, g.Supp))
			}
			if w.BodySupp != g.BodySupp {
				parts = append(parts, fmt.Sprintf("body want %d, got %d", w.BodySupp, g.BodySupp))
			}
			if w.HeadSupp != g.HeadSupp {
				parts = append(parts, fmt.Sprintf("head want %d, got %d", w.HeadSupp, g.HeadSupp))
			}
			if !opts.IgnoreEval && !evalMatches(w.Eval, g.Eval, opts.EvalTol, w.EvalTol) {
				parts = append(parts, fmt.Sprintf("eval want %v%s, got %v", w.Eval, tolText(w.EvalTol), g.Eval))
			}
			if len(parts) == 0 {
				return ""
			}
			return fmt.Sprintf("%q <- %v: %s", w.Head, w.Body, strings.Join(parts, "; "))
		},
		func(a, b Rule) int {
			if c := a.Items().Compare(b.Items()); c != 0 {
				return c
			}
			return cmp.Compare(a.Head, b.Head)
		},
		func(r Rule) bool { return r.Optional })
}

// diffEntry is one listed difference with its canonical position.
type diffEntry[T any] struct {
	elem T
	text string
}

// diff implements DiffSets and DiffRules.
func diff[T any](
	want, got []T,
	opts DiffOptions,
	what string,
	key func(T) string,
	format func(T) string,
	compare func(w, g T) string,
	order func(a, b T) int,
	optional func(T) bool,
) string {
	wantByKey := make(map[string]T, len(want))
	var entries []diffEntry[T]
	var missing, extra, changed, dups int
	for _, w := range want {
		k := key(w)
		if _, dup := wantByKey[k]; dup {
			dups++
			entries = append(entries, diffEntry[T]{w, "duplicate in want: " + format(w)})
			continue
		}
		wantByKey[k] = w
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		k := key(g)
		if seen[k] {
			dups++
			entries = append(entries, diffEntry[T]{g, "duplicate in got:  " + format(g)})
			continue
		}
		seen[k] = true
		w, ok := wantByKey[k]
		if !ok {
			extra++
			entries = append(entries, diffEntry[T]{g, "unexpected: " + format(g)})
			continue
		}
		if c := compare(w, g); c != "" {
			changed++
			entries = append(entries, diffEntry[T]{w, "changed:    " + c})
		}
	}
	for _, w := range want {
		if !seen[key(w)] {
			seen[key(w)] = true
			if optional(w) {
				continue
			}
			missing++
			entries = append(entries, diffEntry[T]{w, "missing:    " + format(w)})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	slices.SortStableFunc(entries, func(a, b diffEntry[T]) int { return order(a.elem, b.elem) })

	maxDiffs := opts.MaxDiffs
	if maxDiffs <= 0 {
		maxDiffs = 20
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s differ (want %d, got %d): %d missing, %d unexpected, %d changed, %d duplicates",
		what, len(want), len(got), missing, extra, changed, dups)
	for i, e := range entries {
		if i == maxDiffs {
			fmt.Fprintf(&b, "\n  ... and %d more", len(entries)-maxDiffs)
			break
		}
		b.WriteString("\n  ")
		b.WriteString(e.text)
	}
	return b.String()
}

// evalMatches compares evaluation values (see DiffOptions.EvalTol): the
// wanted value a with its absolute tolerance absTol and the value b.
func evalMatches(a, b, tol, absTol float64) bool {
	switch {
	case a == b:
		return true
	case math.IsNaN(a) || math.IsNaN(b):
		return math.IsNaN(a) && math.IsNaN(b)
	case math.IsInf(a, 0) || math.IsInf(b, 0):
		return false
	default:
		d := math.Abs(a - b)
		return d <= tol*max(1, math.Abs(a), math.Abs(b)) || d <= absTol
	}
}

// tolText formats a non-zero tolerance as " ±tol".
func tolText(tol float64) string {
	if tol == 0 {
		return ""
	}
	return fmt.Sprintf(" ±%.3g", tol)
}
