package tabread

import (
	"errors"
	"io"
)

// delim is the kind of delimiter that ended a field (tabread.h TRD_*).
type delim int8

const (
	delimErr delim = -2 // TRD_ERR: read error
	delimEOF delim = -1 // TRD_EOF: end of input, no field
	delimFld delim = 0  // TRD_FLD: field ended, more fields follow
	delimRec delim = 1  // TRD_REC: field ended, it was the last one of its record
)

const (
	// bufSize is the size of the read buffer (TRD_BUFSIZE).
	bufSize = 65536

	// chEOF and chErr are getc's end-of-input and error results.
	chEOF = -1
	chErr = -2

	// maxEmptyReads bounds consecutive (0, nil) results of the source,
	// like bufio.Reader.
	maxEmptyReads = 100
)

// errInvalidRead reports an io.Reader that returned an impossible count.
var errInvalidRead = errors.New("invalid read count")

// tokenizer splits its input into fields and records. Port of upstream's
// TABREAD and trd_read (tabread.c) without the null-value class, which
// fpgrowth never uses.
type tokenizer struct {
	src    io.Reader
	cls    classTable
	buf    []byte
	pos    int   // next byte of buf to read
	end    int   // end of the valid bytes of buf
	srcErr error // error of the last source read, reported once buf is drained

	// field is the current field, without the C NUL terminator. It is a
	// view of buf while the field lies inside the buffered block (view),
	// else it is held in fbuf; fill copies a view to fbuf before it
	// overwrites buf, so field stays valid until the next read.
	field []byte
	fbuf  []byte
	view  bool
	delim delim // delimiter of the current field
	rec   int   // 1-based number of the record the current field belongs to
	fld   int   // 1-based number of the current field within its record
}

func newTokenizer(src io.Reader, cfg Config) *tokenizer {
	return newTokenizerSize(src, cfg, bufSize)
}

// newTokenizerSize is newTokenizer with a read buffer of size bytes
// (tests use small buffers to move the block boundaries).
func newTokenizerSize(src io.Reader, cfg Config, size int) *tokenizer {
	return &tokenizer{
		src:   src,
		cls:   newClassTable(cfg),
		buf:   make([]byte, size),
		delim: delimEOF,
		rec:   1,
	}
}

// is reports whether byte c (>= 0) belongs to one of the classes.
func (t *tokenizer) is(c int, class byteClass) bool {
	return t.cls[uint8(c)]&class != 0 // c is a byte value (0-255) here
}

// getc returns the next input byte, chEOF or chErr (trd_getc).
func (t *tokenizer) getc() int {
	if t.pos >= t.end {
		return t.fill()
	}
	t.pos++
	return int(t.buf[t.pos-1])
}

// fill reads the next block from the source and returns (and consumes)
// its first byte, or chEOF/chErr. A source error that comes with data is
// kept and reported when that data has been consumed, like fread/ferror.
func (t *tokenizer) fill() int {
	if t.view { // the field must survive the overwrite of buf
		t.fbuf = append(t.fbuf[:0], t.field...)
		t.field, t.view = t.fbuf, false
	}
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

func (t *tokenizer) errChar() int {
	if errors.Is(t.srcErr, io.EOF) {
		return chEOF
	}
	return chErr
}

// err returns the read error after read returned delimErr.
func (t *tokenizer) err() error {
	return t.srcErr
}

// ungetc pushes back the byte returned by the last getc (trd_ungetc). It
// is only called directly after a successful getc, so pos > 0.
func (t *tokenizer) ungetc() {
	t.pos--
}

// setDelim records and returns the delimiter of the current field.
func (t *tokenizer) setDelim(d delim) delim {
	t.delim = d
	return d
}

// stop ends a field because getc returned c < 0: a read error, or the end
// of input, which yields d.
func (t *tokenizer) stop(c int, d delim) delim {
	if c == chErr {
		return t.setDelim(delimErr)
	}
	return t.setDelim(d)
}

// next advances the field and record numbers to the field read next.
func (t *tokenizer) next() {
	if t.delim == delimFld {
		t.fld++
		return
	}
	t.fld = 1
	// Deviation from upstream: the record number advances when the next
	// record starts, so it is right for every field, including a last
	// record cut short by the end of input. Upstream counts a record when
	// its separator is read and reports the previous record if the input
	// ends during trailing blanks (cli-io.md B10).
	if t.delim == delimRec {
		t.rec++
	}
}

// plainField is the fast path of read for the common field: bytes of no
// class, inside the buffered block, ended by a separator that decides
// the delimiter without reading further: a record separator, a field
// separator that is no blank, or a blank field separator followed by a
// byte of no class, which starts the next field. It reads such a field
// exactly as read's steps 1-6 would and reports true; for any other
// input it changes nothing and reports false.
func (t *tokenizer) plainField() bool {
	buf, start := t.buf[:t.end], t.pos
	i := start
	for i < len(buf) && t.cls[buf[i]] == 0 {
		i++
	}
	if i == start || i >= len(buf) {
		return false // no field byte first, or the block ends
	}
	sep := t.cls[buf[i]]
	var d delim
	switch {
	case sep&classSep == 0: // a blank (or a comment byte) follows
		return false
	case sep&classFldSep == 0: // step 4: a record separator
		d = delimRec
	case sep&classBlank != 0: // step 6 skips blanks after the separator
		if i+1 >= len(buf) || t.cls[buf[i+1]] != 0 {
			return false
		}
		d = delimFld // the next byte starts the next field (ungetc)
	case sep&classRecSep != 0: // step 6: also a record separator
		d = delimRec
	default:
		d = delimFld
	}
	t.next()
	t.field, t.view = buf[start:i:i], true
	t.pos = i + 1
	t.delim = d
	return true
}

// read reads the next field into t.field and returns its delimiter. Port
// of trd_read; the numbered steps refer to docs/upstream/cli-io.md 4.3.
//
// delimEOF is returned only if the input ends at the start of a record
// (and after skipped comment records). If the input ends after at least
// one byte of a record, the field is completed with delimRec.
func (t *tokenizer) read() delim {
	if t.plainField() {
		return t.delim
	}
	t.next()
	t.field, t.view = t.fbuf[:0], false
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

// scanField reads the field that starts with the byte returned by the
// last getc up to the next separator into t.field. It returns the
// delimiter kind and the separator byte that ended the field, which has
// been consumed, or delimRec/delimErr with chEOF/chErr if the input ended.
//
// A field inside the buffered block becomes a view of buf without
// copying; one that continues after the block is copied to fbuf (by fill)
// and extended there.
func (t *tokenizer) scanField() (delim, int) {
	start := t.pos - 1 // the byte of the last getc
	t.view = true
	for {
		buf, i := t.buf[:t.end], t.pos
		for i < len(buf) && t.cls[buf[i]]&classSep == 0 {
			i++
		}
		if t.view {
			t.field = buf[start:i:i] // full slice: never appended to
		} else {
			t.field = append(t.field, buf[t.pos:i]...)
		}
		if i < len(buf) {
			c := buf[i]
			t.pos = i + 1
			if t.cls[c]&classFldSep != 0 {
				return delimFld, int(c)
			}
			return delimRec, int(c)
		}
		t.pos = i
		c := t.fill() // copies the view to fbuf
		switch c {
		case chEOF:
			return delimRec, c
		case chErr:
			return delimErr, c
		}
		t.ungetc() // let the scan loop above classify the new byte
	}
}
