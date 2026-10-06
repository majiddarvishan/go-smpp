package codec

import (
	"os"
	"path/filepath"
	"testing"
)

// The committed fuzz corpus is what keeps `go test -fuzz` from starting cold and
// what lets plain `go test` replay it as a regression suite. A directory that
// is deleted or emptied by a tidy-up would silently turn both off, so this
// fails loudly instead. The minimums are well under the sizes committed on
// 2026-10-05 (13 and 178 files) so refreshing the corpus never trips them.
func TestFuzzCorpusIsCommitted(t *testing.T) {
	for _, c := range []struct {
		target string
		min    int
	}{{"FuzzFramer", 8}, {"FuzzDecodePDU", 100}} {
		entries, err := os.ReadDir(filepath.Join("testdata", "fuzz", c.target))
		if err != nil {
			t.Fatalf("%s: corpus directory is missing: %v", c.target, err)
		}
		files := 0
		for _, e := range entries {
			if !e.IsDir() {
				files++
			}
		}
		if files < c.min {
			t.Fatalf("%s: %d corpus files, want at least %d; was the corpus deleted?", c.target, files, c.min)
		}
	}
}
