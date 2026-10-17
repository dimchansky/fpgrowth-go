package api

import (
	"io"
	"strings"
	"testing"

	fp "github.com/dimchansky/fpgrowth-go"
)

func TestTableFormatFootgun(t *testing.T) {
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("a b 2\na c 3\n")), nil }
	for name, f := range map[string]fp.TableFormat{
		"{Weighted:true}":         {Weighted: true},
		"{FieldSeparators:\";\"}": {FieldSeparators: ";"},
	} {
		sets, sum, err := collect(t, fp.TableReader(open, "mem", f), fp.Options[string]{MinSupport: fp.Count(1)})
		t.Logf("%s: sets=%v tx=%d w=%d err=%v", name, sets, sum.Transactions, sum.TotalWeight, err)
	}
	// non-replayable: pass-2 decreases a frequent item: supports reported?
	pass := 0
	_ = pass
}
