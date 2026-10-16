package tabread

import "testing"

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	want := Config{RecordSeps: "\n", FieldSeps: " \t,", Blanks: " \t\r", Comments: "#"}
	if got := DefaultConfig(); got != want {
		t.Errorf("DefaultConfig() = %+v, want %+v", got, want)
	}
}

func TestNewClassTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cfg      Config
		expected map[byte]byteClass // bytes not listed have no class
	}{
		{
			name: "upstream defaults (trd_create)",
			cfg:  DefaultConfig(),
			expected: map[byte]byteClass{
				'\n': classRecSep,
				' ':  classFldSep | classBlank,
				'\t': classFldSep | classBlank,
				',':  classFldSep,
				'\r': classBlank,
				'#':  classComment,
			},
		},
		{
			name:     "zero config has no special bytes",
			cfg:      Config{},
			expected: map[byte]byteClass{},
		},
		{
			name: "overlapping classes",
			cfg:  Config{RecordSeps: ";\n", FieldSeps: ";", Blanks: "\n", Comments: ";"},
			expected: map[byte]byteClass{
				';':  classRecSep | classFldSep | classComment,
				'\n': classRecSep | classBlank,
			},
		},
		{
			// Deviation from upstream (B4): high bytes become members.
			name: "high bytes and nul",
			cfg:  Config{FieldSeps: "\xc3\xa9\x00", Blanks: "\xff"},
			expected: map[byte]byteClass{
				0xc3: classFldSep,
				0xa9: classFldSep,
				0x00: classFldSep,
				0xff: classBlank,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := newClassTable(tt.cfg)
			for c := range 256 {
				if got, want := table[c], tt.expected[byte(c)]; got != want {
					t.Errorf("class of byte %#02x = %#b, want %#b", c, got, want)
				}
			}
		})
	}
}
