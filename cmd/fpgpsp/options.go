package main

import (
	"math"
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/cconv"
	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/psp"
	"github.com/dimchansky/fpgrowth-go/internal/report"
)

// Exit codes and messages of upstream's main (fpgpsp.c errmsgs[] and the
// tract.c reader codes). Some codes differ from fpgrowth's (E_VARIANT is
// 12 here).
const (
	exitOK        = 0
	exitNoMem     = 1  // E_NOMEM
	exitOpen      = 2  // E_FOPEN
	exitRead      = 3  // E_FREAD
	exitWrite     = 4  // E_FWRITE
	exitOption    = 6  // E_OPTION
	exitOptArg    = 7  // E_OPTARG
	exitArgCount  = 8  // E_ARGCNT
	exitTarget    = 9  // E_TARGET
	exitSize      = 10 // E_SIZE
	exitSupport   = 11 // E_SUPPORT
	exitVariant   = 12 // E_VARIANT
	exitSurrCount = 13 // E_SURRCNT
	exitSurr      = 14 // E_SURR
	exitNoItems   = 15 // E_NOITEMS
	exitItemExp   = 16 // E_ITEMEXP
	exitDupItem   = 18 // E_DUPITEM
	exitWgtExp    = 20 // E_WGTEXP
	exitTaWgt     = 21 // E_TAWGT
	exitFldCnt    = 22 // E_FLDCNT
	exitAppExp    = 23 // E_APPEXP
	exitUnkApp    = 24 // E_UNKAPP
	exitNoTable   = 27 // E_NOTABLE
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
	actRun   action = iota // generate or estimate
	actUsage               // no arguments: usage text
	actHelp                // -!: additional option information
)

// options holds the parsed command line with upstream's defaults
// (fpgpsp.c main). String options are nil when not given; an empty file
// name means standard input or output.
type options struct {
	target byte // -t letter (0 if missing)
	zmin   int32
	zmax   int32
	supp   float64 // -s: percentage if >= 0, else absolute
	algo   byte    // -A letter (0 if missing)
	noPerf bool    // -x
	pack   int32   // -l
	noReo  bool    // -i
	noTail bool    // -u
	surr   byte    // -g letter (0 if missing)
	cnt    int64   // -c (long)
	alpha  float64 // -e
	smpls  int64   // -z (long)
	seed   int64   // -S (long); 0: time
	seedOK bool    // -S given
	cpus   int32   // -Z; <= 0: all CPUs
	border bool    // -D

	inFile, outFile  *string
	selFile          *string
	recSeps, fldSeps *string
	blanks, comment  *string
}

// defaultOptions returns upstream's defaults.
func defaultOptions() options {
	return options{
		target: 's',
		zmin:   1,
		zmax:   math.MaxInt32,
		supp:   10,
		algo:   'c',
		pack:   16,
		surr:   'e',
		cnt:    1000,
		alpha:  0.5,
		smpls:  1000,
	}
}

// parseArgs evaluates the arguments (without the program name) like
// upstream's main: option clusters, char-valued options ("(*s) ? *s++ :
// 0"), numeric options (strtol / strtod prefixes of the rest of the
// cluster), string options (rest of the argument or the next argument
// verbatim), "-" as an empty file name, and errors in scan order. It
// stops at -! (actHelp).
//
// Extension (docs/differences.md U6): one non-option argument is enough;
// the pattern spectrum then goes to standard output. Upstream's usage
// message calls the output file optional but its main requires it.
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
				o.target, s = charValue(s)
			case 'm':
				o.zmin, s = intValue(s)
			case 'n':
				o.zmax, s = intValue(s)
			case 's':
				o.supp, s = floatValue(s)
			case 'A':
				o.algo, s = charValue(s)
			case 'x':
				o.noPerf = true
			case 'l':
				o.pack, s = intValue(s)
			case 'i':
				o.noReo = true
			case 'u':
				o.noTail = true
			case 'g':
				o.surr, s = charValue(s)
			case 'c':
				o.cnt, s = longValue(s)
			case 'e':
				o.alpha, s = floatValue(s)
			case 'z':
				o.smpls, s = longValue(s)
			case 'S':
				o.seed, s = longValue(s)
				o.seedOK = true
			case 'Z':
				o.cpus, s = intValue(s)
			case 'D':
				o.border = true
			case 'R':
				pending = newString(&o.selFile)
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

// charValue is "(*s) ? *s++ : 0".
func charValue(s string) (byte, string) {
	if s == "" {
		return 0, s
	}
	return s[0], s[1:]
}

// intValue is "(int)strtol(s, &s, 0)": a C long truncated to int.
func intValue(s string) (int32, string) {
	v, n, _ := cconv.Strtol(s, 0)
	i, _ := cconv.LongToInt(v)
	return i, s[n:]
}

// longValue is "strtol(s, &s, 0)" into a 64-bit long.
func longValue(s string) (int64, string) {
	v, n, _ := cconv.Strtol(s, 0)
	return v, s[n:]
}

// floatValue is "strtod(s, &s)".
func floatValue(s string) (float64, string) {
	v, n := cconv.Strtod(s)
	return v, s[n:]
}

// validate checks the options in upstream's order and returns the first
// error. Checks marked as deviations reject values for which upstream's
// behavior is undefined.
func (o *options) validate() *cliError {
	switch {
	case o.zmin < 0:
		return errorf(exitSize, "invalid item set size "+strconv.Itoa(int(o.zmin)))
	case o.zmax < 0:
		return errorf(exitSize, "invalid item set size "+strconv.Itoa(int(o.zmax)))
	// Deviation from upstream: NaN passes "supp > 100" upstream and is
	// converted to an integer with undefined behavior. The message prints
	// the value with %g; upstream's format is "%d" for a double argument
	// (undefined output, U14 in docs/differences.md).
	case o.supp > 100 || math.IsNaN(o.supp):
		return errorf(exitSupport, "invalid minimum support "+formatG(o.supp))
	case o.cnt <= 0:
		return errorf(exitSurrCount, "invalid number of surrogates "+strconv.FormatInt(o.cnt, 10))
	}
	if _, ok := targetOf(o.target); !ok {
		return errorf(exitTarget, "invalid target type '"+string([]byte{o.target})+"'")
	}
	if _, ok := algoOf(o.algo); !ok {
		return errorf(exitVariant, "invalid fpgrowth variant '"+string([]byte{o.algo})+"'")
	}
	if _, ok := methodOf(o.surr); !ok {
		return errorf(exitSurr, "invalid surrogate data generation method '"+string([]byte{o.surr})+"'")
	}
	if o.surr == 'e' {
		// Deviation from upstream (U15): a NaN dispersion factor and a
		// number of samples <= 0 (which only the estimation uses) give
		// undefined results upstream (assertion, division by zero,
		// size_t conversion of a negative value).
		if math.IsNaN(o.alpha) {
			return errorf(exitSurrCount, "invalid probability dispersion factor "+formatG(o.alpha))
		}
		if o.alpha > 0 && o.smpls <= 0 {
			return errorf(exitSurrCount, "invalid number of item set samples "+strconv.FormatInt(o.smpls, 10))
		}
	}
	return nil
}

func targetOf(c byte) (report.Target, bool) {
	switch c {
	case 's':
		return report.TargetAll, true
	case 'c':
		return report.TargetClosed, true
	case 'm':
		return report.TargetMaximal, true
	case 'g':
		return report.TargetGenerators, true
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

// methodOf maps the -g letter; 'e' (estimation) has no surrogate method.
func methodOf(c byte) (psp.Method, bool) {
	switch c {
	case 'e':
		return -1, true
	case 'i':
		return psp.Identity, true
	case 'r':
		return psp.Random, true
	case 'p':
		return psp.Swap, true
	case 's':
		return psp.Shuffle, true
	}
	return 0, false
}

// minerConfig is the configuration of fpg_genpsp's fpg_create: the
// target, the minimum support, maximum support 100%, the sizes, no
// evaluation, the variant and the mode of -x, -i, -u and -l.
func (o *options) minerConfig() fpg.Config {
	c := fpg.DefaultConfig()
	c.Target, _ = targetOf(o.target)
	c.SMin = o.supp
	c.ZMin, c.ZMax = int(o.zmin), int(o.zmax)
	c.Algo, _ = algoOf(o.algo)
	if o.noPerf {
		c.Mode &^= fpg.ModePerfect
	}
	if o.noReo {
		c.Mode &^= fpg.ModeReorder
	}
	if o.noTail {
		c.Mode &^= fpg.ModeTail
	}
	c.Pack = int(max(0, min(o.pack, 16)))
	return c
}

// estimConfig is the configuration of fpg_estpsp's fpg_create: the
// target, the support and the sizes, with FPG_AUTO (the complex variant)
// and FPG_DEFAULT, whatever -A, -x, -i, -u and -l say.
func (o *options) estimConfig() fpg.Config {
	c := fpg.DefaultConfig()
	c.Target, _ = targetOf(o.target)
	c.SMin = o.supp
	c.ZMin, c.ZMax = int(o.zmin), int(o.zmax)
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
