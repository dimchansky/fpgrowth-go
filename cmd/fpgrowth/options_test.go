package main

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func strOf(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestParseArgs checks the option grammar with the examples of
// docs/upstream/cli-io.md §1.1 (all confirmed with the C reference).
func TestParseArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, o options)
		code  int // expected error code from parsing or validation (0: none)
		msg   string
	}{
		{name: "octal", args: []string{"-m010", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 8 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "hex upper", args: []string{"-m0X3", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 3 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "hex", args: []string{"-m0x2", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 2 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "octal stops at 8", args: []string{"-m08", "x"}, code: exitOption, msg: "unknown option -8"},
		{name: "0x without hex digit", args: []string{"-m0xZ", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 0 || !o.noPerf || !o.stats {
				t.Errorf("zmin %d -x %v -Z %v", o.zmin, o.noPerf, o.stats)
			}
		}},
		{name: "long truncated to int", args: []string{"-m4294967298", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 2 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "LONG_MAX truncated", args: []string{"-m99999999999999999999", "x"}, code: exitSize, msg: "invalid item set size -1"},
		{name: "LONG_MIN truncated", args: []string{"-m-99999999999999999999", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 0 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "leading blank", args: []string{"-m 2", "x"}, check: func(t *testing.T, o options) {
			if o.zmin != 2 {
				t.Errorf("zmin %d", o.zmin)
			}
		}},
		{name: "hex float", args: []string{"-s0x1E", "-S0x1p4", "-c1e1", "x"}, check: func(t *testing.T, o options) {
			if o.smin.v != 30 || o.smax.v != 16 || o.conf != 10 {
				t.Errorf("smin %g smax %g conf %g", o.smin.v, o.smax.v, o.conf)
			}
		}},
		{name: "exponent without digits", args: []string{"-s1e", "x"}, code: exitMeasure, msg: "invalid evaluation measure '\x00'"},
		{name: "infinite support", args: []string{"-sinf", "x"}, code: exitSupport, msg: "invalid minimum support inf"},
		{name: "huge support", args: []string{"-s1e400", "x"}, code: exitSupport, msg: "invalid minimum support inf"},
		{name: "support %g", args: []string{"-s1234567", "x"}, code: exitSupport, msg: "invalid minimum support 1.23457e+06"},
		{name: "support rounds", args: []string{"-s100.0000001", "x"}, code: exitSupport, msg: "invalid minimum support 100"},
		{name: "nan support", args: []string{"-snan", "x"}, code: exitSupport, msg: "invalid minimum support nan"},
		{name: "negative nan", args: []string{"-s-nan", "x"}, code: exitSupport, msg: "invalid minimum support nan"},
		{name: "nan max support", args: []string{"-Snan", "x"}, code: exitSupport, msg: "invalid maximum support nan"},
		{name: "nan border", args: []string{"-F10:nan", "x"}, code: exitSupport, msg: "invalid support border nan"},
		{name: "nan confidence", args: []string{"-cnan", "x"}, code: exitConf, msg: "invalid minimum confidence nan"},
		{name: "nan threshold", args: []string{"-dnan", "x"}, code: exitMeasure, msg: "invalid evaluation threshold nan"},
		{name: "inf values accepted", args: []string{"-s-inf", "-Sinf", "-dinf", "-F-inf", "x"}, check: func(t *testing.T, o options) {
			if !math.IsInf(o.smin.v, -1) || !math.IsInf(o.thresh, 1) || len(o.border) != 1 {
				t.Errorf("%+v", o)
			}
		}},
		{name: "numeric never takes next arg", args: []string{"-s", "20", "in", "out"}, code: exitArgCount, msg: "wrong number of arguments"},
		{name: "numeric empty", args: []string{"-s", "20", "in"}, check: func(t *testing.T, o options) {
			if o.smin.v != 0 || *o.inFile != "20" || *o.outFile != "in" {
				t.Errorf("smin %g in %v out %v", o.smin.v, strOf(o.inFile), strOf(o.outFile))
			}
		}},
		{name: "cluster", args: []string{"-s10m2n5", "x"}, check: func(t *testing.T, o options) {
			if o.smin.v != 10 || o.zmin != 2 || o.zmax != 5 {
				t.Errorf("%+v", o)
			}
		}},
		{name: "flags", args: []string{"-xjZuoigwN", "x"}, check: func(t *testing.T, o options) {
			if !o.noPerf || !o.noReord || !o.stats || !o.noTail || !o.orig || !o.invBxs || !o.scan || !o.weight {
				t.Errorf("%+v", o)
			}
		}},
		{name: "char options take any byte", args: []string{"-tme-", "x"}, code: exitMeasure, msg: "invalid evaluation measure '-'"},
		{name: "target alone", args: []string{"-t", "x"}, check: func(t *testing.T, o options) {
			if o.target != 's' {
				t.Errorf("target %c", o.target)
			}
		}},
		{name: "target value is a letter", args: []string{"-tcZ", "x"}, check: func(t *testing.T, o options) {
			if o.target != 'c' || !o.stats {
				t.Errorf("%+v", o)
			}
		}},
		{name: "missing measure", args: []string{"-e", "x"}, code: exitMeasure, msg: "invalid evaluation measure '\x00'"},
		{name: "missing aggregation", args: []string{"-a", "x"}, code: exitAggMode, msg: "invalid aggregation mode '\x00'"},
		{name: "missing variant", args: []string{"-A", "x"}, code: exitVariant, msg: "invalid fpgrowth variant '\x00'"},
		{name: "string next argv", args: []string{"-k", "-", "x"}, check: func(t *testing.T, o options) {
			if o.sep != "-" || *o.inFile != "x" {
				t.Errorf("sep %q", o.sep)
			}
		}},
		{name: "string attached", args: []string{"-k::", "-I=>", "-h#", "x"}, check: func(t *testing.T, o options) {
			if o.sep != "::" || o.imp != "=>" || o.hdr != "#" {
				t.Errorf("%+v", o)
			}
		}},
		{name: "empty info", args: []string{"-v", "", "x"}, check: func(t *testing.T, o options) {
			if o.info != "" || !o.infoSet {
				t.Errorf("info %q set %v", o.info, o.infoSet)
			}
		}},
		{name: "string after flags", args: []string{"-xk,", "x"}, check: func(t *testing.T, o options) {
			if !o.noPerf || o.sep != "," {
				t.Errorf("%+v", o)
			}
		}},
		{name: "dash file names", args: []string{"-", "-"}, check: func(t *testing.T, o options) {
			if *o.inFile != "" || *o.outFile != "" {
				t.Errorf("in %q out %q", *o.inFile, *o.outFile)
			}
		}},
		{name: "empty file names", args: []string{"", ""}, check: func(t *testing.T, o options) {
			if *o.inFile != "" || *o.outFile != "" {
				t.Errorf("in %q out %q", *o.inFile, *o.outFile)
			}
		}},
		{name: "R dash is a file", args: []string{"-R-", "x"}, check: func(t *testing.T, o options) {
			if *o.selFile != "-" {
				t.Errorf("sel %q", *o.selFile)
			}
		}},
		{name: "R next dash is a file", args: []string{"-R", "-", "-"}, check: func(t *testing.T, o options) {
			if *o.selFile != "-" || *o.inFile != "" {
				t.Errorf("sel %q", *o.selFile)
			}
		}},
		{name: "stdin twice", args: []string{"-R", "", "-"}, code: exitStdin, msg: "double assignment of standard input"},
		{name: "P empty is stdout", args: []string{"-P", "", "x"}, check: func(t *testing.T, o options) {
			if o.pspFile == nil || *o.pspFile != "" {
				t.Errorf("psp %v", strOf(o.pspFile))
			}
		}},
		{name: "classes", args: []string{"-r%", "-f", `\n`, "-b,", "-C", "", "x"}, check: func(t *testing.T, o options) {
			if *o.recSeps != "%" || *o.fldSeps != `\n` || *o.blanks != "," || *o.comment != "" {
				t.Errorf("%+v", o)
			}
		}},
		{name: "border", args: []string{"-F60:50", "x"}, check: func(t *testing.T, o options) {
			if !slices.Equal(o.border, []suppArg{{v: 60}, {v: 50}}) {
				t.Errorf("border %v", o.border)
			}
		}},
		{name: "border continues", args: []string{"-F60:50Z", "x"}, check: func(t *testing.T, o options) {
			if !slices.Equal(o.border, []suppArg{{v: 60}, {v: 50}}) || !o.stats {
				t.Errorf("border %v Z %v", o.border, o.stats)
			}
		}},
		{name: "border empty value B16", args: []string{"-F60:", "x"}, check: func(t *testing.T, o options) {
			if !slices.Equal(o.border, []suppArg{{v: 60}}) {
				t.Errorf("border %v", o.border)
			}
		}},
		{name: "border without value B16", args: []string{"-F", "x"}, check: func(t *testing.T, o options) {
			if o.border == nil || len(o.border) != 0 {
				t.Errorf("border %v", o.border)
			}
		}},
		{name: "border leading colon", args: []string{"-F:5", "x"}, code: exitOption, msg: "unknown option -:"},
		{name: "border garbage", args: []string{"-Fabc", "x"}, code: exitAggMode, msg: "invalid aggregation mode 'b'"},
		{name: "last wins", args: []string{"-s50", "-s20", "-tc", "-tr", "x"}, check: func(t *testing.T, o options) {
			if o.smin.v != 20 || o.target != 'r' {
				t.Errorf("%+v", o)
			}
		}},
		{name: "unknown option", args: []string{"-z", "x"}, code: exitOption, msg: "unknown option -z"},
		{name: "double dash", args: []string{"--", "x"}, code: exitOption, msg: "unknown option --"},
		{name: "fraction", args: []string{"-m2.5", "x"}, code: exitOption, msg: "unknown option -."},
		{name: "missing argument", args: []string{"x", "-k"}, code: exitOptArg, msg: "missing option argument"},
		{name: "no input", args: []string{"-s10"}, code: exitArgCount, msg: "wrong number of arguments"},
		{name: "three files", args: []string{"a", "b", "c"}, code: exitArgCount, msg: "wrong number of arguments"},
		{name: "argument count before sizes", args: []string{"-m-1", "-q"}, code: exitArgCount, msg: "wrong number of arguments"},
		{name: "validation order", args: []string{"-m-1", "-n-2", "-s200", "-c200", "-tq", "x"}, code: exitSize, msg: "invalid item set size -1"},
		{name: "zmax", args: []string{"-n-2", "-s200", "x"}, code: exitSize, msg: "invalid item set size -2"},
		{name: "support before conf", args: []string{"-s200", "-c200", "x"}, code: exitSupport, msg: "invalid minimum support 200"},
		{name: "conf before target", args: []string{"-c-1", "-tq", "x"}, code: exitConf, msg: "invalid minimum confidence -1"},
		{name: "stdin before target", args: []string{"-R", "", "-tq", "-"}, code: exitStdin, msg: "double assignment of standard input"},
		{name: "target before measure", args: []string{"-tq", "-eB", "x"}, code: exitTarget, msg: "invalid target type 'q'"},
		{name: "measure before agg", args: []string{"-eB", "-aq", "x"}, code: exitMeasure, msg: "invalid evaluation measure 'B'"},
		{name: "agg before variant", args: []string{"-aq", "-Aq", "x"}, code: exitAggMode, msg: "invalid aggregation mode 'q'"},
		{name: "variant", args: []string{"-Aq", "x"}, code: exitVariant, msg: "invalid fpgrowth variant 'q'"},
		{name: "non-ASCII letter", args: []string{"-t\xc3", "x"}, code: exitTarget, msg: "invalid target type '\xc3'"},
		{name: "unvalidated", args: []string{"-n0", "-S0", "-p5", "-q9", "-l99", "-d-5", "x"}, check: func(t *testing.T, o options) {
			if o.zmax != 0 || o.smax.v != 0 || o.prune != 5 || o.sort != 9 || o.pack != 99 || o.thresh != -5 {
				t.Errorf("%+v", o)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o, act, err := parseArgs(tt.args)
			if act != actRun {
				t.Fatalf("action %d", act)
			}
			if err == nil {
				err = o.validate()
			}
			switch {
			case tt.code == 0 && err != nil:
				t.Fatalf("error %d %q", err.code, err.msg)
			case tt.code != 0 && err == nil:
				t.Fatalf("no error, want %d %q", tt.code, tt.msg)
			case tt.code != 0:
				if err.code != tt.code || err.msg != tt.msg || err.Error() != tt.msg {
					t.Fatalf("error %d %q, want %d %q", err.code, err.msg, tt.code, tt.msg)
				}
				return
			}
			if tt.check != nil {
				tt.check(t, o)
			}
		})
	}
}

// TestParseArgsActions checks the usage and help actions.
func TestParseArgsActions(t *testing.T) {
	t.Parallel()
	if _, act, err := parseArgs(nil); act != actUsage || err != nil {
		t.Errorf("no arguments: %d %v", act, err)
	}
	// -! is processed when reached: earlier errors win, later ones are
	// never seen
	if _, act, err := parseArgs([]string{"-s10", "-!", "-tq", "-z"}); act != actHelp || err != nil {
		t.Errorf("-!: %d %v", act, err)
	}
	if _, _, err := parseArgs([]string{"-z", "-!"}); err == nil || err.code != exitOption {
		t.Errorf("-z -!: %v", err)
	}
}

// TestMinerConfig checks the translation into the miner configuration.
func TestMinerConfig(t *testing.T) {
	t.Parallel()
	o, _, err := parseArgs([]string{"-tm", "-s-3", "-S70", "-c40", "-m2", "-n4", "-ec", "-i",
		"-am", "-d30", "-Ad", "-xju", "-o", "-l-5", "-q-2", "-p-1", "x"})
	if err != nil {
		t.Fatal(err)
	}
	c := o.minerConfig()
	want := fpg.Config{
		Target: report.TargetMaximal, SMin: -3, SMax: 70, HasMinCount: true, MinCount: 3, ConfFrac: 0.4, ZMin: 2, ZMax: 4,
		Eval: int(ruleval.Confidence), InvalidateBelowExpected: true, Agg: fpg.AggMin, Thresh: 30,
		Algo: fpg.AlgoSingle, Mode: fpg.ModeOrigSupp, Pack: 0, Sort: -2, Prune: -1,
	}
	if c != want {
		t.Errorf("config\n got %+v\nwant %+v", c, want)
	}
	for _, tc := range []struct {
		args []string
		pack int
		eval int
		tg   report.Target
	}{
		{[]string{"x"}, 16, fpg.EvalNone, report.TargetAll},
		{[]string{"-l99", "-eb", "-tf", "x"}, 16, fpg.EvalLogRatio, report.TargetAll},
		{[]string{"-l3", "-ex", "-tg", "x"}, 3, fpg.EvalNone, report.TargetGenerators},
		{[]string{"-l", "-tr", "-eb", "x"}, 0, fpg.EvalLogRatio, report.TargetRules},
		{[]string{"-tc", "x"}, 16, fpg.EvalNone, report.TargetClosed},
	} {
		o, _, err := parseArgs(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		if c := o.minerConfig(); c.Pack != tc.pack || c.Eval != tc.eval || c.Target != tc.tg {
			t.Errorf("%q: pack %d eval %d target %d", tc.args, c.Pack, c.Eval, c.Target)
		}
	}
	if _, ok := targetOf('q'); ok {
		t.Error("target q")
	}
}

func TestFormatG(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		x    float64
		want string
	}{
		{10, "10"}, {100.0000001, "100"}, {1234567, "1.23457e+06"}, {0.5, "0.5"},
		{1e-5, "1e-05"}, {-1, "-1"}, {math.Inf(1), "inf"}, {math.Inf(-1), "-inf"},
		{math.NaN(), "nan"}, {math.Copysign(0, -1), "-0"},
	} {
		if got := formatG(tc.x); got != tc.want {
			t.Errorf("formatG(%v) = %q, want %q", tc.x, got, tc.want)
		}
	}
}

// nanFree returns o with its NaN values replaced by 0 and a mask of
// where they were, for reflect.DeepEqual (NaN != NaN).
func nanFree(o options) any {
	var mask []byte
	fix := func(x *float64) {
		if math.IsNaN(*x) {
			*x, mask = 0, append(mask, 'n')
		} else {
			mask = append(mask, '.')
		}
	}
	fix(&o.smin.v)
	fix(&o.smax.v)
	fix(&o.conf)
	fix(&o.thresh)
	o.border = slices.Clone(o.border)
	for i := range o.border {
		fix(&o.border[i].v)
	}
	return struct {
		o    options
		mask string
	}{o, string(mask)}
}

// FuzzParseArgs feeds arbitrary command lines (arguments separated by
// NUL bytes) to parseArgs, validate and minerConfig. None may panic, and
// the results must be consistent: parsing is deterministic, errors carry
// the exit codes and messages of their stage, a valid command line names
// an input file, and every option set that passes validate gives a miner
// configuration that fpg.New accepts (run treats a failure as excluded
// by validate). Support literals (issue #18) keep strtod's value and end
// position at every offset of every argument, exactly the negative
// decimal integer literals are exact (checked with strconv.ParseInt), and
// the configuration takes the exact counts.
//
//	go test ./cmd/fpgrowth/ -run '^$' -fuzz FuzzParseArgs -fuzztime 60s
func FuzzParseArgs(f *testing.F) {
	for _, s := range []string{
		"",
		"in",
		"-ts\x00-s-1\x00-eb\x00-d0\x00-v %a %20e\x00in\x00-",
		"-tr\x00-c0\x00-s0\x00-ox\x00-ec\x00-a\x00-d-1e308\x00in\x00out",
		"-s10\x00-!\x00-tq",
		"-z\x00-!",
		"-F60:\x00-F:5\x00-F\x00-Fnan\x00x",
		"-snan\x00-Snan\x00-cnan\x00-dnan\x00x",
		"-m-1\x00-n0x10\x00-p0x7fffffffffff\x00-q-2\x00-l99\x00x",
		"-Az\x00x", "-t\x00x", "-e\x00-a\x00-A\x00x",
		"-R\x00-\x00-\x00", "-Rsel\x00-Ppsp\x00-k,\x00-I =>\x00-h#\x00-r\n\x00-f\t\x00-b \x00-C#\x00in\x00out",
		"-v", "-tg -xjuZgwN in", "a\x00b\x00c",
		"-s-9007199254740993m2\x00-S-9223372036854775807\x00-F-4611686018427387840:50:-2.5\x00x",
		"-s-9223372036854775808\x00x", "-S-99999999999999999999Z\x00x", "-F-1:-9223372036854775808\x00x",
		"-s -0017e\x00-S-0x11\x00-F-1e19:-0\x00x", "-s-17.0\x00-S-17.\x00-s--17\x00x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var args []string
		if s != "" {
			args = strings.Split(s, "\x00")
		}
		for _, arg := range args {
			for i := range len(arg) + 1 {
				checkSuppValue(t, arg[i:])
			}
		}
		o, act, err := parseArgs(args)
		o2, act2, err2 := parseArgs(args)
		if !reflect.DeepEqual(nanFree(o), nanFree(o2)) || act != act2 || !reflect.DeepEqual(err, err2) {
			t.Fatalf("%q: parseArgs is not deterministic", args)
		}
		if (act == actUsage) != (len(args) == 0) {
			t.Fatalf("%q: action %d", args, act)
		}
		if err != nil {
			if act != actRun || err.msg == "" || !slices.Contains([]int{exitOption, exitOptArg, exitArgCount}, err.code) {
				t.Fatalf("%q: parse error %d %q (action %d)", args, err.code, err.msg, act)
			}
			return
		}
		if act != actRun {
			return
		}
		if o.inFile == nil {
			t.Fatalf("%q: no input file", args)
		}
		if verr := o.validate(); verr != nil {
			if verr.msg == "" || !slices.Contains([]int{exitStdin, exitTarget, exitSize, exitSupport, exitConf,
				exitMeasure, exitAggMode, exitVariant}, verr.code) {
				t.Fatalf("%q: validation error %d %q", args, verr.code, verr.msg)
			}
			return
		}
		cfg := o.minerConfig()
		if cfg.Pack < 0 || cfg.Pack > 16 {
			t.Fatalf("%q: pack %d", args, cfg.Pack)
		}
		if _, err := fpg.New[string](cfg); err != nil {
			t.Fatalf("%q: valid options rejected by fpg.New: %v", args, err)
		}
		if cfg.HasMinCount != o.smin.exact() || cfg.MinCount != tract.Supp(o.smin.count) ||
			cfg.HasMaxCount != o.smax.exact() || cfg.MaxCount != tract.Supp(o.smax.count) {
			t.Fatalf("%q: config counts %+v for %+v, %+v", args, cfg, o.smin, o.smax)
		}
	})
}

// checkSuppValue checks suppValue(s) against strtod and an independent
// integer parse (strconv.ParseInt) of the consumed literal.
func checkSuppValue(t *testing.T, s string) {
	t.Helper()
	a, rest := suppValue(s)
	v, frest := floatValue(s)
	if math.Float64bits(a.v) != math.Float64bits(v) || rest != frest {
		t.Fatalf("suppValue(%q) = %+v, %q; strtod %v, %q", s, a, rest, v, frest)
	}
	lit := strings.TrimLeft(s[:len(s)-len(rest)], " \t\n\v\f\r")
	isInt := len(lit) > 1 && lit[0] == '-' && strings.Trim(lit[1:], "0123456789") == ""
	n, perr := strconv.ParseInt(lit, 10, 64)
	switch {
	case !(v < 0) || !isInt: // not an absolute integer literal (also -0)
		if a.count != 0 || a.bad != "" {
			t.Fatalf("suppValue(%q) = %+v: not an absolute integer literal", s, a)
		}
	case perr != nil: // out of range (MinInt64 is -(MaxInt64+1))
		if a.count != 0 || a.bad != lit {
			t.Fatalf("suppValue(%q) = %+v: want bad %q", s, a, lit)
		}
	case n == math.MinInt64:
		if a.count != 0 || a.bad != lit {
			t.Fatalf("suppValue(%q) = %+v: want bad %q", s, a, lit)
		}
	default:
		if a.count != -n || a.bad != "" || !a.exact() {
			t.Fatalf("suppValue(%q) = %+v: want count %d", s, a, -n)
		}
		// below 2^53 the exact count is upstream's double
		if a.count <= 1<<53 && float64(a.count) != -v {
			t.Fatalf("suppValue(%q) = %+v: count differs from strtod %v", s, a, v)
		}
	}
}
