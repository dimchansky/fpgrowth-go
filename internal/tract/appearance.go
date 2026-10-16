package tract

import (
	"slices"
	"sort"
	"strings"
)

// Appearance tells where an item may appear in an association rule.
// The values are upstream's APP_* codes (tract.h); AppearNone also marks
// items that are ignored altogether.
type Appearance uint8

// Appearance codes. AppearBoth == AppearBody|AppearHead.
const (
	AppearNone Appearance = 0 // APP_NONE: item is ignored
	AppearBody Appearance = 1 // APP_BODY: item may appear in rule bodies
	AppearHead Appearance = 2 // APP_HEAD: item may appear in rule heads
	AppearBoth Appearance = 3 // APP_BOTH: item may appear in both
)

// appearanceNames is upstream's appmap table (tract.c): indicator name and
// its code. ParseAppearance searches it in byte-wise sorted order.
var appearanceNames = []struct {
	name string
	app  Appearance
}{
	{"-", AppearNone}, {"n", AppearNone}, {"none", AppearNone},
	{"neither", AppearNone}, {"ignore", AppearNone},
	{"i", AppearBody}, {"in", AppearBody}, {"inp", AppearBody},
	{"input", AppearBody},
	{"a", AppearBody}, {"ante", AppearBody}, {"antecedent", AppearBody},
	{"b", AppearBody}, {"body", AppearBody},
	{"o", AppearHead}, {"out", AppearHead}, {"output", AppearHead},
	{"c", AppearHead}, {"cons", AppearHead}, {"consequent", AppearHead},
	{"h", AppearHead}, {"head", AppearHead},
	{"io", AppearBoth}, {"i&o", AppearBoth}, {"inout", AppearBoth},
	{"in&out", AppearBoth},
	{"ac", AppearBoth}, {"a&c", AppearBoth}, {"c&a", AppearBoth},
	{"canda", AppearBoth},
	{"bh", AppearBoth}, {"b&h", AppearBoth}, {"h&b", AppearBoth},
	{"both", AppearBoth}, {"x", AppearBoth},
}

// sortedAppearanceNames holds the indicator names in byte-wise ascending
// order. Upstream sorts appmap with ptr_qsort; the names are distinct, so
// every correct sort yields this order.
var sortedAppearanceNames = func() []string {
	names := make([]string, len(appearanceNames))
	for i, e := range appearanceNames {
		names[i] = e.name
	}
	slices.Sort(names)
	return names
}()

// ParseAppearance translates an appearance indicator as written in an
// item appearance file (-R with target r) into its code. Port of appcode
// (tract.c): the indicator may be an abbreviation; it resolves to the
// byte-wise smallest indicator name that is not less than s, provided s is
// a prefix of that name. So "bo" means "body", "he" means "head" and the
// empty string means "-" (AppearNone). ok is false for unknown indicators.
func ParseAppearance(s string) (app Appearance, ok bool) {
	// ptr_bisect returns the index of an exact match or the insertion
	// position; for distinct names both equal sort.SearchStrings.
	i := sort.SearchStrings(sortedAppearanceNames, s)
	if i >= len(sortedAppearanceNames) {
		return AppearNone, false
	}
	name := sortedAppearanceNames[i]
	if !strings.HasPrefix(name, s) {
		return AppearNone, false
	}
	for _, e := range appearanceNames {
		if e.name == name {
			return e.app, true
		}
	}
	panic("tract: appearance table is inconsistent")
}

// isValid reports whether a is one of the four appearance codes.
func (a Appearance) isValid() bool { return a <= AppearBoth }
