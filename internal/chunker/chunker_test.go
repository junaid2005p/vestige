package chunker

import (
	"bytes"
	"testing"
)

func TestSplitIsDeterministicAndPreservesData(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefghijklmno"), 100)
	cfg := Config{Window: 8, MinSize: 16, TargetSize: 64, MaxSize: 128}
	first := splitForTest(t, data, cfg)
	second := splitForTest(t, data, cfg)
	if !bytes.Equal(bytes.Join(first, nil), data) {
		t.Fatal("split data did not reconstruct the input")
	}
	if len(first) != len(second) {
		t.Fatal("split was not deterministic")
	}
	for i := range first {
		if !bytes.Equal(first[i], second[i]) {
			t.Fatalf("chunk %d differs", i)
		}
		if len(first[i]) > cfg.MaxSize {
			t.Fatalf("chunk %d exceeds maximum", i)
		}
	}
}

func TestSplitEmpty(t *testing.T) {
	if chunks := splitForTest(t, nil, Config{Window: 8, MinSize: 4, TargetSize: 16, MaxSize: 64}); len(chunks) != 0 {
		t.Fatal("empty input created chunks")
	}
}

func splitForTest(t *testing.T, data []byte, cfg Config) [][]byte {
	t.Helper()
	var chunks [][]byte
	if err := Split(bytes.NewReader(data), cfg, func(chunk []byte) error { chunks = append(chunks, append([]byte(nil), chunk...)); return nil }); err != nil {
		t.Fatal(err)
	}
	return chunks
}
