package main

import (
	"math"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args  []string
		check func(o options) bool
		act   action
		code  int // expected error code, 0 for none
	}{
		{args: nil, act: actUsage},
		{args: []string{"in"}, check: func(o options) bool { return *o.inFile == "in" && o.outFile == nil }},
		{args: []string{"-", "-"}, check: func(o options) bool { return *o.inFile == "" && *o.outFile == "" }},
		{args: []string{"-gr", "-c40", "-S7", "-Z4", "-D", "in", "out"}, check: func(o options) bool {
			return o.surr == 'r' && o.cnt == 40 && o.seed == 7 && o.seedOK && o.cpus == 4 && o.border && *o.outFile == "out"
		}},
		// clusters, numeric prefixes, char options and attached values
		{args: []string{"-tcAdxiu", "-m2n5s-3", "in"}, check: func(o options) bool {
			return o.target == 'c' && o.algo == 'd' && o.noPerf && o.noReo && o.noTail && o.zmin == 2 && o.zmax == 5 && o.supp == -3
		}},
		{args: []string{"-e0.25z50l0", "in"}, check: func(o options) bool { return o.alpha == 0.25 && o.smpls == 50 && o.pack == 0 }},
		{args: []string{"-S0x10", "-c010", "in"}, check: func(o options) bool { return o.seed == 16 && o.cnt == 8 }},
		{args: []string{"-S-1", "-Z-2", "in"}, check: func(o options) bool { return o.seed == -1 && o.cpus == -2 }},
		{args: []string{"-S99999999999", "in"}, check: func(o options) bool { return o.seed == 99999999999 }},
		{args: []string{"-m4294967297", "in"}, check: func(o options) bool { return o.zmin == 1 }}, // (int) of a long
		{args: []string{"-Rsel", "-r", ";", "-f,", "-b", " ", "-C%", "in"}, check: func(o options) bool {
			return *o.selFile == "sel" && *o.recSeps == ";" && *o.fldSeps == "," && *o.blanks == " " && *o.comment == "%"
		}},
		{args: []string{"-t", "in"}, check: func(o options) bool { return o.target == 0 }},
		{args: []string{"-g", "in"}, check: func(o options) bool { return o.surr == 0 }},
		{args: []string{"-q", "in"}, code: exitOption},
		{args: []string{"in", "-R"}, code: exitOptArg},
		{args: []string{"-gi"}, code: exitArgCount},
		{args: []string{"a", "b", "c"}, code: exitArgCount},
		{args: []string{"-x!", "in"}, act: actHelp},
	} {
		o, act, err := parseArgs(c.args)
		switch {
		case c.code != 0:
			if err == nil || err.code != c.code {
				t.Errorf("%q: error %v, want code %d", c.args, err, c.code)
			}
		case err != nil:
			t.Errorf("%q: unexpected error %v", c.args, err)
		case act != c.act:
			t.Errorf("%q: action %d, want %d", c.args, act, c.act)
		case c.check != nil && !c.check(o):
			t.Errorf("%q: unexpected options %+v", c.args, o)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"-m-1", "in"}, exitSize, "invalid item set size -1"},
		{[]string{"-s100.5", "in"}, exitSupport, "invalid minimum support 100.5"}, // U14
		{[]string{"-snan", "in"}, exitSupport, "invalid minimum support nan"},
		{[]string{"-c0", "in"}, exitSurrCount, "invalid number of surrogates 0"},
		{[]string{"-t", "in"}, exitTarget, "invalid target type '\x00'"},
		{[]string{"-Ax", "in"}, exitVariant, "invalid fpgrowth variant 'x'"},
		{[]string{"-gq", "in"}, exitSurr, "invalid surrogate data generation method 'q'"},
		{[]string{"-enan", "in"}, exitSurrCount, "invalid probability dispersion factor nan"}, // U15
		{[]string{"-z0", "in"}, exitSurrCount, "invalid number of item set samples 0"},        // U15
		{[]string{"-z-5", "-gr", "in"}, 0, ""},                                                // unused
		{[]string{"-z0", "-e0", "in"}, 0, ""},                                                 // simple estimation
		{[]string{"-enan", "-gp", "in"}, 0, ""},                                               // unused
		{[]string{"-gs", "-s100", "-m0", "-n0", "-c1", "in"}, 0, ""},
	} {
		o, _, err := parseArgs(c.args)
		if err != nil {
			t.Fatalf("%q: %v", c.args, err)
		}
		err = o.validate()
		switch {
		case c.code == 0 && err != nil:
			t.Errorf("%q: unexpected error %v", c.args, err)
		case c.code != 0 && (err == nil || err.code != c.code || err.msg != c.msg):
			t.Errorf("%q: error %v, want %d %q", c.args, err, c.code, c.msg)
		}
	}
}

// TestMinerConfig checks the translation of the options into fpg_create's
// arguments for generation and estimation.
func TestMinerConfig(t *testing.T) {
	o, _, _ := parseArgs([]string{"-tm", "-As", "-x", "-i", "-u", "-l3", "-s-4", "-m2", "-n7", "in"})
	c := o.minerConfig()
	if c.ZMin != 2 || c.ZMax != 7 || c.SMin != -4 || c.SMax != 100 || c.Pack != 3 || c.Mode != 0 {
		t.Errorf("miner config %+v", c)
	}
	e := o.estimConfig()
	if e.Pack != 16 || e.Mode == 0 || e.Algo != 1 || e.ZMin != 2 || e.SMin != -4 {
		t.Errorf("estimation config %+v", e)
	}
}

// FuzzParseArgs feeds arbitrary arguments to the parser and the
// validation: they must not panic, and an accepted command line must
// have an input file and valid enumerated values.
func FuzzParseArgs(f *testing.F) {
	for _, s := range []string{"-gr -c40 -S7 in -", "-tcAdx -m2n5s-3 in", "-R", "-!", "-t", "-e0.5z0 -gs a b c",
		"-S0x7fffffffffffffff -Z-1 in", "-c9223372036854775808 in", "-s1e400 in"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		args := strings.Fields(line)
		o, act, err := parseArgs(args)
		if err != nil || act != actRun {
			return
		}
		if o.inFile == nil {
			t.Fatalf("%q: accepted without an input file", args)
		}
		if err := o.validate(); err != nil {
			return
		}
		if _, ok := targetOf(o.target); !ok {
			t.Fatalf("%q: invalid target accepted", args)
		}
		if _, ok := methodOf(o.surr); !ok || o.cnt <= 0 || o.zmin < 0 || o.zmax < 0 || math.IsNaN(o.supp) {
			t.Fatalf("%q: invalid values accepted: %+v", args, o)
		}
		_ = o.minerConfig()
		_ = o.estimConfig()
	})
}
