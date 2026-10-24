package doctext

import (
	"os"
	"testing"
)

// DOCTEXT_SAMPLE, when set, is a file to extract into DOCTEXT_OUT, to
// compare with another tool's text by hand.
func TestSampleFile(t *testing.T) {
	in, out := os.Getenv("DOCTEXT_SAMPLE"), os.Getenv("DOCTEXT_OUT")
	if in == "" {
		t.Skip("no DOCTEXT_SAMPLE")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	text, err := Extract(Kind(in, ""), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
