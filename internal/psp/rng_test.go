package psp

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestRNG_matchesC compares the generator with the sequences printed by
// reference/harness/fpgpsp/rngdump (testdata/rng.c.out): rng_uint and
// rng_dbl of rng_create(seed), and urand and, after reseeding, drand
// after rseed(seed), bit for bit.
func TestRNG_matchesC(t *testing.T) {
	f, err := os.Open("testdata/rng.c.out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var a, b *RNG // the uint and the double generator
	var seed uint32
	mode, n, lines, checked := "", 0, 0, 0
	for sc.Scan() {
		line := sc.Text()
		lines++
		if s, ok := strings.CutPrefix(line, "rng "); ok {
			seed = parseU32(t, s)
			a, b, mode, n = NewRNG(seed), NewRNG(seed), "rng", 0
			continue
		}
		if s, ok := strings.CutPrefix(line, "rseed "); ok {
			seed = parseU32(t, s)
			a, mode, n = NewRNG(seed), "rseed", 0
			continue
		}
		switch mode {
		case "rng":
			u, d, _ := strings.Cut(line, " ")
			if got := a.Uint32(); got != parseU32(t, u) {
				t.Fatalf("line %d: rng_uint = %s, Go %d", lines, u, got)
			}
			if got := b.Float64(); got != parseFloat(t, d) {
				t.Fatalf("line %d: rng_dbl = %s, Go %x", lines, d, got)
			}
		case "rseed":
			if n == 20 { // the drand block starts after reseeding
				a.Seed(seed)
			}
			if n < 20 {
				if got := a.Uint32(); got != parseU32(t, line) {
					t.Fatalf("line %d: urand = %s, Go %d", lines, line, got)
				}
			} else if got := a.Float64(); got != parseFloat(t, line) {
				t.Fatalf("line %d: drand = %s, Go %x", lines, line, got)
			}
			n++
		default:
			t.Fatalf("line %d: unexpected %q", lines, line)
		}
		checked++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if checked < 1000 {
		t.Fatalf("only %d values checked", checked)
	}
}

func parseU32(t *testing.T, s string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(v)
}

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSeedOf(t *testing.T) {
	for _, c := range []struct {
		seed int64
		n    int
		want uint32
	}{
		{7, 0, 7}, {7, 3, 10}, {-1, 0, 4294967295}, {-1, 1, 0},
		{1 << 32, 5, 5}, {4294967295, 1, 0}, {-4294967296, 2, 2},
	} {
		if got := SeedOf(c.seed, c.n); got != c.want {
			t.Errorf("SeedOf(%d, %d) = %d, want %d", c.seed, c.n, got, c.want)
		}
	}
}
