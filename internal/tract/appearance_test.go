package tract

import "testing"

func TestParseAppearance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		want   Appearance
		wantOK bool
	}{
		{input: "", want: AppearNone, wantOK: true}, // resolves to "-"
		{input: "-", want: AppearNone, wantOK: true},
		{input: "n", want: AppearNone, wantOK: true},
		{input: "ne", want: AppearNone, wantOK: true}, // neither
		{input: "none", want: AppearNone, wantOK: true},
		{input: "ignore", want: AppearNone, wantOK: true},
		{input: "ig", want: AppearNone, wantOK: true},
		{input: "i", want: AppearBody, wantOK: true},
		{input: "in", want: AppearBody, wantOK: true},
		{input: "inp", want: AppearBody, wantOK: true},
		{input: "input", want: AppearBody, wantOK: true},
		{input: "a", want: AppearBody, wantOK: true},
		{input: "an", want: AppearBody, wantOK: true}, // ante
		{input: "antecedent", want: AppearBody, wantOK: true},
		{input: "b", want: AppearBody, wantOK: true},
		{input: "bo", want: AppearBody, wantOK: true}, // body, not both
		{input: "body", want: AppearBody, wantOK: true},
		{input: "o", want: AppearHead, wantOK: true},
		{input: "ou", want: AppearHead, wantOK: true},
		{input: "output", want: AppearHead, wantOK: true},
		{input: "c", want: AppearHead, wantOK: true},
		{input: "co", want: AppearHead, wantOK: true}, // cons
		{input: "consequent", want: AppearHead, wantOK: true},
		{input: "h", want: AppearHead, wantOK: true},
		{input: "he", want: AppearHead, wantOK: true},
		{input: "head", want: AppearHead, wantOK: true},
		{input: "io", want: AppearBoth, wantOK: true},
		{input: "i&o", want: AppearBoth, wantOK: true},
		{input: "in&", want: AppearBoth, wantOK: true}, // in&out
		{input: "ino", want: AppearBoth, wantOK: true}, // inout
		{input: "ac", want: AppearBoth, wantOK: true},
		{input: "a&", want: AppearBoth, wantOK: true}, // a&c
		{input: "c&a", want: AppearBoth, wantOK: true},
		{input: "ca", want: AppearBoth, wantOK: true}, // canda
		{input: "bh", want: AppearBoth, wantOK: true},
		{input: "b&h", want: AppearBoth, wantOK: true},
		{input: "h&b", want: AppearBoth, wantOK: true},
		{input: "bot", want: AppearBoth, wantOK: true},
		{input: "both", want: AppearBoth, wantOK: true},
		{input: "x", want: AppearBoth, wantOK: true},
		{input: "y", want: AppearNone, wantOK: false},
		{input: "q", want: AppearNone, wantOK: false},
		{input: "bodyx", want: AppearNone, wantOK: false},
		{input: "BODY", want: AppearNone, wantOK: false},
		{input: "headx", want: AppearNone, wantOK: false},
	}
	for _, tt := range tests {
		t.Run("indicator "+tt.input, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseAppearance(tt.input)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ParseAppearance(%q) = %d, %v, want %d, %v", tt.input, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
