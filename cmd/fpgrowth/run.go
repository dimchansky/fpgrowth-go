package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/isrout"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/thresh"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// env is the environment of one run of the program.
type env struct {
	args   []string // program name and arguments (os.Args)
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// retain is the budget of the retained records in bytes; 0 means
	// retainLimit and a negative value disables retention (tests).
	retain int
	// tempDir is the directory for the spool file of standard input;
	// empty means os.TempDir().
	tempDir string
	// create opens the item set output file; nil means os.Create. Tests
	// use it to inject write and close errors.
	create func(name string) (io.WriteCloser, error)
}

// run executes the program like upstream's main and returns the exit
// status. Diagnostics go to e.stderr; item sets go to the output file or
// e.stdout. Cancellation of ctx (SIGINT) stops reading and mining.
func run(ctx context.Context, e env) int {
	prog := e.args[0]
	if len(e.args) == 1 { // no arguments: usage message
		writeUsage(e.stdout, prog)
		return exitOK
	}
	writeBanner(e.stderr, prog)
	o, act, cerr := parseArgs(e.args[1:])
	if cerr == nil && act == actHelp {
		fmt.Fprint(e.stderr, "\n") // terminate the startup message
		writeHelp(e.stdout)
		return exitOK
	}
	if cerr == nil {
		cerr = o.validate()
	}
	if cerr == nil {
		fmt.Fprint(e.stderr, "\n") // terminate the startup message
		r := &runner{env: e, ctx: ctx, o: &o}
		cerr = r.mine()
		r.cleanup()
	}
	if cerr != nil {
		fmt.Fprintf(e.stderr, "\n%s: %s\n", prog, cerr.msg)
		return cerr.code
	}
	return exitOK
}

// retainLimit is the memory, in bytes, that the item base may use to keep
// the counted records for building the transaction bag, so that the
// database is read once instead of twice (tract.ItemBase.Retain). A
// larger database is read a second time. The records take 4 bytes per
// item occurrence and per record, so 128 MiB hold about 25 million item occurrences in records
// of a few items (a table of roughly 100 MB).
const retainLimit = 128 << 20

// retainBudget returns the budget of the retained records.
func (r *runner) retainBudget() int {
	if r.retain == 0 {
		return retainLimit
	}
	return r.retain
}

// runner holds the state of the mining pipeline.
type runner struct {
	env
	ctx      context.Context
	o        *options
	cleanups []func()
}

// cleanup runs the registered cleanup functions (removal of the spool
// file) in reverse order.
func (r *runner) cleanup() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	r.cleanups = nil
}

// logf writes a progress message to stderr (upstream MSG/XMSG; the
// program is always verbose).
func (r *runner) logf(format string, args ...any) {
	fmt.Fprintf(r.stderr, format, args...)
}

// done ends a progress message with the elapsed time.
func (r *runner) done(start time.Time, end string) {
	r.logf(" done [%.2fs].%s", time.Since(start).Seconds(), end)
}

// transactions formats "<n>[/<w>]" like upstream's logs.
func transactions(n int, w tract.Supp) string {
	if w != tract.Supp(n) {
		return fmt.Sprintf("%d/%d", n, w)
	}
	return fmt.Sprintf("%d", n)
}

// tabConfig returns the input format of the options.
func (r *runner) tabConfig() tabread.Config {
	cfg := tabread.DefaultConfig()
	for _, c := range []struct {
		opt *string
		dst *string
	}{
		{r.o.recSeps, &cfg.RecordSeps},
		{r.o.fldSeps, &cfg.FieldSeps},
		{r.o.blanks, &cfg.Blanks},
		{r.o.comment, &cfg.Comments},
	} {
		if c.opt != nil {
			*c.dst = tabread.DecodeEscapes(*c.opt)
		}
	}
	cfg.Weighted = r.o.weight
	return cfg
}

// displayName is upstream's trd_name/isr_name: "<stdin>" or "<stdout>"
// for an empty name.
func displayName(name, std string) string {
	if name == "" {
		return std
	}
	return name
}

// mine is the pipeline of upstream's main after option validation.
func (r *runner) mine() *cliError {
	o := r.o
	cfg := r.tabConfig()
	ib := tract.NewItemBase[string]()
	ib.Retain(r.retainBudget()) // read the database once if it fits

	// --- read item selection/appearance indicators ---
	if o.selFile != nil {
		if err := r.readItems(ib, *o.selFile, cfg); err != nil {
			return err
		}
	}

	// --- read transaction database (pass 1) ---
	inName := displayName(*o.inFile, "<stdin>")
	scan, cerr := r.openInput(*o.inFile, inName, cfg)
	if cerr != nil {
		return cerr
	}
	start := time.Now()
	if err := tract.CountBytes(ib, scan); err != nil {
		return r.inputError(err, inName)
	}
	n, w := ib.Records(), ib.TotalWeight()
	r.logf("[%d item(s), %s transaction(s)]", ib.Len(), transactions(n, w))
	r.done(start, "")
	if ib.Len() <= 0 || n <= 0 {
		return errorf(exitNoItems, "no (frequent) items found")
	}
	r.logf("\n")

	// --- prepare the data (fpg_data) ---
	m, err := fpg.New[string](o.minerConfig())
	if err != nil {
		return errorf(exitNoMem, err.Error()) // excluded by validate
	}
	start = time.Now()
	r.logf("filtering, sorting and recoding items ... ")
	ib.SetAbort(r.aborted) // completing a large bag takes a while
	// Pass 2, unless the records were retained: over byte-string names,
	// like pass 1, so that no name is converted to a string (issue #52).
	if err := fpg.DataBytes(m, ib, scan); err != nil {
		if errors.Is(err, fpg.ErrNoItems) {
			return errorf(exitNoItems, "no (frequent) items found")
		}
		return r.inputError(err, inName)
	}
	r.logf("[%d item(s)]", ib.Len())
	r.done(start, "\n")
	start = time.Now()
	bag := m.Bag()
	r.logf("sorting and reducing transactions ... [%s transaction(s)]",
		transactions(bag.Len(), bag.TotalWeight()))
	r.done(start, "\n")

	// --- configure the reporter (fpg_report, setbdr, isr_addpsp) ---
	rep, err := m.Reporter()
	if err != nil {
		return errorf(exitNoMem, err.Error())
	}
	for i, v := range o.border {
		s := tract.Supp(v.count) // exact absolute integer literal (N3)
		if !v.exact() {
			s = borderSupp(v.v, w)
		}
		rep.SetBorder(int(o.zmin)+i, s)
	}
	if o.pspFile != nil && !o.isRules() {
		rep.AddSpectrum()
	}
	info := o.info
	if !o.infoSet {
		info = isrout.DefaultInfo(o.isRules(), o.smin.v < 0)
	}

	// --- open the output and mine ---
	out, cerr := r.openOutput(ib, bag, info)
	if cerr != nil {
		return cerr
	}
	if out.w == nil {
		rep.SetSink(nil) // count only
	} else {
		rep.SetSink(out)
	}
	if err := r.runMiner(m, rep, out.name); err != nil && !out.failed {
		_ = out.close() // the run failed anyway
		return err
	} // a write error of the output is reported when closing it

	// --- statistics and closing (isr_prstats, isr_close) ---
	var statsErr, closeErr error
	if o.stats {
		if out.isStdout && out.zw == nil {
			_ = out.w.WriteStats(rep.Count(), rep.Stats()) // error at close
		} else {
			if out.isStdout {
				// -z: the statistics are not compressed; they follow the
				// complete zlib stream (docs/differences.md O12).
				closeErr = out.close()
			}
			// Deviation from upstream: write errors of the statistics are
			// reported (upstream ignores them, like the -P errors, O7).
			statsErr = isrout.WriteStats(r.stdout, rep.Count(), rep.Stats())
		}
	}
	if err := out.close(); err != nil || closeErr != nil {
		return errorf(exitWrite, "write error on file "+out.name)
	}
	if statsErr != nil {
		return errorf(exitWrite, "write error on file <stdout>")
	}

	// --- write the pattern spectrum ---
	if o.pspFile != nil && !o.isRules() {
		return r.writeSpectrum(*o.pspFile, rep.Spectrum())
	}
	return nil
}

// borderSupp is setbdr's conversion of a -F value to an absolute support:
// ceil(-v) for v < 0 (saturating; upstream: undefined beyond int), else
// the minimum of the fraction v/100 of w under the threshold contract of
// package thresh: upstream's ceil(v/100*w*(1-DBL_EPSILON)) while
// v/100·w < 2^52, exact above (deviation N2 in docs/differences.md).
// An absolute integer literal bypasses it with its exact value (N3).
func borderSupp(v float64, w tract.Supp) tract.Supp {
	if v >= 0 { // also -0, which upstream treats as a percentage of 0
		return tract.Supp(thresh.New(v / 100.0).Min(int64(w)))
	}
	s := math.Ceil(-v)
	switch {
	case !(s > 0): // NaN
		return 0
	case s >= 0x1p63:
		return math.MaxInt64
	}
	return tract.Supp(s)
}

// readItems reads the item selection (sets) or appearance file (rules)
// into ib (ib_readsel, ib_readapp).
func (r *runner) readItems(ib *tract.ItemBase[string], file string, cfg tabread.Config) *cliError {
	name := displayName(file, "<stdin>")
	var src io.Reader
	if file == "" {
		var buf bytes.Buffer
		if err := r.copyCtx(&buf, r.stdin); err != nil {
			return r.copyError(err, name)
		}
		src = &buf
	} else {
		f, err := os.Open(file)
		if err != nil {
			return errorf(exitOpen, "cannot open file "+name)
		}
		defer f.Close()
		src = f
	}
	start := time.Now()
	r.logf("reading %s ... ", name)
	if r.o.isRules() {
		def, items, err := tabread.ReadAppearances(src, name, cfg)
		if err != nil {
			return r.inputError(err, name)
		}
		ib.SetDefaultAppearance(tract.Appearance(def))
		for _, it := range items {
			if err := ib.AddAppearance(it.Name, tract.Appearance(it.App)); err != nil {
				return r.inputError(err, name)
			}
		}
	} else {
		names, err := tabread.ReadSelection(src, name, cfg)
		if err != nil {
			return r.inputError(err, name)
		}
		if err := ib.Select(names...); err != nil {
			return r.inputError(err, name)
		}
	}
	r.logf("[%d item(s)]", ib.Len())
	r.done(start, "\n")
	return nil
}

// openInput opens the transaction file (upstream opens it before the
// "reading" message, so an open error has no message prefix) and returns
// a replayable scan over it, with byte-string item names (for
// tract.CountBytes and fpg.DataBytes). Standard input is spooled to a temporary
// file first, which is removed by cleanup.
func (r *runner) openInput(file, name string, cfg tabread.Config) (tract.ByteScanFunc, *cliError) {
	path := file
	var first *os.File
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, errorf(exitOpen, "cannot open file "+name)
		}
		first = f
		r.logf("reading %s ... ", name)
	} else {
		r.logf("reading %s ... ", name)
		p, cerr := r.spool(name)
		if cerr != nil {
			return nil, cerr
		}
		path = p
	}
	open := func() (io.ReadCloser, error) {
		if first != nil { // the file opened above serves the first pass
			f := first
			first = nil
			return f, nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	r.cleanups = append(r.cleanups, func() {
		if first != nil {
			first.Close()
		}
	})
	return ctxScan(r.ctx, tabread.ScanBytes(open, name, cfg)), nil
}

// spool copies standard input to a temporary file and returns its path.
func (r *runner) spool(name string) (string, *cliError) {
	f, err := os.CreateTemp(r.tempDir, "fpgrowth-stdin-*")
	if err != nil {
		// Deviation from upstream (which reads stdin into memory): the
		// spool file could not be created.
		return "", errorf(exitOpen, "cannot create temporary file for "+name)
	}
	path := f.Name()
	r.cleanups = append(r.cleanups, func() { _ = os.Remove(path) })
	err = r.copyCtx(f, r.stdin)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = &spoolWriteError{cerr}
	}
	if err != nil {
		return "", r.copyError(err, name)
	}
	return path, nil
}

// spoolWriteError marks a failure to write the spool file.
type spoolWriteError struct{ err error }

func (e *spoolWriteError) Error() string { return e.err.Error() }
func (e *spoolWriteError) Unwrap() error { return e.err }

// spoolWriter tags write errors of the destination of copyCtx.
type spoolWriter struct{ w io.Writer }

func (s spoolWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		err = &spoolWriteError{err}
	}
	return n, err
}

// copyCtx copies src to dst; it returns ctx.Err() as soon as ctx is
// canceled, even while a read from src blocks (the copy then continues
// in the background until the process exits).
func (r *runner) copyCtx(dst io.Writer, src io.Reader) error {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(spoolWriter{dst}, src)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

// copyError maps an error of copyCtx.
func (r *runner) copyError(err error, name string) *cliError {
	var we *spoolWriteError
	switch {
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.As(err, &we):
		return errorf(exitWrite, "write error on temporary file for "+name)
	default:
		return errorf(exitRead, "read error on file "+name)
	}
}

// aborted is a non-blocking check of ctx (SIGINT).
func (r *runner) aborted() error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	default:
		return nil
	}
}

// ctxScan stops a scan (a tract.ScanFunc or tract.ByteScanFunc) when ctx
// is canceled (checked every 1024 records).
func ctxScan[E any](ctx context.Context, scan func(yield func([]E, tract.Supp) bool) error) func(yield func([]E, tract.Supp) bool) error {
	done := ctx.Done()
	return func(yield func([]E, tract.Supp) bool) error {
		var cerr error
		cnt := 0
		err := scan(func(items []E, w tract.Supp) bool {
			cnt++
			if cnt&1023 == 0 {
				select {
				case <-done:
					cerr = ctx.Err()
					return false
				default:
				}
			}
			return yield(items, w)
		})
		if cerr != nil {
			return cerr
		}
		return err
	}
}

// inputError maps an error of reading the named input to upstream's
// message and exit status.
func (r *runner) inputError(err error, name string) *cliError {
	var se *tabread.SyntaxError
	switch {
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.As(err, &se):
		code := exitItemExp
		switch {
		case errors.Is(se.Err, tabread.ErrWeightExpected):
			code = exitWgtExp
		case errors.Is(se.Err, tabread.ErrInvalidWeight):
			code = exitTaWgt
		case errors.Is(se.Err, tabread.ErrDuplicateItem):
			code = exitDupItem
		case errors.Is(se.Err, tabread.ErrTooManyFields):
			code = exitFldCnt
		case errors.Is(se.Err, tabread.ErrAppearanceExpected):
			code = exitAppExp
		case errors.Is(se.Err, tabread.ErrUnknownAppearance):
			code = exitUnkApp
		}
		return errorf(code, se.Error())
	case errors.Is(err, tabread.ErrOpen):
		return errorf(exitOpen, "cannot open file "+name)
	case errors.Is(err, tabread.ErrRead):
		// Deviation from upstream (cli-io.md B8): the message names the
		// file, not the current field.
		return errorf(exitRead, "read error on file "+name)
	case errors.Is(err, tract.ErrNotReplayable):
		return errorf(exitRead, "read error on file "+name+" (input changed while reading)")
	case errors.Is(err, tract.ErrWeightOverflow):
		// Deviation from upstream (cli-io.md B11): upstream's int sum
		// overflows and the program crashes.
		return errorf(exitTaWgt, name+": total transaction weight too large")
	case errors.Is(err, tract.ErrTooLarge):
		return errorf(exitNoMem, "database too large")
	}
	return errorf(exitNoMem, err.Error())
}

// output is the item set output: an isrout.Writer on a file or stdout,
// with -z through a zlib writer, or nothing (count only). It implements
// report.Sink and remembers write errors, which upstream reports only
// when the file is closed.
type output struct {
	w        *isrout.Writer
	zw       *zlib.Writer   // -z: compresses into the file or stdout; else nil
	f        io.WriteCloser // nil for stdout and count only
	name     string
	isStdout bool
	failed   bool
}

func (o *output) ItemSet(s report.ItemSet) error {
	err := o.w.ItemSet(s)
	o.failed = o.failed || err != nil
	return err
}

func (o *output) Rule(rl report.Rule) error {
	err := o.w.Rule(rl)
	o.failed = o.failed || err != nil
	return err
}

// close flushes and closes the output (isr_close): the isrout.Writer,
// then the zlib stream (its final block and checksum), then the file. It
// returns the first error; the file is closed in any case. Later calls
// return nil.
func (o *output) close() error {
	if o.w == nil {
		return nil
	}
	err := o.w.Close()
	if o.zw != nil {
		// Close writes the final block and the Adler-32 checksum; after
		// a write error (the zlib writer's errors are sticky) it returns
		// that error again.
		if cerr := o.zw.Close(); err == nil {
			err = cerr
		}
	}
	if o.f != nil {
		if cerr := o.f.Close(); err == nil {
			err = cerr
		}
	}
	o.w, o.zw, o.f = nil, nil, nil
	return err
}

// openOutput opens the item set output (isr_open): no file means count
// only ("<null>"), an empty name stdout.
func (r *runner) openOutput(ib *tract.ItemBase[string], bag *tract.Bag, info string) (*output, *cliError) {
	out := &output{name: "<null>"}
	if r.o.outFile == nil {
		return out, nil
	}
	var dst io.Writer = r.stdout
	out.name = displayName(*r.o.outFile, "<stdout>")
	out.isStdout = *r.o.outFile == ""
	if !out.isStdout {
		create := r.create
		if create == nil {
			create = func(name string) (io.WriteCloser, error) { return os.Create(name) }
		}
		f, err := create(*r.o.outFile)
		if err != nil {
			return nil, errorf(exitOpen, "cannot open file "+out.name)
		}
		out.f, dst = f, f
	}
	if r.o.zlib {
		// upstream: deflateInit(Z_DEFAULT_COMPRESSION), the zlib format
		// of RFC 1950 (report.c isr_open); only the item set output
		out.zw = zlib.NewWriter(dst)
		dst = out.zw
	}
	names := make([]string, ib.Len())
	for i := range names {
		names[i] = ib.ID(tract.Item(i))
	}
	out.w = isrout.New(dst, isrout.Config{
		Names: names,
		Total: bag.TotalWeight(),
		Format: isrout.Format{
			Header: r.o.hdr,
			Sep:    r.o.sep,
			Imp:    r.o.imp,
			Info:   info,
			Scan:   r.o.scan,
		},
	})
	return out, nil
}

// runMiner runs fpg_mine with upstream's log messages. A write error of
// the output stops mining early (upstream mines to the end and reports
// the error when closing the file); the "writing" message then reports
// the number of sets written until then.
func (r *runner) runMiner(m *fpg.Miner[string], rep *report.Reporter, outName string) *cliError {
	start := time.Now()
	noun := "set"
	if r.o.isRules() {
		noun = "rule"
	}
	writing := func() {
		r.logf("[%d %s(s)]", rep.Count(), noun)
		r.done(start, "\n")
	}
	reported := false
	if m.UsesTree() {
		target := map[byte]string{'c': "closed", 'm': "maximal", 'g': "generator"}[r.o.target]
		m.SetProgress(func(p fpg.Phase, done bool) {
			if done {
				if p == fpg.PhaseReport {
					writing()
					reported = true
				} else {
					r.logf("done [%.2fs].\n", time.Since(start).Seconds())
				}
				return
			}
			start = time.Now()
			switch p {
			case fpg.PhaseTree:
				r.logf("finding frequent item set(s) ... ")
			case fpg.PhaseFilter:
				r.logf("filtering with evaluation ... ")
			case fpg.PhaseCloMax:
				r.logf("filtering for %s item sets ... ", target)
			case fpg.PhaseReport:
				r.logf("writing %s ... ", outName)
			}
		})
	} else {
		r.logf("writing %s ... ", outName)
	}
	err := m.Mine(r.ctx)
	switch {
	case err == nil && r.ctx.Err() != nil:
		// interrupted after the miner's last check: the output may be
		// incomplete
		return errorf(exitInterrupt, "interrupted")
	case err == nil:
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.Is(err, tract.ErrTooLarge):
		return errorf(exitNoMem, "database too large")
	default:
		// a write error of the output (the sink's only error)
		if !reported {
			writing()
		}
		return errorf(exitWrite, "write error on file "+outName)
	}
	if !m.UsesTree() {
		writing()
	}
	return nil
}

// writeSpectrum writes the pattern spectrum (-P) after the item sets.
func (r *runner) writeSpectrum(file string, psp *report.Spectrum) *cliError {
	name := displayName(file, "<stdout>")
	var dst io.Writer = r.stdout
	var f *os.File
	if file != "" {
		var err error
		if f, err = os.Create(file); err != nil {
			return errorf(exitOpen, "cannot open file "+name)
		}
		dst = f
	}
	start := time.Now()
	r.logf("writing %s ... ", name)
	err := isrout.WriteSpectrum(dst, psp)
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		// Deviation from upstream (O7): write errors are reported.
		return errorf(exitWrite, "write error on file "+name)
	}
	r.logf("[%d signature(s)]", psp.SigCount())
	r.done(start, "\n")
	return nil
}
