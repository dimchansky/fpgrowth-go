package difftest

import "testing"

// TestNormGTies checks the normalization of Apple's %g ties (F-2).
func TestNormGTies(t *testing.T) {
	for in, want := range map[string]string{
		"1 6 1.67960e+06\n":       "1 6 1.6796e+06\n",
		"2 3 2.50000e+06\n":       "2 3 2.5e+06\n",
		"1 2 1.67962e+06\n":       "1 2 1.67962e+06\n",
		"1 2 1.6796e+06\n":        "1 2 1.6796e+06\n",
		"1 2 100\n3 4 0.0012\n":   "1 2 100\n3 4 0.0012\n",
		"1 10 3.00000e-07 x":      "1 10 3e-07 x",
		"5 1000000 1e+06\n":       "5 1000000 1e+06\n",
		"7 20 4.10000e+10\n8 1 2": "7 20 4.1e+10\n8 1 2",
	} {
		if got := normGTies(in); got != want {
			t.Errorf("normGTies(%q) = %q, want %q", in, got, want)
		}
	}
}
