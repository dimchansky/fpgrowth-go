package oracle

import (
	"slices"
	"testing"
)

func TestNewItemSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    []string
		expected ItemSet
	}{
		{name: "nil input", input: nil, expected: ItemSet{}},
		{name: "sorted unique", input: []string{"a", "b"}, expected: ItemSet{"a", "b"}},
		{name: "unsorted with duplicates", input: []string{"c", "a", "c", "b", "a"}, expected: ItemSet{"a", "b", "c"}},
		{name: "byte order", input: []string{"a", "B", "2", "10", "é"}, expected: ItemSet{"10", "2", "B", "a", "é"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := slices.Clone(tt.input)
			got := NewItemSet(tt.input...)
			if got == nil || !got.Equal(tt.expected) {
				t.Errorf("NewItemSet(%q) = %#v, want %#v", tt.input, got, tt.expected)
			}
			if !slices.Equal(input, tt.input) {
				t.Errorf("NewItemSet modified its input: %q", tt.input)
			}
		})
	}
}

func TestItemSet_Key(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		set      ItemSet
		expected string
	}{
		{name: "empty set", set: NewItemSet(), expected: ``},
		{name: "two items", set: NewItemSet("b", "a"), expected: `"a" "b"`},
		{name: "item with space", set: NewItemSet("a b"), expected: `"a b"`},
		{name: "item with quotes", set: NewItemSet(`a" "b`), expected: `"a\" \"b"`},
		{name: "control bytes", set: NewItemSet("x\ty\n", "\x00"), expected: `"\x00" "x\ty\n"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.set.Key(); got != tt.expected {
				t.Errorf("Key() = %s, want %s", got, tt.expected)
			}
			if got := tt.set.String(); got != "{"+tt.expected+"}" {
				t.Errorf("String() = %s, want {%s}", got, tt.expected)
			}
		})
	}

	// Sets that a naive join would confuse must get different keys.
	confusable := []ItemSet{
		NewItemSet("a b"), NewItemSet("a", "b"), NewItemSet(`a" "b`), NewItemSet("a", " b"),
	}
	keys := map[string]ItemSet{}
	for _, s := range confusable {
		if other, dup := keys[s.Key()]; dup {
			t.Errorf("sets %#v and %#v share the key %s", s, other, s.Key())
		}
		keys[s.Key()] = s
	}
}

func TestItemSet_Contains(t *testing.T) {
	t.Parallel()
	s := NewItemSet("c", "a", "b")
	for _, item := range []string{"a", "b", "c"} {
		if !s.Contains(item) {
			t.Errorf("%v.Contains(%q) = false", s, item)
		}
	}
	for _, item := range []string{"", "d", "ab"} {
		if s.Contains(item) {
			t.Errorf("%v.Contains(%q) = true", s, item)
		}
	}
}

func TestItemSet_Compare(t *testing.T) {
	t.Parallel()
	ordered := []ItemSet{
		NewItemSet(),
		NewItemSet("10"),
		NewItemSet("2"),
		NewItemSet("b"),
		NewItemSet("z"),
		NewItemSet("a", "z"),
		NewItemSet("b", "c"),
		NewItemSet("a", "b", "c"),
	}
	for i, s := range ordered {
		for j, u := range ordered {
			expected := 0
			switch {
			case i < j:
				expected = -1
			case i > j:
				expected = +1
			}
			if got := s.Compare(u); got != expected {
				t.Errorf("%v.Compare(%v) = %d, want %d", s, u, got, expected)
			}
		}
	}
}
