package oracle

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestWriteTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		txs      []Transaction
		weighted bool
		expected string
	}{
		{
			name: "unweighted with empty and duplicate items",
			txs: []Transaction{
				{Items: []string{"a", "b"}, Weight: 1},
				{Items: []string{}, Weight: 1},
				{Items: []string{"c", "a", "a"}, Weight: 1},
				{Items: nil, Weight: 1},
			},
			expected: "a b\n\nc a a\n\n",
		},
		{
			name: "comment protection",
			txs: []Transaction{
				{Items: []string{"#x", "y"}, Weight: 1},
				{Items: []string{"y", "#x"}, Weight: 1},
			},
			expected: " #x y\ny #x\n",
		},
		{
			name: "weighted",
			txs: []Transaction{
				{Items: []string{"a", "b"}, Weight: 2},
				{Items: []string{}, Weight: 3},
				{Items: []string{"#x"}, Weight: 0},
				{Items: []string{"9"}, Weight: 1 << 40},
			},
			weighted: true,
			expected: "a b 2\n3\n #x 0\n9 1099511627776\n",
		},
		{name: "no transactions", txs: nil, expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WriteTable(&buf, tt.txs, tt.weighted); err != nil {
				t.Fatalf("WriteTable: %v", err)
			}
			if got := buf.String(); got != tt.expected {
				t.Errorf("WriteTable wrote %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWriteTable_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		tx       Transaction
		weighted bool
	}{
		{name: "weight without -w", tx: Transaction{Items: []string{"a"}, Weight: 2}},
		{name: "zero weight without -w", tx: Transaction{Items: []string{"a"}, Weight: 0}},
		{name: "negative weight", tx: Transaction{Items: []string{"a"}, Weight: -1}, weighted: true},
		{name: "empty name", tx: Transaction{Items: []string{"a", ""}, Weight: 1}},
		{name: "newline in name", tx: Transaction{Items: []string{"a\nb"}, Weight: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := WriteTable(&bytes.Buffer{}, []Transaction{tt.tx}, tt.weighted); err == nil {
				t.Errorf("WriteTable(%+v) succeeded, want an error", tt.tx)
			}
		})
	}
	if err := WriteTable(failingWriter{}, parseTxs("a"), false); err == nil {
		t.Error("WriteTable to a failing writer succeeded")
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// readDefaultTable parses data like upstream's reader with its default
// character classes, restricted to what matters for WriteTable output:
// "\n" ends a record, a record whose first byte is '#' is a comment,
// fields are separated by runs of ' ', '\t' and ',' (blanks around fields
// are dropped), an empty record is an empty transaction, duplicates are
// dropped (first occurrence kept), and with weighted the last field is
// the weight.
func readDefaultTable(data string, weighted bool) ([]Transaction, error) {
	txs := []Transaction{}
	records := strings.SplitAfter(data, "\n")
	for _, rec := range records {
		if rec == "" {
			continue // after the final newline
		}
		rec = strings.TrimSuffix(rec, "\n")
		if strings.HasPrefix(rec, "#") {
			continue
		}
		fields := strings.FieldsFunc(rec, func(r rune) bool { return strings.ContainsRune(" \t,\r", r) })
		tx := Transaction{Items: []string{}, Weight: 1}
		if weighted {
			if len(fields) == 0 {
				return nil, fmt.Errorf("record %q: weight expected", rec)
			}
			w, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
			if err != nil {
				return nil, err
			}
			tx.Weight = tract.Supp(w)
			fields = fields[:len(fields)-1]
		}
		for _, f := range fields {
			if !slices.Contains(tx.Items, f) {
				tx.Items = append(tx.Items, f)
			}
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

// dedup returns items without repetitions, first occurrences kept.
func dedup(items []string) []string {
	out := []string{}
	for _, item := range items {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

func TestWriteTable_roundTrip(t *testing.T) {
	t.Parallel()
	for _, names := range []NameStyle{NamesLetters, NamesNumbers, NamesUnicode, NamesHash} {
		for _, weights := range []WeightDist{WeightsUnit, WeightsWithZero, WeightsLarge} {
			for seed := range uint64(20) {
				cfg := richConfig(seed, names, weights)
				cfg.EmptyProb = 0.2
				txs, err := Generate(cfg)
				if err != nil {
					t.Fatal(err)
				}
				weighted := weights != WeightsUnit
				data := string(generateTable(t, cfg, weighted))
				back, err := readDefaultTable(data, weighted)
				if err != nil {
					t.Fatalf("names %d weights %d seed %d: %v", names, weights, seed, err)
				}
				if len(back) != len(txs) {
					t.Fatalf("names %d weights %d seed %d: read %d transactions, want %d", names, weights, seed, len(back), len(txs))
				}
				for i := range txs {
					if !slices.Equal(back[i].Items, dedup(txs[i].Items)) || back[i].Weight != txs[i].Weight {
						t.Fatalf("names %d weights %d seed %d: transaction %d read as %+v, want %+v", names, weights, seed, i, back[i], txs[i])
					}
				}
			}
		}
	}
}

// TestWriteTable_unsafeNames shows why NamesSeparators does not round-trip.
func TestWriteTable_unsafeNames(t *testing.T) {
	t.Parallel()
	txs := []Transaction{{Items: []string{"a,x", "b x", "c\tx", "d\r"}, Weight: 1}}
	var buf bytes.Buffer
	if err := WriteTable(&buf, txs, false); err != nil {
		t.Fatal(err)
	}
	back, err := readDefaultTable(buf.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	if expected := []string{"a", "x", "b", "c", "d"}; len(back) != 1 || !slices.Equal(back[0].Items, expected) {
		t.Errorf("read back %+v, want items %q", back, expected)
	}
}
