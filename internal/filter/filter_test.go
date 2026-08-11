package filter

import "testing"

func TestMatcher(t *testing.T) {
	m, err := New([]string{"*.txt", "nested/*"}, []string{"nested/private.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Include("notes.txt") || !m.Include("nested/data.bin") || m.Include("image.png") || m.Include("nested/private.txt") {
		t.Fatal("unexpected filter result")
	}
}

func TestMatcherRejectsInvalidGlob(t *testing.T) {
	if _, err := New([]string{"["}, nil); err == nil {
		t.Fatal("accepted invalid glob")
	}
}
