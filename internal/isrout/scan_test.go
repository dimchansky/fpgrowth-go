package isrout

import "testing"

func TestScanable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty name stays empty", "", ""},
		{"letters and digits", "abc123", "abc123"},
		{"unquoted punctuation", "a_b+c-1.5", "a_b+c-1.5"},
		{"space", "a b", `"a b"`},
		{"apostrophe", "it'em", `"it'em"`},
		{"dollar", "x$y", `"x$y"`},
		{"double quote", `a"b`, `"a\"b"`},
		{"backslash", `c\d`, `"c\\d"`},
		{"ansi escapes", "\a\b\t\n\v\f\r", `"\a\b\t\n\v\f\r"`},
		{"control characters", "\x01\x0e\x1f", `"\x01\x0e\x1f"`},
		{"delete", "e\x7ff", `"e\x7ff"`},
		{"utf-8 copied", "café", `"café"`},
		{"brackets and operators", "[a]{b}<c>=d", `"[a]{b}<c>=d"`},
		{"comma", "a,b", `"a,b"`},
		{"high byte", "\xff", "\"\xff\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanable(tt.input); got != tt.expected {
				t.Errorf("scanable(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestScanClassTable spot-checks the transcription of upstream's scftab.
func TestScanClassTable(t *testing.T) {
	var counts [4]int // classes 0, 1, 2, escape
	for c := range 256 {
		switch cls := scanClass[c]; cls {
		case 0, 1, 2:
			counts[cls]++
		default:
			counts[3]++
		}
	}
	// 0: '+' '-' '.' digits, letters, '_' (66); escapes: \a \b \t \n \v
	// \f \r \" \\ (9); 2: the other 25 control characters and DEL (26).
	expected := [4]int{66, 256 - 66 - 26 - 9, 26, 9}
	if counts != expected {
		t.Errorf("class counts %v, want %v", counts, expected)
	}
}
