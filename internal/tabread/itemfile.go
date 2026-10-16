package tabread

import (
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Appearance tells where an item may appear in an association rule. The
// values mirror upstream's appearance flags (tract.h APP_NONE, APP_BODY,
// APP_HEAD, APP_BOTH): bit 0 allows the rule body, bit 1 the head, so
// the zero value is the meaningful "ignore the item" (no sentinel, to
// keep the values identical to upstream's codes).
type Appearance uint8

// Appearance values.
const (
	AppearanceNone Appearance = 0                               // APP_NONE: item is ignored
	AppearanceBody Appearance = 1                               // APP_BODY: body (antecedent) only
	AppearanceHead Appearance = 2                               // APP_HEAD: head (consequent) only
	AppearanceBoth            = AppearanceBody | AppearanceHead // APP_BOTH: body and head
)

// String returns "none", "body", "head" or "both".
func (a Appearance) String() string {
	switch a {
	case AppearanceNone:
		return "none"
	case AppearanceBody:
		return "body"
	case AppearanceHead:
		return "head"
	case AppearanceBoth:
		return "both"
	}
	return "Appearance(" + strconv.Itoa(int(a)) + ")"
}

// ItemAppearance is one item of an appearance file.
type ItemAppearance struct {
	Name string
	App  Appearance
}

// appearanceIndicators is upstream's indicator table (tract.c appmap).
var appearanceIndicators = map[string]Appearance{
	"-": AppearanceNone, "n": AppearanceNone, "none": AppearanceNone,
	"neither": AppearanceNone, "ignore": AppearanceNone,

	"i": AppearanceBody, "in": AppearanceBody, "inp": AppearanceBody,
	"input": AppearanceBody, "a": AppearanceBody, "ante": AppearanceBody,
	"antecedent": AppearanceBody, "b": AppearanceBody, "body": AppearanceBody,

	"o": AppearanceHead, "out": AppearanceHead, "output": AppearanceHead,
	"c": AppearanceHead, "cons": AppearanceHead, "consequent": AppearanceHead,
	"h": AppearanceHead, "head": AppearanceHead,

	"io": AppearanceBoth, "i&o": AppearanceBoth, "inout": AppearanceBoth,
	"in&out": AppearanceBoth, "ac": AppearanceBoth, "a&c": AppearanceBoth,
	"c&a": AppearanceBoth, "canda": AppearanceBoth, "bh": AppearanceBoth,
	"b&h": AppearanceBoth, "h&b": AppearanceBoth, "both": AppearanceBoth,
	"x": AppearanceBoth,
}

// sortedIndicators holds the indicator names in byte order, as appcode
// sorts appmap with appcmp (an unsigned byte comparison). The names are
// distinct, so the sort algorithm does not matter.
var sortedIndicators = slices.Sorted(maps.Keys(appearanceIndicators))

// parseAppearance maps an appearance indicator to its value. Port of
// appcode: the indicator matches the first name in byte order that is
// greater than or equal to it (ptr_bisect) if it is a prefix of that
// name, so abbreviations resolve to the lexicographically first candidate
// ("bo" is "body", not "both") and the empty string is "-" (none).
// Matching is case-sensitive.
func parseAppearance(s []byte) (Appearance, bool) {
	i, _ := slices.BinarySearch(sortedIndicators, string(s))
	if i == len(sortedIndicators) || !strings.HasPrefix(sortedIndicators[i], string(s)) {
		return 0, false
	}
	return appearanceIndicators[sortedIndicators[i]], true
}

// ReadSelection reads an item selection file (fpgrowth -R for item set
// targets) and returns the selected item names in file order without
// duplicates. Port of ib_readsel: every field of every record is an item
// name (the record structure does not matter), comment records are
// skipped, and an empty field (an empty line, "a,,b", a trailing field
// separator) is an error (ErrItemExpected). An empty input selects no
// items. Names are cut at their first NUL byte. cfg.Weighted is ignored.
//
// Errors are *SyntaxError values for malformed input and errors wrapping
// ErrRead for read failures; name identifies the input in messages.
func ReadSelection(src io.Reader, name string, cfg Config) ([]string, error) {
	t := newTokenizer(src, cfg)
	names := []string{}
	seen := map[string]struct{}{}
	for {
		switch t.read() {
		case delimErr:
			return nil, readError(name, t.err())
		case delimEOF:
			return names, nil
		}
		field := cString(t.field)
		if len(field) == 0 {
			return nil, newSyntaxError(t, name, ErrItemExpected, field)
		}
		if _, ok := seen[string(field)]; ok {
			continue // upstream skips items already in the item base
		}
		s := string(field)
		seen[s] = struct{}{}
		names = append(names, s)
	}
}

// ReadAppearances reads an item appearance file (fpgrowth -R for the rule
// target) and returns the default appearance and the listed items in file
// order. Port of ib_readapp:
//
//   - the first record holds exactly one field, the default appearance of
//     items not listed; an empty first record means "none", a missing
//     first record (empty input) or a second field is ErrTooManyFields;
//   - every further record holds an item name and its indicator; a
//     missing indicator is ErrAppearanceExpected, a third field
//     ErrTooManyFields, an empty record or name ErrItemExpected, a name
//     listed twice ErrDuplicateItem;
//   - indicators (case-sensitive, any unambiguous-by-order prefix, see
//     parseAppearance): none "-", "n", "none", "neither", "ignore"; body
//     "i", "in", "inp", "input", "a", "ante", "antecedent", "b", "body";
//     head "o", "out", "output", "c", "cons", "consequent", "h", "head";
//     both "io", "i&o", "inout", "in&out", "ac", "a&c", "c&a", "canda",
//     "bh", "b&h", "h&b", "both", "x". Others are ErrUnknownAppearance.
//
// Names and indicators are cut at their first NUL byte; cfg.Weighted is
// ignored. Errors are *SyntaxError values for malformed input and errors
// wrapping ErrRead for read failures.
func ReadAppearances(src io.Reader, name string, cfg Config) (def Appearance, items []ItemAppearance, err error) {
	t := newTokenizer(src, cfg)
	switch t.read() {
	case delimErr:
		return 0, nil, readError(name, t.err())
	case delimRec:
	default: // a second field, or no record at all
		return 0, nil, newSyntaxError(t, name, ErrTooManyFields, cString(t.field))
	}
	def, ok := parseAppearance(cString(t.field))
	if !ok {
		return 0, nil, newSyntaxError(t, name, ErrUnknownAppearance, cString(t.field))
	}

	items = []ItemAppearance{}
	seen := map[string]struct{}{}
	for {
		d := t.read()
		switch d {
		case delimErr:
			return 0, nil, readError(name, t.err())
		case delimEOF:
			return def, items, nil
		}
		field := cString(t.field)
		if len(field) == 0 {
			return 0, nil, newSyntaxError(t, name, ErrItemExpected, field)
		}
		if _, ok := seen[string(field)]; ok {
			return 0, nil, newSyntaxError(t, name, ErrDuplicateItem, field)
		}
		if d != delimFld {
			return 0, nil, newSyntaxError(t, name, ErrAppearanceExpected, field)
		}
		item := string(field)
		seen[item] = struct{}{}

		switch t.read() {
		case delimErr:
			return 0, nil, readError(name, t.err())
		case delimFld:
			return 0, nil, newSyntaxError(t, name, ErrTooManyFields, cString(t.field))
		}
		app, ok := parseAppearance(cString(t.field))
		if !ok {
			return 0, nil, newSyntaxError(t, name, ErrUnknownAppearance, cString(t.field))
		}
		items = append(items, ItemAppearance{Name: item, App: app})
	}
}
