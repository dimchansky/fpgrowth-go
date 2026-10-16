package tract

// Replays the fixtures produced by reference/harness/tract (upstream
// tract.c driven like fpg_data) through ItemBase and Bag. The inputs are
// read with a minimal test-only port of upstream's table reader.

import (
	"errors"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Delimiter types and character classes of tabread.c.
const (
	trdEOF = -1
	trdFld = 0
	trdRec = 1

	clsRecSep  = 0x01
	clsFldSep  = 0x02
	clsBlank   = 0x04
	clsNull    = 0x08
	clsComment = 0x10

	trdMaxLen = 1024
)

// tabFormat holds the character class overrides fpgrowth passes to
// trd_allchs (-r, -f, -b, -C); unset classes keep their defaults.
type tabFormat struct {
	recseps, fldseps, blanks string
	setRec, setFld, setBlank bool
}

// tabFormats are the formats the harness uses (see run.sh).
var tabFormats = map[string]tabFormat{
	"default": {},
	"csv":     {blanks: ",", setBlank: true},
	"seps":    {fldseps: ",.;:", setFld: true},
	"para": {
		recseps: "%", setRec: true,
		fldseps: "\n", setFld: true,
		blanks: "\n", setBlank: true,
	},
}

// tabReader is a test-only port of TABREAD's trd_read for in-memory data.
type tabReader struct {
	data  []byte
	pos   int
	flags [256]uint8
	delim int
	field []byte
}

func newTabReader(data []byte, f tabFormat) *tabReader {
	r := &tabReader{data: data, delim: trdEOF}
	r.flags['\n'] = clsRecSep // trd_create defaults
	r.flags['\t'] = clsBlank | clsFldSep
	r.flags[' '] = clsBlank | clsFldSep
	r.flags['\r'] = clsBlank
	r.flags[','] = clsFldSep
	r.flags['?'] = clsNull
	r.flags['*'] = clsNull
	r.flags['#'] = clsComment
	if f.setRec {
		r.setChars(clsRecSep, f.recseps)
	}
	if f.setFld {
		r.setChars(clsFldSep, f.fldseps)
	}
	if f.setBlank {
		r.setChars(clsBlank, f.blanks)
	}
	r.setChars(clsNull, "") // fpgrowth passes "" as null characters
	return r
}

// setChars ports trd_chars: replace the members of a character class.
func (r *tabReader) setChars(class uint8, chars string) {
	for c := range r.flags {
		r.flags[c] &^= class
	}
	for i := range len(chars) {
		r.flags[chars[i]] |= class
	}
}

func (r *tabReader) is(c int, class uint8) bool { return r.flags[byte(c)]&class != 0 }

func (r *tabReader) getc() int {
	if r.pos >= len(r.data) {
		return -1
	}
	c := int(r.data[r.pos])
	r.pos++
	return c
}

// read ports trd_read: it reads the next field into r.field and returns
// the delimiter type that ended it.
func (r *tabReader) read() int {
	r.field = r.field[:0]
	c := r.getc()
	if c < 0 {
		r.delim = trdEOF
		return r.delim
	}
	if r.delim != trdFld { // skip comment records
		for r.is(c, clsComment) {
			for !r.is(c, clsRecSep) {
				if c = r.getc(); c < 0 {
					r.delim = trdEOF
					return r.delim
				}
			}
			if c = r.getc(); c < 0 {
				r.delim = trdEOF
				return r.delim
			}
		}
	}
	for r.is(c, clsBlank) { // skip leading blanks
		if c = r.getc(); c < 0 {
			r.delim = trdRec
			return r.delim
		}
	}
	if r.is(c, clsFldSep|clsRecSep) {
		r.delim = trdRec
		if r.is(c, clsFldSep) {
			r.delim = trdFld
		}
		return r.delim
	}
	d := trdRec
	for { // read the field
		if len(r.field) < trdMaxLen {
			r.field = append(r.field, byte(c))
		}
		if c = r.getc(); c < 0 {
			break
		}
		if r.is(c, clsFldSep|clsRecSep) {
			if r.is(c, clsFldSep) {
				d = trdFld
			}
			break
		}
	}
	for len(r.field) > 0 && r.is(int(r.field[len(r.field)-1]), clsBlank) {
		r.field = r.field[:len(r.field)-1] // remove trailing blanks
	}
	if d != trdFld {
		r.delim = d
		return d
	}
	for r.is(c, clsBlank) { // skip trailing blanks
		if c = r.getc(); c < 0 {
			r.delim = trdRec
			return r.delim
		}
	}
	if r.is(c, clsRecSep) {
		r.delim = trdRec
		return r.delim
	}
	if !r.is(c, clsFldSep) {
		r.pos-- // trd_ungetc
	}
	r.delim = trdFld
	return r.delim
}

// testRecord is one record as ib_read sees it (before deduplication).
type testRecord struct {
	items []string
	w     Supp
}

// readRecords ports the field loop of ib_read (without the item base).
func readRecords(data []byte, f tabFormat, weights bool) ([]testRecord, error) {
	r := newTabReader(data, f)
	recs := []testRecord{}
	for {
		rec := testRecord{w: 1}
		for {
			d := r.read()
			if d <= trdEOF {
				return recs, nil // a partial record is discarded
			}
			b := string(r.field)
			if d == trdRec && weights {
				w, err := parseWeight(b)
				if err != nil {
					return nil, err
				}
				rec.w = w
				break
			}
			if b == "" {
				if d == trdRec {
					break
				}
				return nil, errors.New("item expected")
			}
			rec.items = append(rec.items, b)
			if d != trdFld {
				break
			}
		}
		recs = append(recs, rec)
	}
}

// parseWeight accepts the weights the fixtures use: decimal integers
// (upstream also accepts strtol's base prefixes and brackets).
func parseWeight(s string) (Supp, error) {
	if s == "" {
		return 0, errors.New("transaction weight expected")
	}
	w, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid transaction weight %q", s)
	}
	return Supp(w), nil
}

// readSelection ports the parsing part of ib_readsel.
func readSelection(data []byte, f tabFormat) ([]string, error) {
	r := newTabReader(data, f)
	ids := []string{}
	for r.read() > trdEOF {
		if len(r.field) == 0 {
			return nil, errors.New("item expected")
		}
		ids = append(ids, string(r.field))
	}
	return ids, nil
}

// applyAppearances ports ib_readapp on top of the appearance methods.
func applyAppearances(b *ItemBase[string], data []byte, f tabFormat) error {
	r := newTabReader(data, f)
	if r.read() != trdRec {
		return errors.New("too many fields")
	}
	app, ok := ParseAppearance(string(r.field))
	if !ok {
		return fmt.Errorf("unknown appearance indicator %q", r.field)
	}
	b.SetDefaultAppearance(app)
	for {
		d := r.read()
		if d <= trdEOF {
			return nil
		}
		name := string(r.field)
		if name == "" || d != trdFld {
			return errors.New("item/appearance pair expected")
		}
		if r.read() == trdFld {
			return errors.New("too many fields")
		}
		if app, ok = ParseAppearance(string(r.field)); !ok {
			return fmt.Errorf("unknown appearance indicator %q", r.field)
		}
		if err := b.AddAppearance(name, app); err != nil {
			return err
		}
	}
}

// recordScan returns a ScanFunc over recs.
func recordScan(recs []testRecord) ScanFunc[string] {
	return func(yield func([]string, Supp) bool) error {
		for _, r := range recs {
			if !yield(r.items, r.w) {
				return nil
			}
		}
		return nil
	}
}

// fixtureCase is one "case" block of an expected file.
type fixtureCase struct {
	header string
	params map[string]string
	input  string
	want   string
}

func loadFixtures(t *testing.T) map[string][]fixtureCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "expected", "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found (err %v)", err)
	}
	sets := map[string][]fixtureCase{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		set := strings.TrimSuffix(filepath.Base(file), ".txt")
		var cur *fixtureCase
		var body strings.Builder
		flush := func() {
			if cur != nil {
				cur.want = body.String()
				sets[set] = append(sets[set], *cur)
			}
			body.Reset()
		}
		for line := range strings.Lines(string(data)) {
			header, ok := strings.CutPrefix(line, "case ")
			if !ok {
				if cur == nil {
					t.Fatalf("%s: data before the first case", file)
				}
				body.WriteString(line)
				continue
			}
			flush()
			fields := strings.Fields(header)
			cur = &fixtureCase{header: strings.TrimSpace(header), params: map[string]string{}, input: fields[0]}
			for _, kv := range fields[1:] {
				k, v, _ := strings.Cut(kv, "=")
				cur.params[k] = v
			}
		}
		flush()
	}
	return sets
}

func TestUpstreamFixtures(t *testing.T) {
	t.Parallel()
	sets := loadFixtures(t)
	total := 0
	for set, cases := range sets {
		for i, fc := range cases {
			total++
			t.Run(fmt.Sprintf("%s/%02d", set, i), func(t *testing.T) {
				t.Parallel()
				got, err := replayCase(fc)
				if err != nil {
					t.Fatalf("case %s: %v", fc.header, err)
				}
				if got != fc.want {
					t.Errorf("case %s:\n%s", fc.header, firstDiff(got, fc.want))
				}
			})
		}
	}
	t.Logf("%d fixture sets, %d cases", len(sets), total)
}

// replayCase runs fpg_data's preparation for one case and renders it in
// the harness's output format.
func replayCase(fc fixtureCase) (string, error) {
	p := fc.params
	num := func(key string) int {
		v, err := strconv.Atoi(p[key])
		if err != nil {
			panic(fmt.Sprintf("bad parameter %s=%q", key, p[key]))
		}
		return v
	}
	f, ok := tabFormats[p["fmt"]]
	if !ok {
		return "", fmt.Errorf("unknown format %q", p["fmt"])
	}
	data, err := os.ReadFile(filepath.Join("testdata", "inputs", fc.input))
	if err != nil {
		return "", err
	}
	b := NewItemBase[string]()
	if sel := p["sel"]; sel != "-" {
		sdata, err := os.ReadFile(filepath.Join("testdata", "inputs", sel))
		if err != nil {
			return "", err
		}
		switch filepath.Ext(sel) {
		case ".sel":
			ids, err := readSelection(sdata, f)
			if err != nil {
				return "", err
			}
			if err := b.Select(ids...); err != nil {
				return "", err
			}
		case ".app":
			if err := applyAppearances(b, sdata, f); err != nil {
				return "", err
			}
		}
	}
	recs, err := readRecords(data, f, p["w"] == "1")
	if err != nil {
		return "", err
	}
	scan := recordScan(recs)
	if err := b.Count(scan); err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "read items %d records %d wgt %d maxfrq %d defapp %d\n",
		b.Len(), b.Records(), b.TotalWeight(), b.MaxFrequency(), b.DefaultAppearance())
	writeItems(&sb, b, "item")
	order := num("q")
	if p["reorder"] == "1" { // FPG_REORDER: sort = sign(sort)
		order = max(min(order, 1), -1)
	}
	m := b.Recode(Supp(num("smin")), order)
	fmt.Fprintf(&sb, "recode %d\n", m)
	writeItems(&sb, b, "code")
	bag, err := b.BuildBag(scan, num("zmin"))
	if err != nil {
		return "", err
	}
	if err := checkIterators(bag); err != nil {
		return "", err
	}
	fmt.Fprintf(&sb, "bag cnt %d records %d wgt %d max %d extent %d\n",
		bag.Len(), bag.Records(), bag.TotalWeight(), bag.MaxSize(), bag.Extent())
	writeBag(&sb, bag, "ta")
	if pack := num("pack"); pack > 0 {
		bag.Pack(pack)
		fmt.Fprintf(&sb, "pack %d\n", pack)
		writeBag(&sb, bag, "pta")
		if err := checkIterators(bag); err != nil {
			return "", err
		}
	}
	sb.WriteString("ifrqs")
	for _, f := range bag.Frequencies() {
		fmt.Fprintf(&sb, " %d", f)
	}
	sb.WriteString("\nend\n")
	return sb.String(), nil
}

func writeItems(sb *strings.Builder, b *ItemBase[string], tag string) {
	for i := range Item(b.Len()) {
		fmt.Fprintf(sb, "%s %d %d %d %d %s\n", tag, i, b.Appearance(i),
			b.Frequency(i), b.ExtFrequency(i), b.ID(i))
	}
}

// writeBag prints the transactions like the harness: weight, upstream
// size (packing keeps the number of items) and the entries.
func writeBag(sb *strings.Builder, bag *Bag, tag string) {
	for t, w := range bag.All() {
		size := len(t)
		if size > 0 && t[0].IsPacked() {
			size += bits.OnesCount32(t[0].PackedBits()) - 1
		}
		fmt.Fprintf(sb, "%s %d %d", tag, w, size)
		for _, i := range t {
			fmt.Fprintf(sb, " %d", i)
		}
		sb.WriteByte('\n')
	}
}

// checkIterators verifies Len and that Backward is the reverse of All.
func checkIterators(bag *Bag) error {
	fwd := collect(bag.All())
	bwd := collect(bag.Backward())
	if len(fwd) != bag.Len() {
		return fmt.Errorf("All yields %d transactions, Len is %d", len(fwd), bag.Len())
	}
	slices.Reverse(bwd)
	if !slices.EqualFunc(fwd, bwd, func(a, b weighted) bool {
		return a.w == b.w && slices.Equal(a.items, b.items)
	}) {
		return errors.New("Backward is not the reverse of All")
	}
	return nil
}

// firstDiff describes the first differing line of got and want.
func firstDiff(got, want string) string {
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	for i := range max(len(g), len(w)) {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d:\n got: %q\nwant: %q", i+1, gl, wl)
		}
	}
	return "outputs differ"
}
