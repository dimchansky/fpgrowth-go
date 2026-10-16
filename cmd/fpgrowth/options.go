package main

import (
	"math"
	"strconv"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/cconv"
	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
)

// Exit codes and messages of upstream's main (fpgrowth.c errmsgs[] and
// the tract.c reader codes), see docs/upstream/cli-io.md §3.4.
const (
	exitOK       = 0
	exitNoMem    = 1  // E_NOMEM
	exitOpen     = 2  // E_FOPEN
	exitRead     = 3  // E_FREAD
	exitWrite    = 4  // E_FWRITE
	exitStdin    = 5  // E_STDIN
	exitOption   = 6  // E_OPTION
	exitOptArg   = 7  // E_OPTARG
	exitArgCount = 8  // E_ARGCNT
	exitTarget   = 9  // E_TARGET
	exitSize     = 10 // E_SIZE
	exitSupport  = 11 // E_SUPPORT
	exitConf     = 12 // E_CONF
	exitMeasure  = 13 // E_MEASURE
	exitAggMode  = 14 // E_AGGMODE
	exitNoItems  = 15 // E_NOITEMS
	exitVariant  = 16 // E_VARIANT (shared with E_ITEMEXP)
	exitItemExp  = 16 // E_ITEMEXP
	exitDupItem  = 18 // E_DUPITEM
	exitWgtExp   = 20 // E_WGTEXP
	exitTaWgt    = 21 // E_TAWGT
	exitFldCnt   = 22 // E_FLDCNT
	exitAppExp   = 23 // E_APPEXP
	exitUnkApp   = 24 // E_UNKAPP
	// exitInterrupt is the status of a run stopped by SIGINT (128+2, what
	// a shell reports for upstream, which dies from the signal).
	exitInterrupt = 130
)

// cliError is a fatal error: the message (without upstream's
// "\n<prgname>: " prefix) and the exit status.
type cliError struct {
	code int
	msg  string
}

func (e *cliError) Error() string { return e.msg }

func errorf(code int, msg string) *cliError { return &cliError{code: code, msg: msg} }

// action is what the command line asks for.
type action int

const (
	actRun   action = iota // mine
	actUsage               // no arguments: usage text
	actHelp                // -!: additional option information
)

// options holds the parsed command line, with upstream's defaults
// (fpgrowth.c main). String options are nil when not given; an empty file
// name means standard input or output.
type options struct {
	target  byte // -t letter
	zmin    int32
	zmax    int32
	smin    float64
	smax    float64
	conf    float64
	eval    byte // -e letter (0 if missing)
	agg     byte // -a letter
	thresh  float64
	prune   int32
	sort    int32
	algo    byte // -A letter
	pack    int32
	border  []float64 // -F values (nil: none)
	invBxs  bool      // -i
	orig    bool      // -o
	noPerf  bool      // -x
	noReord bool      // -j
	noTail  bool      // -u
	stats   bool      // -Z
	scan    bool      // -g
	weight  bool      // -w

	inFile, outFile  *string
	selFile, pspFile *string
	recSeps, fldSeps *string
	blanks, comment  *string

	hdr, sep, imp, info string
	infoSet             bool // -v given (upstream compares the pointer)
}

// defaultOptions returns upstream's defaults.
func defaultOptions() options {
	return options{
		target: 's',
		zmin:   1,
		zmax:   math.MaxInt32,
		smin:   10,
		smax:   100,
		conf:   80,
		eval:   'x',
		agg:    'x',
		thresh: 10,
		prune:  math.MinInt32,
		sort:   2,
		algo:   'c',
		pack:   16,
		sep:    " ",
		imp:    " <- ",
		info:   " (%S)",
	}
}

// parseArgs evaluates the arguments (without the program name) exactly
// like upstream's main: option clusters, char-valued, numeric (strtol /
// strtod prefixes of the rest of the cluster) and string options (rest of
// the argument or the next argument verbatim), "-" as an empty file name,
// and errors raised in scan order. It stops at -! (actHelp).
func parseArgs(args []string) (options, action, *cliError) {
	o := defaultOptions()
	if len(args) == 0 {
		return o, actUsage, nil
	}
	var pending *string // string option waiting for its value
	k := 0              // number of non-option arguments
	for _, arg := range args {
		if pending != nil {
			*pending, pending = arg, nil
			continue
		}
		if len(arg) < 2 || arg[0] != '-' {
			name := arg
			if arg == "-" {
				name = "" // upstream skips the '-' before testing for options
			}
			switch k {
			case 0:
				o.inFile = &name
			case 1:
				o.outFile = &name
			default:
				return o, actRun, errorf(exitArgCount, "wrong number of arguments")
			}
			k++
			continue
		}
		s := arg[1:]
		for len(s) > 0 {
			c := s[0]
			s = s[1:]
			switch c {
			case '!':
				return o, actHelp, nil
			case 't':
				o.target, s = charValue(s, 's')
			case 'm':
				o.zmin, s = intValue(s)
			case 'n':
				o.zmax, s = intValue(s)
			case 's':
				o.smin, s = floatValue(s)
			case 'S':
				o.smax, s = floatValue(s)
			case 'o':
				o.orig = true
			case 'c':
				o.conf, s = floatValue(s)
			case 'e':
				o.eval, s = charValue(s, 0)
			case 'a':
				o.agg, s = charValue(s, 0)
			case 'd':
				o.thresh, s = floatValue(s)
			case 'i':
				o.invBxs = true
			case 'p':
				o.prune, s = intValue(s)
			case 'q':
				o.sort, s = intValue(s)
			case 'A':
				o.algo, s = charValue(s, 0)
			case 'x':
				o.noPerf = true
			case 'l':
				o.pack, s = intValue(s)
			case 'j':
				o.noReord = true
			case 'u':
				o.noTail = true
			case 'F':
				o.border, s = borderValue(s)
			case 'R':
				pending = newString(&o.selFile)
			case 'P':
				pending = newString(&o.pspFile)
			case 'Z':
				o.stats = true
			case 'N': // pre-formatting of integers only affects upstream's speed
			case 'g':
				o.scan = true
			case 'h':
				pending = &o.hdr
			case 'k':
				pending = &o.sep
			case 'I':
				pending = &o.imp
			case 'v':
				pending, o.infoSet = &o.info, true
			case 'w':
				o.weight = true
			case 'r':
				pending = newString(&o.recSeps)
			case 'f':
				pending = newString(&o.fldSeps)
			case 'b':
				pending = newString(&o.blanks)
			case 'C':
				pending = newString(&o.comment)
			default:
				return o, actRun, errorf(exitOption, "unknown option -"+string([]byte{c}))
			}
			if pending != nil && len(s) > 0 { // value attached to the option
				*pending, pending = s, nil
				break
			}
		}
	}
	if pending != nil {
		return o, actRun, errorf(exitOptArg, "missing option argument")
	}
	if k < 1 {
		return o, actRun, errorf(exitArgCount, "wrong number of arguments")
	}
	return o, actRun, nil
}

// newString points *p at a new empty string and returns it.
func newString(p **string) *string {
	*p = new(string)
	return *p
}

// charValue is "(*s) ? *s++ : def".
func charValue(s string, def byte) (byte, string) {
	if s == "" {
		return def, s
	}
	return s[0], s[1:]
}

// intValue is "(int)strtol(s, &s, 0)": a C long truncated to int.
func intValue(s string) (int32, string) {
	v, n, _ := cconv.Strtol(s, 0)
	i, _ := cconv.LongToInt(v)
	return i, s[n:]
}

// floatValue is "strtod(s, &s)".
func floatValue(s string) (float64, string) {
	v, n := cconv.Strtod(s)
	return v, s[n:]
}

// borderValue is a port of getbdr: a ':'-separated list of strtod values;
// parsing continues after the last number.
//
// Deviation from upstream (cli-io.md B16): when the list ends in an empty
// value ("-F", "-F60:", "-F:5"), getbdr counts the unparsed slot, so an
// uninitialized double becomes a border value. Here only parsed values
// count; "-F" without a value gives no border. The position where option
// parsing continues is upstream's.
func borderValue(s string) ([]float64, string) {
	k := strings.Count(s, ":") + 1
	b := []float64{}
	for range k {
		v, n := cconv.Strtod(s)
		if n == 0 {
			break // empty parameter: upstream's end pointer stays at s
		}
		b = append(b, v)
		s = s[n:]
		if s == "" || s[0] != ':' {
			break
		}
		s = s[1:]
	}
	return b, s
}

// validate checks the options in upstream's order and returns the first
// error. Checks marked as deviations reject values upstream accepts with
// undefined or meaningless behavior (docs/upstream/reference-build.md
// F10, cli-io.md B17).
func (o *options) validate() *cliError {
	switch {
	case o.zmin < 0:
		return errorf(exitSize, "invalid item set size "+strconv.Itoa(int(o.zmin)))
	case o.zmax < 0:
		return errorf(exitSize, "invalid item set size "+strconv.Itoa(int(o.zmax)))
	// Deviation from upstream: NaN passes "smin > 100" and is converted
	// to an integer with undefined behavior; it is rejected here.
	case o.smin > 100 || math.IsNaN(o.smin):
		return errorf(exitSupport, "invalid minimum support "+formatG(o.smin))
	// Deviation from upstream: -S is not validated upstream; NaN is
	// rejected here (other values, also -S0, are well defined).
	case math.IsNaN(o.smax):
		return errorf(exitSupport, "invalid maximum support "+formatG(o.smax))
	}
	for _, v := range o.border {
		// Deviation from upstream: a NaN border value is converted to an
		// integer with undefined behavior upstream.
		if math.IsNaN(v) {
			return errorf(exitSupport, "invalid support border "+formatG(v))
		}
	}
	// NaN fails both comparisons upstream and is accepted; rejected here.
	if !(o.conf >= 0 && o.conf <= 100) {
		return errorf(exitConf, "invalid minimum confidence "+formatG(o.conf))
	}
	// Deviation from upstream: -d is not validated upstream.
	if math.IsNaN(o.thresh) {
		return errorf(exitMeasure, "invalid evaluation threshold "+formatG(o.thresh))
	}
	inStdin := o.inFile == nil || *o.inFile == ""
	if inStdin && o.selFile != nil && *o.selFile == "" {
		return errorf(exitStdin, "double assignment of standard input")
	}
	if _, ok := targetOf(o.target); !ok {
		return errorf(exitTarget, "invalid target type '"+string([]byte{o.target})+"'")
	}
	if _, ok := evalOf(o.eval); !ok {
		return errorf(exitMeasure, "invalid evaluation measure '"+string([]byte{o.eval})+"'")
	}
	if _, ok := aggOf(o.agg); !ok {
		return errorf(exitAggMode, "invalid aggregation mode '"+string([]byte{o.agg})+"'")
	}
	if _, ok := algoOf(o.algo); !ok {
		return errorf(exitVariant, "invalid fpgrowth variant '"+string([]byte{o.algo})+"'")
	}
	return nil
}

func targetOf(c byte) (report.Target, bool) {
	switch c {
	case 's', 'f':
		return report.TargetAll, true
	case 'c':
		return report.TargetClosed, true
	case 'm':
		return report.TargetMaximal, true
	case 'g':
		return report.TargetGenerators, true
	case 'r':
		return report.TargetRules, true
	}
	return 0, false
}

// evalOf maps the -e letter: 'b' is fpgrowth's own item set measure (for
// rules fpg maps it to the imbalance ratio, deviation R1), the other
// letters are re_getid's.
func evalOf(c byte) (int, bool) {
	if c == 'b' {
		return fpg.EvalLogRatio, true
	}
	m, ok := ruleval.ByLetter(c)
	return int(m), ok
}

func aggOf(c byte) (int, bool) {
	switch c {
	case 'x':
		return fpg.AggNone, true
	case 'm':
		return fpg.AggMin, true
	case 'n':
		return fpg.AggMax, true
	case 'a':
		return fpg.AggAvg, true
	}
	return 0, false
}

func algoOf(c byte) (fpg.Algorithm, bool) {
	switch c {
	case 's':
		return fpg.AlgoSimple, true
	case 'c':
		return fpg.AlgoComplex, true
	case 'd':
		return fpg.AlgoSingle, true
	case 't':
		return fpg.AlgoTopDown, true
	}
	return 0, false
}

// isRules reports whether association rules are the target.
func (o *options) isRules() bool { return o.target == 'r' }

// minerConfig translates validated options into the miner configuration
// (fpg_create's arguments, the -q sort and the -p prune values).
func (o *options) minerConfig() fpg.Config {
	c := fpg.DefaultConfig()
	c.Target, _ = targetOf(o.target)
	c.SMin, c.SMax, c.Conf = o.smin, o.smax, o.conf
	c.ZMin, c.ZMax = int(o.zmin), int(o.zmax)
	c.Eval, _ = evalOf(o.eval)
	c.InvalidateBelowExpected = o.invBxs
	c.Agg, _ = aggOf(o.agg)
	c.Thresh = o.thresh
	c.Algo, _ = algoOf(o.algo)
	if o.orig {
		c.Mode |= fpg.ModeOrigSupp
	}
	if o.noPerf {
		c.Mode &^= fpg.ModePerfect
	}
	if o.noReord {
		c.Mode &^= fpg.ModeReorder
	}
	if o.noTail {
		c.Mode &^= fpg.ModeTail
	}
	c.Pack = int(max(0, min(o.pack, 16)))
	c.Sort = int(o.sort)
	c.Prune = int(o.prune)
	return c
}

// formatG formats x like C's printf("%g").
func formatG(x float64) string {
	switch {
	case math.IsNaN(x):
		return "nan"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	return strconv.FormatFloat(x, 'g', 6, 64)
}
