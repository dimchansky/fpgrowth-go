package difftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestGenerate_matchesOracle pins the copied generator to the SHA-256
// values of internal/oracle's TestGenerate_golden, so that the copy
// produces exactly the oracle's datasets.
func TestGenerate_matchesOracle(t *testing.T) {
	t.Parallel()
	rich := func(seed uint64, names nameStyle, weights weightDist) genConfig {
		return genConfig{
			seed: seed, numTransactions: 300, numItems: 30, minLen: 0, maxLen: 8, skew: 1,
			patterns: 4, patternLen: 3, patternProb: 0.3, dupItemProb: 0.05, dupTxProb: 0.1,
			emptyProb: 0.05, weights: weights, names: names,
		}
	}
	skew3 := rich(3, namesNumbers, weightsLarge)
	skew3.skew = 3
	tests := []struct {
		name     string
		cfg      genConfig
		weighted bool
		expected string
	}{
		{"letters unit weights", rich(1, namesLetters, weightsUnit), false,
			"cadf469f385356855d4f6cf281607bdc2c975d8cee53ce38eef608835d420451"},
		{"unicode zero weights", rich(2, namesUnicode, weightsWithZero), true,
			"4bc5b50abbde288d61bc5b2bddfbb48cc229eb7737de380f89e66b9612d89894"},
		{"numbers large weights skew 3", skew3, true,
			"a450dabb6052da3eda7e7e4ed9cfae21646ff4a27f000f02aa377ff4c3e563b5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			txs, err := generate(tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := writeTable(&buf, txs, tt.weighted); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(buf.Bytes())
			if got := hex.EncodeToString(sum[:]); got != tt.expected {
				t.Errorf("SHA-256 = %s, want %s (oracle)", got, tt.expected)
			}
		})
	}
}
