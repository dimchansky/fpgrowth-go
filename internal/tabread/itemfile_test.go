package tabread

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAppearanceString(t *testing.T) {
	t.Parallel()
	for app, want := range map[Appearance]string{
		AppearanceNone: "none", AppearanceBody: "body", AppearanceHead: "head", AppearanceBoth: "both", 7: "Appearance(7)",
	} {
		if got := app.String(); got != want {
			t.Errorf("Appearance(%d).String() = %q, want %q", app, got, want)
		}
	}
}

// TestAppearanceIndicatorTable compares the table with upstream's appmap
// (tract.c), written in upstream's "code:name" notation.
func TestAppearanceIndicatorTable(t *testing.T) {
	t.Parallel()
	appmap := []string{
		"0:-", "0:n", "0:none", "0:neither", "0:ignore",
		"1:i", "1:in", "1:inp", "1:input",
		"1:a", "1:ante", "1:antecedent", "1:b", "1:body",
		"2:o", "2:out", "2:output",
		"2:c", "2:cons", "2:consequent", "2:h", "2:head",
		"3:io", "3:i&o", "3:inout", "3:in&out",
		"3:ac", "3:a&c", "3:c&a", "3:canda",
		"3:bh", "3:b&h", "3:h&b", "3:both", "3:x",
	}
	if len(appearanceIndicators) != len(appmap) {
		t.Fatalf("%d indicators, upstream has %d", len(appearanceIndicators), len(appmap))
	}
	for _, e := range appmap {
		name := e[2:]
		if got, ok := appearanceIndicators[name]; !ok || int(got) != int(e[0]-'0') {
			t.Errorf("indicator %q = %d (%t), want %c", name, got, ok, e[0])
		}
	}
	if !slices.IsSorted(sortedIndicators) {
		t.Error("sortedIndicators is not sorted")
	}
}

func TestParseAppearance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		app   Appearance
		ok    bool
	}{
		{input: "", app: AppearanceNone, ok: true}, // matches "-"
		{input: "-", app: AppearanceNone, ok: true},
		{input: "ignore", app: AppearanceNone, ok: true},
		{input: "ign", app: AppearanceNone, ok: true},
		{input: "ne", app: AppearanceNone, ok: true},
		{input: "in", app: AppearanceBody, ok: true},
		{input: "inpu", app: AppearanceBody, ok: true},
		{input: "bo", app: AppearanceBody, ok: true}, // "body" sorts before "both"
		{input: "both", app: AppearanceBoth, ok: true},
		{input: "an", app: AppearanceBody, ok: true},
		{input: "he", app: AppearanceHead, ok: true},
		{input: "con", app: AppearanceHead, ok: true},
		{input: "can", app: AppearanceBoth, ok: true},
		{input: "i&", app: AppearanceBoth, ok: true},
		{input: "x", app: AppearanceBoth, ok: true},
		{input: "Both", ok: false}, // case-sensitive
		{input: "bodyx", ok: false},
		{input: "y", ok: false},
		{input: "zzz", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			app, ok := parseAppearance([]byte(tt.input))
			if app != tt.app || ok != tt.ok {
				t.Errorf("parseAppearance(%q) = (%v, %t), want (%v, %t)", tt.input, app, ok, tt.app, tt.ok)
			}
		})
	}
}

func TestReadSelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected []string
		errMsg   string
	}{
		{name: "empty", input: "", expected: []string{}},
		{name: "any layout", input: "a c\nd,a\n", expected: []string{"a", "c", "d"}},
		{name: "comments", input: "#x\na\n#y z\nb", expected: []string{"a", "b"}},
		{name: "nul cuts names", input: "a\x00x b\n", expected: []string{"a", "b"}},
		{name: "empty line", input: "a\n\nb\n", errMsg: "sel:2(1): item expected"},
		{name: "double comma", input: "a,,b\n", errMsg: "sel:1(2): item expected"},
		// Deviation from upstream (T3): like "a,\n".
		{name: "trailing separator at end of input", input: "a,", errMsg: "sel:1(2): item expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadSelection(strings.NewReader(tt.input), "sel", DefaultConfig())
			if tt.errMsg != "" {
				if !errors.Is(err, ErrItemExpected) || err.Error() != tt.errMsg {
					t.Errorf("error = %v, want %q", err, tt.errMsg)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.expected) {
				t.Errorf("ReadSelection = %q (%v), want %q", got, err, tt.expected)
			}
		})
	}
}

func TestReadAppearances(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		def      Appearance
		expected []ItemAppearance
		err      error
		errMsg   string
	}{
		{name: "default only", input: "both\n", def: AppearanceBoth, expected: []ItemAppearance{}},
		{
			name:  "items",
			input: "in\na out\nb -\n#c x\nd x",
			def:   AppearanceBody,
			expected: []ItemAppearance{
				{Name: "a", App: AppearanceHead}, {Name: "b", App: AppearanceNone}, {Name: "d", App: AppearanceBoth},
			},
		},
		{name: "empty first record is none", input: "\na in\n", def: AppearanceNone, expected: []ItemAppearance{{Name: "a", App: AppearanceBody}}},
		{name: "empty input", input: "", err: ErrTooManyFields, errMsg: "app:1(1): too many fields/columns"},
		{name: "two default fields", input: "in out\n", err: ErrTooManyFields, errMsg: "app:1(1): too many fields/columns"},
		{name: "unknown default", input: "foo\n", err: ErrUnknownAppearance, errMsg: "app:1(1): unknown appearance indicator 'foo'"},
		{name: "missing indicator", input: "in\na\n", err: ErrAppearanceExpected, errMsg: "app:2(1): appearance indicator expected"},
		{name: "three fields", input: "in\na in out\n", err: ErrTooManyFields, errMsg: "app:2(2): too many fields/columns"},
		{name: "duplicate", input: "in\na in\na out\n", err: ErrDuplicateItem, errMsg: "app:3(1): duplicate item 'a'"},
		{name: "unknown indicator", input: "in\na Both\n", err: ErrUnknownAppearance, errMsg: "app:2(2): unknown appearance indicator 'Both'"},
		{name: "empty record", input: "in\n\na in\n", err: ErrItemExpected, errMsg: "app:2(1): item expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			def, items, err := ReadAppearances(strings.NewReader(tt.input), "app", DefaultConfig())
			if tt.err != nil {
				if !errors.Is(err, tt.err) || err.Error() != tt.errMsg {
					t.Errorf("error = %v, want %q", err, tt.errMsg)
				}
				return
			}
			if err != nil || def != tt.def || !slices.Equal(items, tt.expected) {
				t.Errorf("ReadAppearances = %v %v (%v), want %v %v", def, items, err, tt.def, tt.expected)
			}
		})
	}
}

func TestItemFileReadErrors(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	if _, err := ReadSelection(&errAfterReader{data: "a b", err: errBoom}, "sel", DefaultConfig()); !errors.Is(err, ErrRead) || !errors.Is(err, errBoom) {
		t.Errorf("ReadSelection error = %v", err)
	}
	for _, input := range []string{"", "in\n", "in\na", "in\na b"} {
		_, _, err := ReadAppearances(&errAfterReader{data: input, err: errBoom}, "app", DefaultConfig())
		if !errors.Is(err, ErrRead) || !errors.Is(err, errBoom) {
			t.Errorf("ReadAppearances(%q) error = %v", input, err)
		}
	}
}
