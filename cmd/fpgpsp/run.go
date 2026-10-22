package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/isrout"
	"github.com/dimchansky/fpgrowth-go/internal/psp"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// env is the environment of one run of the program.
type env struct {
	args   []string // program name and arguments (os.Args)
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// tempDir is the directory for the spool file of standard input;
	// empty means os.TempDir().
	tempDir string
	// now returns the current time for the default seed; nil means
	// time.Now.
	now func() time.Time
	// numCPU returns the number of CPUs for -Z <= 0; nil means
	// runtime.NumCPU.
	numCPU func() int
}

// run executes the program like upstream's main and returns the exit
// status. Diagnostics go to e.stderr; the pattern spectrum goes to the
// output file or e.stdout, the decision border to e.stdout.
// Cancellation of ctx (SIGINT) stops reading, generation and mining.
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
		cerr = r.main()
		r.cleanup()
	}
	if cerr != nil {
		fmt.Fprintf(e.stderr, "\n%s: %s\n", prog, cerr.msg)
		return cerr.code
	}
	return exitOK
}

// runner holds the state of one run.
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

// logf writes a progress message to stderr (upstream MSG).
func (r *runner) logf(format string, args ...any) {
	fmt.Fprintf(r.stderr, format, args...)
}

// done ends a progress message with the elapsed time.
func (r *runner) done(start time.Time, end string) {
	r.logf(" done [%.2fs].%s", time.Since(start).Seconds(), end)
}

// main is the pipeline of upstream's main after option validation.
func (r *runner) main() *cliError {
	o := r.o
	cfg := r.tabConfig()
	ib := tract.NewItemBase[string]()

	// --- read the item selection ---
	if o.selFile != nil {
		if err := r.readItems(ib, *o.selFile, cfg); err != nil {
			return err
		}
	}

	// --- read the transaction database ---
	inName := displayName(*o.inFile, "<stdin>")
	scan, cerr := r.openInput(*o.inFile, inName, cfg)
	if cerr != nil {
		return cerr
	}
	start := time.Now()
	if err := ib.Count(scan); err != nil {
		return r.inputError(err, inName)
	}
	n, w := ib.Records(), ib.TotalWeight()
	r.logf("[%d item(s), %s transaction(s)]", ib.Len(), transactions(n, w))
	r.done(start, "")
	if ib.Len() <= 0 || n <= 0 {
		return errorf(exitNoItems, "no (frequent) items found")
	}
	ib.SetAbort(r.aborted) // completing a large bag takes a while
	method, _ := methodOf(o.surr)
	var recs [][]tract.Item // Shuffle: the unrecoded records
	if o.surr == 's' {
		var err error
		if recs, err = psp.Records(ib, scan); err != nil {
			return r.inputError(err, inName)
		}
		if !psp.IsTable(recs, ib.Len()) {
			return errorf(exitNoTable, "transactions are not table-derived")
		}
	}
	r.logf("\n")

	// --- estimate or generate the pattern spectrum ---
	cnt := o.cnt
	if o.surr == 'i' {
		cnt = 1 // identity: one data set
	}
	seed := o.seed
	seedDefault := !o.seedOK || seed == 0
	if seedDefault {
		now := time.Now
		if r.now != nil {
			now = r.now
		}
		seed = now().Unix() // upstream: time(NULL)
	}
	var spec *report.Spectrum
	if o.surr == 'e' {
		if seedDefault && o.alpha > 0 { // the simple estimation is not random
			r.logf("using -S%d\n", seed)
		}
		start = time.Now()
		r.logf("estimating pattern spectrum ... ")
		var cerr *cliError
		if spec, cerr = r.estimate(ib, scan, seed); cerr != nil {
			return cerr
		}
	} else {
		cpus := int(o.cpus)
		cpuDefault := cpus <= 0
		if cpuDefault {
			cpus = runtime.NumCPU()
			if r.numCPU != nil {
				cpus = r.numCPU()
			}
		}
		parts := psp.Partition(seed, cnt, cpus)
		if method != psp.Identity && (seedDefault || cpuDefault && cnt > 1) {
			r.logf("using -S%d -Z%d (%d chain(s))\n", seed, cpus, len(parts))
		}
		start = time.Now()
		r.logf("generating pattern spectrum ... ")
		var cerr *cliError
		if spec, cerr = r.generate(ib, scan, recs, method, parts); cerr != nil {
			return cerr
		}
	}
	r.logf("[%d signature(s)]", spec.SigCount())
	r.done(start, "\n")

	// --- write the pattern spectrum and the decision border ---
	if cerr := r.writeSpectrum(spec, 1.0/float64(cnt)); cerr != nil {
		return cerr
	}
	if o.border {
		if _, err := io.WriteString(r.stdout, psp.FormatBorder(psp.Border(spec, int(o.zmin)))); err != nil {
			return errorf(exitWrite, "write error on file <stdout>")
		}
	}
	return nil
}

// estimate is fpg_estpsp: prepare the data (with the complex variant and
// the default mode, whatever the options say) and estimate the spectrum.
func (r *runner) estimate(ib *tract.ItemBase[string], scan tract.ScanFunc[string], seed int64) (*report.Spectrum, *cliError) {
	o := r.o
	m, d, err := psp.Prepare(ib, scan, o.estimConfig())
	if err != nil {
		return nil, r.prepError(err)
	}
	spec := report.NewSpectrum(int(o.zmin), int(o.zmax), m.MinSupport(), tract.Supp(d.Len()))
	err = psp.Estimate(r.ctx, d, spec, int(o.zmin), psp.EstimateConfig{
		Equiv:   uint64(o.cnt),
		Alpha:   o.alpha,
		Samples: uint64(o.smpls),
		// Deviation from upstream (U7): only 0 means "time"; upstream's
		// fpg_estpsp also replaces negative seeds by the time.
		Seed: uint32(seed),
	})
	if err != nil || r.ctx.Err() != nil {
		return nil, errorf(exitInterrupt, "interrupted")
	}
	return spec, nil
}

// generate is fpg_genpsp: prepare the data, then generate and mine the
// surrogate data sets of every part (one goroutine per part, like
// upstream's threads) and sum their spectra in part order.
func (r *runner) generate(ib *tract.ItemBase[string], scan tract.ScanFunc[string],
	recs [][]tract.Item, method psp.Method, parts []psp.Part) (*report.Spectrum, *cliError) {
	cfg := r.o.minerConfig()
	var d *psp.Data
	var smin, smax tract.Supp
	if method == psp.Shuffle {
		zmin := cfg.ZMin
		if cfg.Target == report.TargetGenerators {
			zmin = 0 // no -m filter for generators (U25, see psp.PrepConfig)
		}
		d = psp.TableData(recs, ib.Len(), zmin)
		smin, smax = fpg.Thresholds(cfg, ib.TotalWeight())
	} else {
		m, data, err := psp.Prepare(ib, scan, cfg)
		if err != nil {
			return nil, r.prepError(err)
		}
		d, smin, smax = data, m.MinSupport(), m.MaxSupport()
	}

	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	var mu sync.Mutex
	var finished, shown int64
	progress := func() { // upstream's repfn: every 20 data sets
		mu.Lock()
		defer mu.Unlock()
		finished++
		if finished > shown && finished%20 == 0 {
			shown = finished
			r.logf("%10d\b\b\b\b\b\b\b\b\b\b", finished)
		}
	}
	specs := make([]*report.Spectrum, len(parts))
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	for i, p := range parts {
		wg.Go(func() {
			specs[i], errs[i] = psp.RunChain(ctx, d, method, p, cfg, smin, smax, progress)
			if errs[i] != nil {
				cancel() // stop the other chains
			}
		})
	}
	wg.Wait()
	if err := r.ctx.Err(); err != nil {
		return nil, errorf(exitInterrupt, "interrupted")
	}
	for _, err := range errs {
		if err != nil {
			return nil, r.prepError(err)
		}
	}
	spec := specs[0]
	for _, s := range specs[1:] {
		spec.Merge(s)
	}
	return spec, nil
}

// prepError maps an error of preparing or mining the data.
func (r *runner) prepError(err error) *cliError {
	switch {
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.Is(err, fpg.ErrNoItems):
		return errorf(exitNoItems, "no (frequent) items found")
	case errors.Is(err, fpg.ErrTooLarge), errors.Is(err, tract.ErrTooLarge), errors.Is(err, psp.ErrTooLarge):
		return errorf(exitNoMem, "database too large")
	}
	return r.inputError(err, displayName(*r.o.inFile, "<stdin>"))
}

// writeSpectrum writes the pattern spectrum to the output file (stdout if
// none is given) with every count scaled by scale (psp_report).
func (r *runner) writeSpectrum(spec *report.Spectrum, scale float64) *cliError {
	file := ""
	if r.o.outFile != nil {
		file = *r.o.outFile
	}
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
	err := isrout.WriteSpectrumScaled(dst, spec, scale)
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return errorf(exitWrite, "write error on file "+name)
	}
	r.logf("[%d signature(s)]", spec.SigCount())
	r.done(start, "\n")
	return nil
}

// transactions formats "<n>[/<w>]" like upstream's logs.
func transactions(n int, w tract.Supp) string {
	if w != tract.Supp(n) {
		return fmt.Sprintf("%d/%d", n, w)
	}
	return fmt.Sprintf("%d", n)
}

// tabConfig returns the input format of the options (no weights: fpgpsp
// reads transactions with tbg_read(..., 0)).
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
	return cfg
}

// displayName is upstream's trd_name/twr_name: "<stdin>" or "<stdout>"
// for an empty name.
func displayName(name, std string) string {
	if name == "" {
		return std
	}
	return name
}

// readItems reads the item selection file (ib_readsel).
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
	names, err := tabread.ReadSelection(src, name, cfg)
	if err != nil {
		return r.inputError(err, name)
	}
	if err := ib.Select(names...); err != nil {
		return r.inputError(err, name)
	}
	r.logf("[%d item(s)]", ib.Len())
	r.done(start, "\n")
	return nil
}
