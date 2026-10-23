package isrout

import (
	"bufio"
	"io"
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/report"
)

// AppendStats is a port of isr_prstats(rep, out, 0), the statistics of
// option -Z: it appends "all: <count>" and one line "%3d: <n>" per size
// from 0 to len(stats)-1. count is the number of reported sets or rules
// (report.Reporter.Count) and stats the counts per size
// (report.Reporter.Stats, trimmed after the largest non-zero size).
func AppendStats(dst []byte, count uint64, stats []uint64) []byte {
	dst = append(dst, "all: "...)
	dst = strconv.AppendUint(dst, count, 10)
	dst = append(dst, '\n')
	for size, n := range stats {
		for p := 100; p > 1 && size < p; p /= 10 { // "%3d"
			dst = append(dst, ' ')
		}
		dst = strconv.AppendInt(dst, int64(size), 10)
		dst = append(dst, ": "...)
		dst = strconv.AppendUint(dst, n, 10)
		dst = append(dst, '\n')
	}
	return dst
}

// WriteStats writes the statistics of option -Z (see AppendStats) to w and
// returns the write error, if any.
func WriteStats(w io.Writer, count uint64, stats []uint64) error {
	_, err := w.Write(AppendStats(nil, count, stats))
	return err
}

// WriteSpectrum is a port of psp_report(psp, twr, 1.0) as used by option
// -P: one line "<size> <support> <count>" per non-zero cell of p, sizes
// ascending, then supports ascending. The count is formatted like C's
// "%g" (6 significant digits, so counts of 10^6 and more are rounded,
// e.g. 5.2003e+06). A nil spectrum writes nothing.
//
// Deviation from upstream: fpgrowth ignores write and close errors of the
// pattern spectrum file (cli-io.md B9, docs/differences.md O7). Here the
// first write error, including that of the final flush, is returned.
func WriteSpectrum(w io.Writer, p *report.Spectrum) error {
	return WriteSpectrumScaled(w, p, 1.0)
}

// WriteSpectrumScaled is a port of psp_report(psp, twr, scale): like
// WriteSpectrum, with every count c printed as the C double product
// scale*(double)c (fpgpsp: scale = 1/(number of surrogates), the average
// count per surrogate data set). With scale 1 it is WriteSpectrum.
func WriteSpectrumScaled(w io.Writer, p *report.Spectrum, scale float64) error {
	if p == nil {
		return nil
	}
	bw := bufio.NewWriterSize(w, bufferSize)
	var line []byte
	for c := range p.All() {
		line = strconv.AppendInt(line[:0], int64(c.Size), 10)
		line = append(line, ' ')
		line = strconv.AppendInt(line, int64(c.Supp), 10)
		line = append(line, ' ')
		line = appendG(line, scale*float64(c.Count))
		line = append(line, '\n')
		if _, err := bw.Write(line); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// appendG appends x formatted like C's printf("%g", x) for finite x:
// strconv's 'g' format with precision 6 removes trailing zeros and
// switches to the exponential form (with at least two exponent digits)
// at the same exponents as C.
func appendG(dst []byte, x float64) []byte {
	return strconv.AppendFloat(dst, x, 'g', 6, 64)
}
