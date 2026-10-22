package report

import (
	"iter"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Spectrum is a pattern spectrum: the number of reported item sets per
// (size, support) signature. It is a port of PATSPEC (patspec.c) for
// integer supports.
//
// Deviation from upstream: patspec.c keeps a dense counter array per size
// that spans the observed support range, so its memory grows with the
// support values themselves (two item sets with supports 1 and 2·10⁷ cost
// 162 MB, tract.md T9/R10, and resize computes supp+32 in int, T18). Here
// every size row is a map from support to count, so memory is proportional
// to the number of distinct (size, support) pairs. The reported cells, their
// order (All), SigCount and Total are identical.
//
// Counts are uint64 and saturate at math.MaxUint64 instead of wrapping like
// upstream's size_t (see Saturated).
type Spectrum struct {
	minSize, maxSize int
	minSupp, maxSupp tract.Supp
	rows             []map[tract.Supp]uint64 // rows[size-minSize]
	sigCnt           int
	total            uint64
	saturated        bool
}

// Cell is one non-zero entry of a pattern spectrum.
type Cell struct {
	Size  int        // item set size
	Supp  tract.Supp // item set support
	Count uint64     // number of item sets with this size and support
}

// NewSpectrum is a port of psp_create. Sizes and supports outside
// [minSize, maxSize] × [minSupp, maxSupp] are ignored by Add. A negative or
// unlimited (>= MaxSize) maxSize is clamped to MaxSize-1 and a negative or
// unlimited (>= MaxSupp) maxSupp to MaxSupp-1, as upstream does with
// ITEM_MAX and RSUPP_MAX.
func NewSpectrum(minSize, maxSize int, minSupp, maxSupp tract.Supp) *Spectrum {
	if maxSize < 0 || maxSize >= MaxSize {
		maxSize = MaxSize - 1
	}
	if maxSupp < 0 || maxSupp >= MaxSupp {
		maxSupp = MaxSupp - 1
	}
	return &Spectrum{
		minSize: minSize,
		maxSize: maxSize,
		minSupp: minSupp,
		maxSupp: maxSupp,
	}
}

// Add is a port of psp_incfrq: it adds frq item sets with the given size
// and support. Values outside the spectrum's limits are silently ignored.
func (p *Spectrum) Add(size int, supp tract.Supp, frq uint64) {
	if size < p.minSize || size > p.maxSize || supp < p.minSupp || supp > p.maxSupp {
		return
	}
	if frq == 0 { // no observable effect upstream either
		return
	}
	i := size - p.minSize
	for len(p.rows) <= i {
		p.rows = append(p.rows, nil)
	}
	row := p.rows[i]
	if row == nil {
		row = make(map[tract.Supp]uint64)
		p.rows[i] = row
	}
	old := row[supp]
	if old == 0 {
		p.sigCnt++
	}
	row[supp] = p.add(old, frq)
	p.total = p.add(p.total, frq)
}

func (p *Spectrum) add(a, b uint64) uint64 {
	s, sat := satAdd(a, b)
	p.saturated = p.saturated || sat
	return s
}

// Merge is a port of psp_addpsp for integer supports: it adds every cell
// of src to p (Add, so cells outside p's limits are ignored). The result
// does not depend on the order of merges.
func (p *Spectrum) Merge(src *Spectrum) {
	for c := range src.All() {
		p.Add(c.Size, c.Supp, c.Count)
	}
	p.saturated = p.saturated || src.saturated
}

// MaxSupport returns the largest support with a non-zero count for the
// given size (psp_max4sz for a row with signatures) and whether there is
// one.
func (p *Spectrum) MaxSupport(size int) (tract.Supp, bool) {
	i := size - p.minSize
	if i < 0 || i >= len(p.rows) || len(p.rows[i]) == 0 {
		return 0, false
	}
	var m tract.Supp
	first := true
	for s := range p.rows[i] {
		if first || s > m {
			m, first = s, false
		}
	}
	return m, true
}

// MinSupp returns the smallest support counted by the spectrum
// (psp_minsupp).
func (p *Spectrum) MinSupp() tract.Supp { return p.minSupp }

// MaxSize returns the largest size with a non-zero count (psp_max), or
// the minimum size minus 1 for an empty spectrum.
func (p *Spectrum) MaxSize() int {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if len(p.rows[i]) > 0 {
			return p.minSize + i
		}
	}
	return p.minSize - 1
}

// Frequency is a port of psp_getfrq: the number of item sets with the given
// size and support.
func (p *Spectrum) Frequency(size int, supp tract.Supp) uint64 {
	i := size - p.minSize
	if i < 0 || i >= len(p.rows) {
		return 0
	}
	return p.rows[i][supp]
}

// SigCount is a port of psp_sigcnt: the number of (size, support) pairs
// with a non-zero count.
func (p *Spectrum) SigCount() int { return p.sigCnt }

// Total is a port of psp_total: the sum of all counts.
func (p *Spectrum) Total() uint64 { return p.total }

// Saturated reports whether a count or the total reached math.MaxUint64
// and was clamped.
func (p *Spectrum) Saturated() bool { return p.saturated }

// All yields the non-zero cells ordered by increasing size and, within a
// size, by increasing support: the order of psp_report. The spectrum must
// not be modified during the iteration.
func (p *Spectrum) All() iter.Seq[Cell] {
	return func(yield func(Cell) bool) {
		var supps []tract.Supp
		for i, row := range p.rows {
			if len(row) == 0 {
				continue
			}
			supps = supps[:0]
			for s := range row {
				supps = append(supps, s)
			}
			slices.Sort(supps)
			for _, s := range supps {
				if !yield(Cell{Size: p.minSize + i, Supp: s, Count: row[s]}) {
					return
				}
			}
		}
	}
}
