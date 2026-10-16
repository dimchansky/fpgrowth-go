package report

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// This file implements the reporter script language of the C harness
// reference/harness/report/isrreplay.c for the Go reporter: runScript
// executes a script and produces the same text as the harness.

// scriptSink prints item sets and rules like the harness callbacks (emit
// mode) or collects lines in the harness's file format (file mode).
type scriptSink struct {
	out   *strings.Builder
	file  bool
	lines []string
}

func (s *scriptSink) ItemSet(set ItemSet) error {
	if s.file {
		var b strings.Builder
		for i, it := range set.Items {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.Itoa(int(it)))
		}
		fmt.Fprintf(&b, " (%d)", set.Supp)
		s.lines = append(s.lines, b.String())
		return nil
	}
	s.out.WriteString("set")
	for _, it := range set.Items {
		fmt.Fprintf(s.out, " %d", it)
	}
	fmt.Fprintf(s.out, " : %d w=%s e=%s\n", set.Supp, fmtFloat(set.Weight), fmtFloat(set.Eval))
	return nil
}

func (s *scriptSink) Rule(r Rule) error {
	if s.file {
		var b strings.Builder
		b.WriteString(strconv.Itoa(int(r.Head)))
		b.WriteString(" <- ")
		for i, it := range r.Body {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.Itoa(int(it)))
		}
		fmt.Fprintf(&b, " (%d)", r.Supp)
		s.lines = append(s.lines, b.String())
		return nil
	}
	fmt.Fprintf(s.out, "rule %d <-", r.Head)
	for _, it := range r.Body {
		fmt.Fprintf(s.out, " %d", it)
	}
	fmt.Fprintf(s.out, " : %d %d %d e=%s\n", r.Supp, r.BodySupp, r.HeadSupp, fmtFloat(r.Eval))
	return nil
}

// fmtFloat formats like C's printf("%.17g").
func fmtFloat(x float64) string { return strconv.FormatFloat(x, 'g', 17, 64) }

// scriptRunner holds the state of one script execution.
type scriptRunner struct {
	out    strings.Builder
	rep    *Reporter
	nItems int
	total  tract.Supp
	freqs  []tract.Supp
	sink   *scriptSink
	line   int
}

// runScript executes a reporter script and returns the harness output.
func runScript(src string) (string, error) {
	sr := &scriptRunner{nItems: -1}
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		sr.line++
		text := sc.Text()
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = text[:i]
		}
		tok := strings.Fields(text)
		if len(tok) == 0 {
			continue
		}
		if err := sr.command(tok); err != nil {
			return sr.out.String(), fmt.Errorf("line %d: %w", sr.line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return sr.out.String(), err
	}
	if err := sr.finish(); err != nil {
		return sr.out.String(), err
	}
	return sr.out.String(), nil
}

func (sr *scriptRunner) needRep() error {
	if sr.rep != nil {
		return nil
	}
	if sr.nItems < 0 {
		return errors.New("'items' must come first")
	}
	sr.rep = New(sr.nItems, sr.total, sr.freqs)
	return nil
}

func argInt(tok []string, i int) (int64, error) {
	if i >= len(tok) {
		return 0, fmt.Errorf("%s: missing argument", tok[0])
	}
	return strconv.ParseInt(tok[i], 10, 64)
}

func argFloat(tok []string, i int) (float64, error) {
	if i >= len(tok) {
		return 0, fmt.Errorf("%s: missing argument", tok[0])
	}
	return strconv.ParseFloat(tok[i], 64)
}

// argInts parses tok[from:] as integers.
func argInts(tok []string, from int) ([]int64, error) {
	vals := make([]int64, 0, len(tok))
	for i := from; i < len(tok); i++ {
		v, err := argInt(tok, i)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, nil
}

//nolint:gocyclo // a flat command dispatcher mirroring the C harness
func (sr *scriptRunner) command(tok []string) error {
	cmd := tok[0]
	switch cmd {
	case "items", "total", "freqs":
		return sr.baseCommand(tok)
	case "target", "supp", "size", "border", "eval", "psp", "output", "setup":
		if err := sr.needRep(); err != nil {
			return err
		}
		return sr.configCommand(tok)
	}
	if sr.rep == nil {
		return fmt.Errorf("%s before reporter creation", cmd)
	}
	return sr.miningCommand(tok)
}

func (sr *scriptRunner) baseCommand(tok []string) error {
	if sr.rep != nil {
		return fmt.Errorf("%s after reporter creation", tok[0])
	}
	vals, err := argInts(tok, 1)
	if err != nil {
		return err
	}
	switch tok[0] {
	case "items":
		if len(vals) != 1 {
			return errors.New("items: one argument expected")
		}
		sr.nItems = int(vals[0])
	case "total":
		if len(vals) != 1 {
			return errors.New("total: one argument expected")
		}
		sr.total = tract.Supp(vals[0])
	case "freqs":
		if len(vals) != sr.nItems {
			return errors.New("wrong number of frequencies")
		}
		sr.freqs = make([]tract.Supp, len(vals))
		for i, v := range vals {
			sr.freqs[i] = tract.Supp(v)
		}
	}
	return nil
}

func parseTarget(tok []string) (Target, Mode, int, error) {
	if len(tok) < 2 {
		return 0, 0, 0, errors.New("missing target")
	}
	targets := map[string]Target{
		"all":        TargetAll,
		"closed":     TargetClosed,
		"maximal":    TargetMaximal,
		"generators": TargetGenerators,
		"rules":      TargetRules,
	}
	t, ok := targets[tok[1]]
	if !ok {
		return 0, 0, 0, fmt.Errorf("unknown target %q", tok[1])
	}
	var m Mode
	dir := -1
	for i := 2; i < len(tok); i++ {
		switch tok[i] {
		case "nofilter":
			m |= ModeNoFilter
		case "noexpand":
			m |= ModeNoExpand
		case "sort":
			m |= ModeSort
		case "dir":
			i++
			d, err := argInt(tok, i)
			if err != nil {
				return 0, 0, 0, err
			}
			dir = int(d)
		default:
			return 0, 0, 0, fmt.Errorf("unknown mode %q", tok[i])
		}
	}
	return t, m, dir, nil
}

// argLimit parses tok[i], where "max" stands for limit.
func argLimit(tok []string, i int, limit int64) (int64, error) {
	if i < len(tok) && tok[i] == "max" {
		return limit, nil
	}
	return argInt(tok, i)
}

func (sr *scriptRunner) configCommand(tok []string) error {
	r := sr.rep
	switch tok[0] {
	case "target":
		t, m, dir, err := parseTarget(tok)
		if err != nil {
			return err
		}
		r.SetTarget(t, m, dir)
	case "supp":
		smin, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		smax, err := argLimit(tok, 2, int64(MaxSupp))
		if err != nil {
			return err
		}
		r.SetSupport(tract.Supp(smin), tract.Supp(smax))
	case "size":
		zmin, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		zmax, err := argLimit(tok, 2, MaxSize)
		if err != nil {
			return err
		}
		r.SetSize(int(zmin), int(zmax))
	case "border":
		vals, err := argInts(tok, 1)
		if err != nil || len(vals) != 2 {
			return fmt.Errorf("border: two integers expected (%v)", err)
		}
		r.SetBorder(int(vals[0]), tract.Supp(vals[1]))
	case "eval":
		if len(tok) < 2 || tok[1] != "logrto" {
			return errors.New("unknown evaluation")
		}
		dir, err := argInt(tok, 2)
		if err != nil {
			return err
		}
		thresh, err := argFloat(tok, 3)
		if err != nil {
			return err
		}
		r.SetEval(LogRatio, int(dir), thresh)
	case "psp":
		r.AddSpectrum()
	case "output":
		return sr.outputCommand(tok)
	case "setup":
		r.Setup()
	}
	return nil
}

func (sr *scriptRunner) outputCommand(tok []string) error {
	if len(tok) < 2 {
		return errors.New("missing output mode")
	}
	switch tok[1] {
	case "count":
		sr.sink = nil
		sr.rep.SetSink(nil)
	case "emit":
		sr.sink = &scriptSink{out: &sr.out}
		sr.rep.SetSink(sr.sink)
	case "file":
		sr.sink = &scriptSink{out: &sr.out, file: true}
		sr.rep.SetSink(sr.sink)
	default:
		return fmt.Errorf("unknown output mode %q", tok[1])
	}
	return nil
}

func (sr *scriptRunner) miningCommand(tok []string) error {
	r := sr.rep
	switch tok[0] {
	case "add":
		vals, err := argInts(tok, 1)
		if err != nil || len(vals) != 2 {
			return fmt.Errorf("add: two integers expected (%v)", err)
		}
		res := 0
		if r.Add(tract.Item(vals[0]), tract.Supp(vals[1])) {
			res = 1
		}
		fmt.Fprintf(&sr.out, "add %s %s -> %d\n", tok[1], tok[2], res)
	case "addpex":
		v, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		res := -1
		if r.AddPex(tract.Item(v)) {
			res = 0
		}
		fmt.Fprintf(&sr.out, "addpex %s -> %d\n", tok[1], res)
	case "addpexpk":
		v, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		r.AddPexPacked(tract.Item(v) | PackedFlag)
	case "remove":
		v, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		r.Remove(int(v))
	case "report":
		return r.Report()
	case "reportv":
		v, err := argFloat(tok, 1)
		if err != nil {
			return err
		}
		return r.ReportEval(v)
	case "reprule":
		return sr.repruleCommand(tok)
	default:
		return sr.queryCommand(tok)
	}
	return nil
}

func (sr *scriptRunner) repruleCommand(tok []string) error {
	vals, err := argInts(tok[:4], 1)
	if err != nil || len(vals) != 3 {
		return fmt.Errorf("reprule: three integers expected (%v)", err)
	}
	e, err := argFloat(tok, 4)
	if err != nil {
		return err
	}
	return sr.rep.ReportRule(tract.Item(vals[0]), tract.Supp(vals[1]), tract.Supp(vals[2]), e)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (sr *scriptRunner) queryCommand(tok []string) error {
	r := sr.rep
	switch tok[0] {
	case "xable":
		v, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		fmt.Fprintf(&sr.out, "xable %s -> %d\n", tok[1], b2i(r.Xable(int(v))))
	case "uses":
		v, err := argInt(tok, 1)
		if err != nil {
			return err
		}
		fmt.Fprintf(&sr.out, "uses %s -> %d\n", tok[1], b2i(r.Uses(tract.Item(v))))
	case "tail":
		vals, err := argInts(tok, 1)
		if err != nil {
			return err
		}
		items := make([]tract.Item, len(vals))
		for i, v := range vals {
			items[i] = tract.Item(v)
		}
		fmt.Fprintf(&sr.out, "%s -> %d\n", strings.Join(tok, " "), b2i(r.Tail(items)))
	case "cnt":
		fmt.Fprintf(&sr.out, "cnt -> %d\n", r.Len())
	case "getsupp":
		fmt.Fprintf(&sr.out, "getsupp -> %d\n", r.Supp())
	case "pexcnt":
		fmt.Fprintf(&sr.out, "pexcnt -> %d\n", r.PexCount())
	default:
		return fmt.Errorf("unknown command %q", tok[0])
	}
	return nil
}

func (sr *scriptRunner) finish() error {
	if err := sr.needRep(); err != nil {
		return err
	}
	if sr.sink != nil && sr.sink.file {
		for _, l := range sr.sink.lines {
			fmt.Fprintf(&sr.out, "line %s\n", l)
		}
	}
	fmt.Fprintf(&sr.out, "all: %d\n", sr.rep.Count())
	for size, c := range sr.rep.Stats() {
		fmt.Fprintf(&sr.out, "%3d: %d\n", size, c)
	}
	if psp := sr.rep.Spectrum(); psp != nil {
		for c := range psp.All() {
			fmt.Fprintf(&sr.out, "psp %d %d %d\n", c.Size, c.Supp, c.Count)
		}
		fmt.Fprintf(&sr.out, "psp sigcnt=%d total=%d\n", psp.SigCount(), psp.Total())
	}
	return nil
}

// floatTolerance is the relative tolerance for e= values in golden
// comparisons: math.Log and the C library's log may differ in the last
// unit in the last place (docs/upstream/report.md §8 risk 2).
const floatTolerance = 1e-12

// sameOutputLine reports whether two output lines are equal, allowing the
// e= values of set and rule lines to differ by floatTolerance.
func sameOutputLine(a, b string) bool {
	if a == b {
		return true
	}
	ia, ib := strings.LastIndex(a, " e="), strings.LastIndex(b, " e=")
	if ia < 0 || ib < 0 || a[:ia] != b[:ib] {
		return false
	}
	x, err1 := strconv.ParseFloat(a[ia+3:], 64)
	y, err2 := strconv.ParseFloat(b[ib+3:], 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return math.Abs(x-y) <= floatTolerance*math.Max(1, math.Max(math.Abs(x), math.Abs(y)))
}
