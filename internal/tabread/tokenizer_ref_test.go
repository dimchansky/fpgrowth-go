package tabread

// refTokenizer is the tokenizer as it was before the buffer views and the
// fast path of issue #48 (main at fde9e41), kept unchanged as a test
// oracle: FuzzTokenizerReference checks that the optimized tokenizer
// gives the same fields, delimiters, record and field numbers and errors
// for every input, configuration and buffer size.

import (
	"errors"
	"io"
)

// refTokenizer splits its input into fields and records. Port of upstream's
// TABREAD and trd_read (tabread.c) without the null-value class, which
// fpgrowth never uses.
type refTokenizer struct {
	src    io.Reader
	cls    classTable
	buf    []byte
	pos    int   // next byte of buf to read
	end    int   // end of the valid bytes of buf
	srcErr error // error of the last source read, reported once buf is drained

	field []byte // current field, without the C NUL terminator
	delim delim  // delimiter of the current field
	rec   int    // 1-based number of the record the current field belongs to
	fld   int    // 1-based number of the current field within its record
}

func newRefTokenizer(src io.Reader, cfg Config) *refTokenizer {
	return &refTokenizer{
		src:   src,
		cls:   newClassTable(cfg),
		buf:   make([]byte, bufSize),
		delim: delimEOF,
		rec:   1,
	}
}

// is reports whether byte c (>= 0) belongs to one of the classes.
func (t *refTokenizer) is(c int, class byteClass) bool {
	return t.cls[uint8(c)]&class != 0 // c is a byte value (0-255) here
}

// getc returns the next input byte, chEOF or chErr (trd_getc).
func (t *refTokenizer) getc() int {
	if t.pos < t.end {
		c := t.buf[t.pos]
		t.pos++
		return int(c)
	}
	return t.fill()
}

// fill reads the next block from the source and returns (and consumes)
// its first byte, or chEOF/chErr. A source error that comes with data is
// kept and reported when that data has been consumed, like fread/ferror.
func (t *refTokenizer) fill() int {
	if t.srcErr != nil {
		return t.errChar()
	}
	for range maxEmptyReads {
		n, err := t.src.Read(t.buf)
		if n < 0 || n > len(t.buf) {
			t.srcErr = errInvalidRead
			return chErr
		}
		if err != nil {
			t.srcErr = err
		}
		if n > 0 {
			t.pos, t.end = 1, n
			return int(t.buf[0])
		}
		if err != nil {
			return t.errChar()
		}
	}
	t.srcErr = io.ErrNoProgress
	return chErr
}

func (t *refTokenizer) errChar() int {
	if errors.Is(t.srcErr, io.EOF) {
		return chEOF
	}
	return chErr
}

// err returns the read error after read returned delimErr.
func (t *refTokenizer) err() error {
	return t.srcErr
}

// ungetc pushes back the byte returned by the last getc (trd_ungetc). It
// is only called directly after a successful getc, so pos > 0.
func (t *refTokenizer) ungetc() {
	t.pos--
}

// setDelim records and returns the delimiter of the current field.
func (t *refTokenizer) setDelim(d delim) delim {
	t.delim = d
	return d
}

// stop ends a field because getc returned c < 0: a read error, or the end
// of input, which yields d.
func (t *refTokenizer) stop(c int, d delim) delim {
	if c == chErr {
		return t.setDelim(delimErr)
	}
	return t.setDelim(d)
}

// read reads the next field into t.field and returns its delimiter. Port
// of trd_read; the numbered steps refer to docs/upstream/cli-io.md 4.3.
//
// delimEOF is returned only if the input ends at the start of a record
// (and after skipped comment records). If the input ends after at least
// one byte of a record, the field is completed with delimRec.
func (t *refTokenizer) read() delim {
	if t.delim == delimFld {
		t.fld++
	} else {
		t.fld = 1
		// Deviation from upstream: the record number advances when the
		// next record starts, so it is right for every field, including
		// a last record cut short by the end of input. Upstream counts a
		// record when its separator is read and reports the previous
		// record if the input ends during trailing blanks (cli-io.md B10).
		if t.delim == delimRec {
			t.rec++
		}
	}
	t.field = t.field[:0]
	c := t.getc()
	if c < 0 {
		if c == chEOF && t.delim == delimFld {
			// Deviation from upstream: the end of input right after a
			// field separator ends the record with an empty last field,
			// as a record separator would. Upstream returns TRD_EOF here
			// and the caller loses the whole record (tract.md T3).
			return t.setDelim(delimRec)
		}
		return t.stop(c, delimEOF)
	}

	// 1. Skip comment records (only at the start of a record).
	if t.delim != delimFld {
		for t.is(c, classComment) {
			for !t.is(c, classRecSep) {
				if c = t.getc(); c < 0 {
					return t.stop(c, delimEOF)
				}
			}
			t.rec++
			if c = t.getc(); c < 0 {
				return t.stop(c, delimEOF)
			}
		}
	}

	// 2. Skip leading blanks; the end of input yields an empty last field.
	for t.is(c, classBlank) {
		if c = t.getc(); c < 0 {
			return t.stop(c, delimRec)
		}
	}

	// 3. A separator here ends an empty field; field separator wins.
	if t.is(c, classSep) {
		if t.is(c, classFldSep) {
			return t.setDelim(delimFld)
		}
		return t.setDelim(delimRec)
	}

	// 4. Read the field up to the next separator or the end of input.
	// Deviation from upstream: no truncation to TRD_MAXLEN = 1024 bytes
	// (cli-io.md B14).
	t.field = append(t.field, byte(c))
	d, c := t.scanField()

	// 5. Remove trailing blanks. The first byte is not a blank, so at
	// least one byte remains.
	n := len(t.field)
	for n > 1 && t.cls[t.field[n-1]]&classBlank != 0 {
		n--
	}
	t.field = t.field[:n]
	if d != delimFld {
		return t.setDelim(d)
	}

	// 6. After a field separator skip blanks; a record separator then
	// ends the record, another field separator merges with this one, and
	// any other byte starts the next field.
	for t.is(c, classBlank) {
		if c = t.getc(); c < 0 {
			return t.stop(c, delimRec)
		}
	}
	if t.is(c, classRecSep) {
		return t.setDelim(delimRec)
	}
	if !t.is(c, classFldSep) {
		t.ungetc()
	}
	return t.setDelim(delimFld)
}

// scanField appends the bytes up to the next separator to t.field. It
// returns the delimiter kind and the separator byte that ended the field,
// which has been consumed, or delimRec/delimErr with chEOF/chErr if the
// input ended.
func (t *refTokenizer) scanField() (delim, int) {
	for {
		i := t.pos
		for i < t.end && t.cls[t.buf[i]]&classSep == 0 {
			i++
		}
		t.field = append(t.field, t.buf[t.pos:i]...)
		if i < t.end {
			c := t.buf[i]
			t.pos = i + 1
			if t.cls[c]&classFldSep != 0 {
				return delimFld, int(c)
			}
			return delimRec, int(c)
		}
		t.pos = i
		c := t.fill()
		switch c {
		case chEOF:
			return delimRec, c
		case chErr:
			return delimErr, c
		}
		t.ungetc() // let the scan loop above classify the new byte
	}
}
