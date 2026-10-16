package isrout

import (
	"bufio"
	"errors"
	"io"
	"math"
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Defaults of the fpgrowth program's format options (fpgrowth.c main).
const (
	DefaultSep = " "    // item separator (-k)
	DefaultImp = " <- " // implication sign (-I)
)

// bufferSize is the size of the output buffer (upstream BS_WRITE).
const bufferSize = 64 * 1024

// ErrClosed is returned by the methods of a closed Writer.
var ErrClosed = errors.New("isrout: writer is closed")

// DefaultInfo returns the information format fpgrowth substitutes when
// option -v is not given (fpgrowth.c:2457-2461): " (%S)" for item sets and
// " (%X, %C)" for rules, or with absolute support (negative -s) " (%a)"
// and " (%b, %C)".
func DefaultInfo(rules, absolute bool) string {
	switch {
	case rules && absolute:
		return " (%b, %C)"
	case rules:
		return " (%X, %C)"
	case absolute:
		return " (%a)"
	default:
		return " (%S)"
	}
}

// Format holds the output format options of the fpgrowth program.
type Format struct {
	// Header is written at the start of every item set and rule line
	// (-h).
	Header string
	// Sep separates the items of a set and of a rule body (-k).
	Sep string
	// Imp separates the rule head from the rule body (-I).
	Imp string
	// Info is the information format appended to every line (-v); see
	// the package documentation for its codes.
	Info string
	// Scan writes item names in scanable form (-g): names that contain
	// characters other than letters, digits, '_', '+', '-' and '.' are
	// quoted, with special characters escaped.
	Scan bool
}

// Config configures a Writer.
type Config struct {
	// Names are the item names by item code (the recoded item base).
	Names []string
	// Total is the total transaction weight, the support of the empty
	// set (upstream supps[0]); it is the denominator of the relative
	// supports and the value of %Q.
	Total tract.Supp
	Format
}

// Writer writes item sets and association rules in the text format of
// upstream's item set reporter (isr_report/isr_reprule with isr_sinfo and
// isr_rinfo). It implements report.Sink.
//
// Output is buffered; the first write error is returned by the call that
// hit it and by every later call, including Flush and Close. A Writer is
// not safe for concurrent use.
type Writer struct {
	bw       *bufio.Writer
	names    []string
	header   string
	sep      string
	imp      string
	info     string
	smax     float64 // total as double, 1 if not positive
	line     []byte  // line buffer, reused
	err      error   // first write error (sticky)
	isClosed bool
}

var _ report.Sink = (*Writer)(nil)

// New returns a Writer that writes to w with the configuration cfg. The
// Writer keeps cfg.Names (formatted for scanable output if cfg.Scan is
// set); it must not be modified while the Writer is in use.
func New(w io.Writer, cfg Config) *Writer {
	names := cfg.Names
	if cfg.Scan {
		// Deviation from upstream: isr_createx formats the names before
		// isr_setfmtx sets the scan flag, so -g has no effect upstream
		// (report.md B2, docs/differences.md O2). Here the names are
		// formatted as documented, with scn_format's rules.
		names = make([]string, len(cfg.Names))
		for i, name := range cfg.Names {
			names[i] = scanable(name)
		}
	}
	smax := float64(cfg.Total)
	if smax <= 0 {
		smax = 1
	}
	return &Writer{
		bw:     bufio.NewWriterSize(w, bufferSize),
		names:  names,
		header: cfg.Header,
		sep:    cfg.Sep,
		imp:    cfg.Imp,
		info:   cfg.Info,
		smax:   smax,
		line:   make([]byte, 0, 256),
	}
}

// ItemSet writes the line of an item set (report.c output): the header,
// the item names separated by Sep, the expansion of Info and a newline.
func (w *Writer) ItemSet(s report.ItemSet) error {
	if err := w.check(); err != nil {
		return err
	}
	// Deviation from upstream: isr_setfmtx copies the header but never
	// assigns rep->hdr, so -h has no effect upstream (report.md B1,
	// docs/differences.md O1). Here the header starts every line.
	b := append(w.line[:0], w.header...)
	for i, item := range s.Items {
		if i > 0 {
			b = append(b, w.sep...)
		}
		b = append(b, w.names[item]...)
	}
	b = w.appendSetInfo(b, s)
	b = append(b, '\n')
	w.line = b
	return w.write(b)
}

// Rule writes the line of an association rule (isr_reprule): the header,
// the head's name, Imp, the body's names separated by Sep, the expansion
// of Info and a newline.
func (w *Writer) Rule(r report.Rule) error {
	if err := w.check(); err != nil {
		return err
	}
	b := append(w.line[:0], w.header...) // see ItemSet on the header
	b = append(b, w.names[r.Head]...)
	b = append(b, w.imp...)
	for i, item := range r.Body {
		if i > 0 {
			b = append(b, w.sep...)
		}
		b = append(b, w.names[item]...)
	}
	b = w.appendRuleInfo(b, r)
	b = append(b, '\n')
	w.line = b
	return w.write(b)
}

// WriteStats writes the item set statistics of option -Z (see the
// package function WriteStats) to the Writer's output, after everything
// written so far.
//
// Deviation from upstream: fpgrowth prints the statistics with fprintf to
// stdout while the item sets are still in the reporter's private buffer,
// so with output to stdout they appear before the sets or spliced into
// them at a 64 KiB boundary (report.md B9, docs/differences.md O6). Here
// they are written in order.
func (w *Writer) WriteStats(count uint64, stats []uint64) error {
	if err := w.check(); err != nil {
		return err
	}
	w.line = AppendStats(w.line[:0], count, stats)
	return w.write(w.line)
}

// Flush writes buffered output to the underlying writer.
func (w *Writer) Flush() error {
	if err := w.check(); err != nil {
		return err
	}
	if err := w.bw.Flush(); err != nil {
		w.err = err
		return err
	}
	return nil
}

// Close flushes the output (isr_close). It does not close the underlying
// writer, which belongs to the caller. It returns the first write error,
// if any; later calls of any method return ErrClosed or that error.
func (w *Writer) Close() error {
	err := w.Flush()
	w.isClosed = true
	return err
}

func (w *Writer) check() error {
	if w.err != nil {
		return w.err
	}
	if w.isClosed {
		return ErrClosed
	}
	return nil
}

func (w *Writer) write(b []byte) error {
	if _, err := w.bw.Write(b); err != nil {
		w.err = err
		return err
	}
	return nil
}

// appendSetInfo is a port of isr_sinfo: it appends the expansion of the
// information format for an item set.
func (w *Writer) appendSetInfo(b []byte, s report.ItemSet) []byte {
	sdbl := float64(s.Supp)
	smax := w.smax
	wmax := smax // wgts[0] is the total weight as well
	wgt := s.Weight
	eval := s.Eval
	info := w.info
	for i := 0; i < len(info); {
		if info[i] != '%' {
			j := i + 1
			for j < len(info) && info[j] != '%' {
				j++
			}
			b = append(b, info[i:j]...)
			i = j
			continue
		}
		k, n := parseDigits(info[i+1:])
		c := i + 1 + n // position of the code character
		if c >= len(info) {
			// '%' and digits at the end are copied (case 0).
			return append(b, info[i:]...)
		}
		switch info[c] {
		case '%':
			b = append(b, '%')
		case 'i':
			b = strconv.AppendInt(b, int64(len(s.Items)), 10)
		case 'a':
			b = strconv.AppendInt(b, int64(s.Supp), 10)
		case 'd':
			// Deviation from upstream: %d (absolute support) is only
			// understood by upstream's fast output path and printed
			// literally otherwise (report.md B6). Here it is always the
			// absolute support, as on the fast path.
			b = strconv.AppendInt(b, int64(s.Supp), 10)
		case 'q', 'Q':
			b = strconv.AppendInt(b, floatToInt(smax), 10)
		case 's', 'x':
			b = appendNum(b, sdbl/smax, k)
		case 'S', 'X':
			b = appendNum(b, float64(100*(sdbl/smax)), k)
		case 'w':
			b = appendNum(b, wgt, k)
		case 'W':
			b = appendNum(b, float64(100*wgt), k)
		case 'r':
			b = appendNum(b, wgt/wmax, k)
		case 'R':
			b = appendNum(b, float64(100*(wgt/wmax)), k)
		case 'z':
			b = appendNum(b, float64(wgt*smax), k)
		case 'e', 'p':
			b = appendNum(b, eval, k)
		case 'E', 'P':
			b = appendNum(b, float64(100*eval), k)
		default: // unknown code: copy '%', digits and the character
			b = append(b, info[i:c+1]...)
		}
		i = c + 1
	}
	return b
}

// appendRuleInfo is a port of isr_rinfo: it appends the expansion of the
// information format for an association rule. Codes that isr_rinfo does
// not know (among them %q, %w and %d) are copied literally.
func (w *Writer) appendRuleInfo(b []byte, r report.Rule) []byte {
	smax := w.smax
	supp := float64(r.Supp)
	body := float64(r.BodySupp)
	head := float64(r.HeadSupp)
	info := w.info
	for i := 0; i < len(info); {
		if info[i] != '%' {
			j := i + 1
			for j < len(info) && info[j] != '%' {
				j++
			}
			b = append(b, info[i:j]...)
			i = j
			continue
		}
		k, n := parseDigits(info[i+1:])
		c := i + 1 + n
		if c >= len(info) {
			return append(b, info[i:]...)
		}
		switch info[c] {
		case '%':
			b = append(b, '%')
		case 'i':
			b = strconv.AppendInt(b, int64(len(r.Body)+1), 10)
		case 'a':
			b = strconv.AppendInt(b, int64(r.Supp), 10)
		case 'b':
			b = strconv.AppendInt(b, int64(r.BodySupp), 10)
		case 'h':
			b = strconv.AppendInt(b, int64(r.HeadSupp), 10)
		case 'Q':
			b = strconv.AppendInt(b, floatToInt(smax), 10)
		case 's':
			b = appendNum(b, supp/smax, k)
		case 'S':
			b = appendNum(b, float64(100*(supp/smax)), k)
		case 'x':
			b = appendNum(b, body/smax, k)
		case 'X':
			b = appendNum(b, float64(100*(body/smax)), k)
		case 'y':
			b = appendNum(b, head/smax, k)
		case 'Y':
			b = appendNum(b, float64(100*(head/smax)), k)
		case 'c':
			b = appendNum(b, confidence(r), k)
		case 'C':
			b = appendNum(b, float64(100*confidence(r)), k)
		case 'l':
			b = appendNum(b, lift(r, smax), k)
		case 'L':
			b = appendNum(b, float64(100*lift(r, smax)), k)
		case 'e':
			b = appendNum(b, r.Eval, k)
		case 'E':
			b = appendNum(b, float64(100*r.Eval), k)
		default:
			b = append(b, info[i:c+1]...)
		}
		i = c + 1
	}
	return b
}

// confidence is the %c value of isr_rinfo.
func confidence(r report.Rule) float64 {
	if r.BodySupp <= 0 {
		return 0
	}
	return float64(r.Supp) / float64(r.BodySupp)
}

// lift is the %l value of isr_rinfo.
func lift(r report.Rule, smax float64) float64 {
	if r.BodySupp <= 0 || r.HeadSupp <= 0 {
		return 0
	}
	return float64(float64(r.Supp)*smax) /
		float64(float64(r.BodySupp)*float64(r.HeadSupp))
}

// floatToInt converts like C's (ptrdiff_t) cast of a double on the
// reference platform (arm64 saturates); upstream prints the clamped total
// weight this way for %q and %Q.
func floatToInt(x float64) int64 {
	if x >= 0x1p63 {
		return math.MaxInt64
	}
	return int64(x)
}
