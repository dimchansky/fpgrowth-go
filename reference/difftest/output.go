package difftest

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// infoField is one code of an information format (-v), e.g. %17e.
type infoField struct {
	code   byte
	digits int // significant digits; 0 means upstream's default (6)
}

// infoFormat is a parseable -v value: open + fields joined by "," + close,
// e.g. " (%a,%17e)". Item names never contain the delimiters, and numbers
// never contain ','.
type infoFormat struct {
	open, close string
	fields      []infoField
}

// String returns the -v argument.
func (f infoFormat) String() string {
	var b strings.Builder
	b.WriteString(f.open)
	for i, fl := range f.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('%')
		if fl.digits > 0 {
			b.WriteString(strconv.Itoa(fl.digits))
		}
		b.WriteByte(fl.code)
	}
	b.WriteString(f.close)
	return b.String()
}

// isInt reports whether a code prints an integer (absolute supports, the
// set size and the total weight).
func isInt(code byte) bool {
	switch code {
	case 'a', 'b', 'h', 'i', 'Q':
		return true
	}
	return false
}

// fieldIndex returns the position of the first field with one of the
// codes, or -1.
func (f infoFormat) fieldIndex(codes string) int {
	for i, fl := range f.fields {
		if strings.IndexByte(codes, fl.code) >= 0 {
			return i
		}
	}
	return -1
}

// record is one parsed output line.
type record struct {
	key    string   // canonical key (setKey or ruleKey)
	items  []string // item set, or rule body, sorted
	head   string   // rule head ("" for item sets)
	values []float64
	line   string
}

// size is the number of items of an item set or a rule.
func (r *record) size() int {
	if r.head != "" {
		return len(r.items) + 1
	}
	return len(r.items)
}

// parseOutput parses an item set or rule file written with the format f,
// item separator sep and (for rules) implication sign imp.
func parseOutput(data []byte, f infoFormat, rules bool, sep, imp string) ([]record, error) {
	var recs []record
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		rec, err := parseLine(line, f, rules, sep, imp)
		if err != nil {
			return nil, fmt.Errorf("line %d %q: %w", n+1, line, err)
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func parseLine(line string, f infoFormat, rules bool, sep, imp string) (record, error) {
	if !strings.HasSuffix(line, f.close) {
		return record{}, fmt.Errorf("missing %q", f.close)
	}
	i := strings.LastIndex(line, f.open)
	if i < 0 {
		return record{}, fmt.Errorf("missing %q", f.open)
	}
	prefix := line[:i]
	inner := line[i+len(f.open) : len(line)-len(f.close)]
	parts := strings.Split(inner, ",")
	if len(parts) != len(f.fields) {
		return record{}, fmt.Errorf("%d fields, want %d", len(parts), len(f.fields))
	}
	rec := record{line: line, values: make([]float64, len(parts))}
	for k, p := range parts {
		v, err := parseNumber(p)
		if err != nil {
			return record{}, err
		}
		if isInt(f.fields[k].code) && v != math.Trunc(v) {
			return record{}, fmt.Errorf("field %d: %q is not an integer", k, p)
		}
		rec.values[k] = v
	}
	if rules {
		j := strings.Index(prefix, imp)
		if j <= 0 {
			return record{}, fmt.Errorf("missing implication sign %q", imp)
		}
		rec.head = prefix[:j]
		prefix = prefix[j+len(imp):]
	}
	if prefix != "" {
		rec.items = strings.Split(prefix, sep)
	}
	slices.Sort(rec.items)
	if len(slices.Compact(slices.Clone(rec.items))) != len(rec.items) {
		return record{}, fmt.Errorf("duplicate item")
	}
	if rules {
		rec.key = ruleKey(rec.head, rec.items)
	} else {
		rec.key = setKey(rec.items)
	}
	return rec, nil
}

// parseNumber parses upstream's number output (including "inf", "-inf"
// and "nan").
func parseNumber(s string) (float64, error) {
	switch s {
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan", "-nan":
		return math.NaN(), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("bad number %q", s)
	}
	return v, nil
}

// timeRE matches the timings of the progress messages.
var timeRE = regexp.MustCompile(`\[[0-9]+\.[0-9]{2}s\]`)

// countRE matches the set/rule count of the "writing" message.
var countRE = regexp.MustCompile(`\[([0-9]+) (set|rule)\(s\)\]`)

// normStderr removes the run-dependent timings from a log.
func normStderr(b []byte) []byte {
	return timeRE.ReplaceAll(b, []byte("[T]"))
}

// reportedCount returns the number in "[N set(s)]" of a log, or -1.
func reportedCount(stderr []byte) int64 {
	m := countRE.FindSubmatch(stderr)
	if m == nil {
		return -1
	}
	n, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// parseStats parses the -Z statistics ("all: N" and "  k: n" lines) into
// counts per size; ok is false if stdout holds anything else.
func parseStats(stdout []byte) (all int64, perSize []int64, ok bool) {
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "all: ") {
		return 0, nil, false
	}
	all, err := strconv.ParseInt(strings.TrimPrefix(lines[0], "all: "), 10, 64)
	if err != nil {
		return 0, nil, false
	}
	for k, l := range lines[1:] {
		want := fmt.Sprintf("%3d: ", k)
		if !strings.HasPrefix(l, want) {
			return 0, nil, false
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(l, want), 10, 64)
		if err != nil {
			return 0, nil, false
		}
		perSize = append(perSize, n)
	}
	return all, perSize, true
}

// parseSpectrum parses a pattern spectrum file ("size supp count" lines)
// into a map from [size, supp] to count.
func parseSpectrum(data []byte) (map[[2]int64]int64, error) {
	psp := map[[2]int64]int64{}
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		f := strings.Fields(string(l))
		if len(f) != 3 {
			return nil, fmt.Errorf("bad spectrum line %q", l)
		}
		var v [3]int64
		for i := range f {
			n, err := strconv.ParseInt(f[i], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("bad spectrum line %q", l)
			}
			v[i] = n
		}
		psp[[2]int64{v[0], v[1]}] += v[2]
	}
	return psp, nil
}
