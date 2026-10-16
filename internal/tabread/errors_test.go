package tabread

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestSyntaxErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *SyntaxError
		expected string
	}{
		{
			name:     "item expected",
			err:      &SyntaxError{File: "test1.csv", Record: 5, Field: 3, Err: ErrItemExpected},
			expected: "test1.csv:5(3): item expected",
		},
		{
			name:     "weight expected",
			err:      &SyntaxError{File: "<stdin>", Record: 2, Field: 1, Err: ErrWeightExpected},
			expected: "<stdin>:2(1): transaction weight expected",
		},
		{
			name:     "invalid weight quotes the field",
			err:      &SyntaxError{File: "f", Record: 1, Field: 2, Text: "(3]", Err: ErrInvalidWeight},
			expected: "f:1(2): invalid transaction weight (3]",
		},
		{
			name:     "negative weight",
			err:      &SyntaxError{File: "f", Record: 1, Field: 2, Text: "-2", Err: ErrNegativeWeight},
			expected: "f:1(2): invalid transaction weight -2 (must not be negative)",
		},
		{
			name:     "duplicate item",
			err:      &SyntaxError{File: "app", Record: 3, Field: 1, Text: "a", Err: ErrDuplicateItem},
			expected: "app:3(1): duplicate item 'a'",
		},
		{
			name:     "too many fields",
			err:      &SyntaxError{File: "app", Record: 1, Field: 1, Text: "in", Err: ErrTooManyFields},
			expected: "app:1(1): too many fields/columns",
		},
		{
			name:     "appearance expected",
			err:      &SyntaxError{File: "app", Record: 2, Field: 1, Text: "a", Err: ErrAppearanceExpected},
			expected: "app:2(1): appearance indicator expected",
		},
		{
			name:     "unknown appearance",
			err:      &SyntaxError{File: "app", Record: 2, Field: 2, Text: "Both", Err: ErrUnknownAppearance},
			expected: "app:2(2): unknown appearance indicator 'Both'",
		},
		{
			name:     "no kind",
			err:      &SyntaxError{File: "f", Record: 1, Field: 1},
			expected: "f:1(1): syntax error",
		},
		{
			name: "long field quoted with at most 1024 bytes",
			err: &SyntaxError{
				File: "f", Record: 1, Field: 2, Text: strings.Repeat("y", 1024) + "zz", Err: ErrInvalidWeight,
			},
			expected: "f:1(2): invalid transaction weight " + strings.Repeat("y", 1024),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestErrorKinds(t *testing.T) {
	t.Parallel()
	neg := error(&SyntaxError{File: "f", Record: 1, Field: 1, Err: ErrNegativeWeight})
	if !errors.Is(neg, ErrNegativeWeight) || !errors.Is(neg, ErrInvalidWeight) {
		t.Errorf("a negative weight error must match ErrNegativeWeight and ErrInvalidWeight")
	}
	if errors.Is(neg, ErrWeightExpected) {
		t.Errorf("a negative weight error must not match ErrWeightExpected")
	}
	se, ok := errors.AsType[*SyntaxError](neg)
	if !ok || se.Record != 1 {
		t.Errorf("errors.AsType[*SyntaxError] failed: %v %v", se, ok)
	}

	rerr := readError("data.tab", fs.ErrPermission)
	if !errors.Is(rerr, ErrRead) || !errors.Is(rerr, fs.ErrPermission) {
		t.Errorf("readError must wrap ErrRead and the cause: %v", rerr)
	}
	// Deviation from upstream (B8): the message names the file.
	if want := "read error on file data.tab: permission denied"; rerr.Error() != want {
		t.Errorf("readError message = %q, want %q", rerr.Error(), want)
	}
}
