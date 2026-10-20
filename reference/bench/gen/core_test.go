package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

// coreBlock returns the shared generator code between the markers.
func coreBlock(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	begin := bytes.Index(b, []byte("// BEGIN BENCHGEN CORE"))
	end := bytes.Index(b, []byte("// END BENCHGEN CORE"))
	if begin < 0 || end < begin {
		t.Fatalf("%s: markers not found", path)
	}
	return b[begin:end]
}

// TestCoreInSync checks that the root module's copy of the generator
// (benchgen_test.go) equals this one.
func TestCoreInSync(t *testing.T) {
	if !bytes.Equal(coreBlock(t, "core.go"), coreBlock(t, "../../../benchgen_test.go")) {
		t.Fatal("reference/bench/gen/core.go and benchgen_test.go differ between the BEGIN/END markers")
	}
}

// TestDatasetsPinned pins the bytes of every dataset, so that results
// recorded in docs/benchmarks.md stay reproducible. Update the hashes only
// together with the documented dataset characteristics.
func TestDatasetsPinned(t *testing.T) {
	want := map[string]string{
		"sparse":        "486a2624710d",
		"sparse-large":  "f9d614829794",
		"dense":         "b29d20acbcb1",
		"long":          "cddca8f4d640",
		"weighted":      "12e2b434b872",
		"d2000x40d60":   "4b7365057352",
		"d1000x30d80":   "868be1637571",
		"d5000x60d40":   "4017ec71e7fa",
		"d20000x200d30": "a91a38aab4da",
		"t100000x204c4": "768bd8ccd3c2",
	}
	for _, s := range benchSpecs {
		h := sha256.New()
		if err := benchWrite(h, s, false); err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%x", h.Sum(nil)[:6])
		if got != want[s.Name] {
			t.Errorf("%s: sha256 prefix %s, want %s", s.Name, got, want[s.Name])
		}
	}
}
