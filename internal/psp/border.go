package psp

import (
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Border is the decision border of fpgpsp's option -D (with oracle patch
// P3): for the sizes zmin..zmax, where zmax is the largest size with a
// signature in psp, the smallest support that exceeds every support of
// the spectrum at this size and all larger sizes, at least 1:
// border[m] = max(maxsupp(m)+1, border[m+1]). It returns nil if the
// spectrum has no signature of size zmin or larger.
//
// The values are absolute supports for the sizes starting at zmin.
// fpgrowth's -F reads positive values as percentages, so they must be
// passed negated and with the same -m (docs/cli.md, U10).
func Border(psp *report.Spectrum, zmin int) []tract.Supp {
	zmax := psp.MaxSize()
	if zmax < zmin {
		return nil
	}
	b := make([]tract.Supp, zmax-zmin+1)
	bmin := tract.Supp(1)
	for m := zmax; m >= zmin; m-- {
		v := bmin
		if s, ok := psp.MaxSupport(m); ok && s+1 > v {
			v = s + 1
		}
		b[m-zmin] = v
		bmin = v
	}
	return b
}

// FormatBorder formats a border like fpgpsp: the values separated by
// colons, terminated by a newline; nothing for an empty border.
func FormatBorder(b []tract.Supp) string {
	if len(b) == 0 {
		return ""
	}
	buf := make([]byte, 0, 8*len(b))
	for i, v := range b {
		if i > 0 {
			buf = append(buf, ':')
		}
		buf = strconv.AppendInt(buf, int64(v), 10)
	}
	return string(append(buf, '\n'))
}
