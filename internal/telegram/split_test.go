package telegram

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func TestSplitKeepsShortText(t *testing.T) {
	chunks := Split("hello", MessageLimit)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("chunks = %q", chunks)
	}
}

func TestSplitEmptyTextYieldsNothing(t *testing.T) {
	if chunks := Split("", MessageLimit); len(chunks) != 0 {
		t.Errorf("chunks = %q", chunks)
	}
}

func TestSplitPrefersLineBreaks(t *testing.T) {
	text := "first line\nsecond line\nthird line"
	chunks := Split(text, 25)
	want := []string{"first line\nsecond line\n", "third line"}
	if len(chunks) != len(want) || chunks[0] != want[0] || chunks[1] != want[1] {
		t.Errorf("chunks = %q, want %q", chunks, want)
	}
}

func TestSplitFallsBackToSpaces(t *testing.T) {
	chunks := Split("alpha beta gamma", 12)
	if len(chunks) != 2 || chunks[0] != "alpha beta " || chunks[1] != "gamma" {
		t.Errorf("chunks = %q", chunks)
	}
}

func TestSplitCutsLongUnbrokenLine(t *testing.T) {
	text := strings.Repeat("a", 9000)
	chunks := Split(text, MessageLimit)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if strings.Join(chunks, "") != text {
		t.Error("chunks do not rejoin to the original text")
	}
}

func TestSplitCountsUTF16UnitsAndKeepsRunesWhole(t *testing.T) {
	text := strings.Repeat("😀", 3000) // 2 UTF-16 units each: 6000 units
	chunks := Split(text, MessageLimit)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	for i, chunk := range chunks {
		if units := utf16Len(chunk); units > MessageLimit {
			t.Errorf("chunk %d has %d UTF-16 units", i, units)
		}
		if strings.ContainsRune(chunk, '�') {
			t.Errorf("chunk %d contains a broken rune", i)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Error("chunks do not rejoin to the original text")
	}
}

func TestSplitHandlesOddBoundaryWithWideRunes(t *testing.T) {
	text := "a" + strings.Repeat("😀", 10) // boundary falls inside a surrogate pair at limit 4
	chunks := Split(text, 4)
	for i, chunk := range chunks {
		if utf16Len(chunk) > 4 {
			t.Errorf("chunk %d = %q exceeds the limit", i, chunk)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Error("chunks do not rejoin to the original text")
	}
}
