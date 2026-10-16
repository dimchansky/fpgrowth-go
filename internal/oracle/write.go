package oracle

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteTable writes txs in upstream's default input format: one record per
// transaction, terminated by "\n", with the item names separated by single
// spaces. Names are written verbatim (upstream has no escaping), in the
// transaction's order and including duplicates.
//
// With weighted, the weight is appended as the last field, as fpgrowth -w
// expects; an empty transaction becomes a record holding only its weight.
// Without weighted, every weight must be 1 and an empty transaction is an
// empty line (upstream counts empty records as empty transactions, also a
// trailing one).
//
// If a record would start with '#' (the default comment character), it is
// prefixed with one space: upstream recognizes comments only at the first
// byte of a record and skips leading blanks afterwards, so the record is
// read as data. Names must be non-empty and must not contain "\n";
// otherwise WriteTable returns an error. Names for which SafeName is false
// are written anyway but are read back differently.
//
// Round-trip guarantee: if every name satisfies SafeName (true for all
// names of the styles NamesLetters, NamesNumbers, NamesUnicode and
// NamesHash), reading the output with upstream's default settings (and -w
// iff weighted) yields exactly txs: the same records in the same order,
// the same items (duplicates are dropped by the reader as by this
// package) and the same weights.
func WriteTable(w io.Writer, txs []Transaction, weighted bool) error {
	bw := bufio.NewWriter(w)
	for t, tx := range txs {
		if tx.Weight < 0 {
			return fmt.Errorf("transaction %d: weight %d: %w", t, tx.Weight, ErrNegativeWeight)
		}
		if !weighted && tx.Weight != 1 {
			return fmt.Errorf("oracle: transaction %d has weight %d; write it weighted", t, tx.Weight)
		}
		for _, item := range tx.Items {
			if item == "" || strings.Contains(item, "\n") {
				return fmt.Errorf("oracle: transaction %d: item name %q cannot be written", t, item)
			}
		}
		if len(tx.Items) > 0 && tx.Items[0][0] == '#' {
			bw.WriteByte(' ')
		}
		for i, item := range tx.Items {
			if i > 0 {
				bw.WriteByte(' ')
			}
			bw.WriteString(item)
		}
		if weighted {
			if len(tx.Items) > 0 {
				bw.WriteByte(' ')
			}
			bw.WriteString(strconv.FormatInt(int64(tx.Weight), 10))
		}
		bw.WriteByte('\n')
	}
	return bw.Flush()
}
