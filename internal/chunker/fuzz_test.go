package chunker

import (
	"bytes"
	"testing"
)

func FuzzSplitPreservesInput(f *testing.F) {
	f.Add([]byte("a small deterministic seed"))
	f.Add(make([]byte, 0))
	f.Fuzz(func(t *testing.T, input []byte) {
		cfg := Config{Window: 8, MinSize: 4, TargetSize: 16, MaxSize: 64}
		var joined []byte
		if err := Split(bytes.NewReader(input), cfg, func(chunk []byte) error {
			if len(chunk) == 0 || len(chunk) > cfg.MaxSize {
				t.Fatalf("invalid chunk size %d", len(chunk))
			}
			joined = append(joined, chunk...)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(joined, input) {
			t.Fatal("chunking changed input")
		}
	})
}
