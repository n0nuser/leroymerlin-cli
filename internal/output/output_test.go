package output

import (
	"bytes"
	"strings"
	"testing"
)

// Remote text (store and product names) must not drive the terminal.
func TestTableStripsControlSequences(t *testing.T) {
	var b bytes.Buffer
	Table(&b, false, nil, [][]string{{"evil\x1b]52;c;cGF3bmVk\x07name\u009b31m", "ok\nline"}})
	out := b.String()
	if strings.ContainsAny(out, "\x1b\x07\u009b") {
		t.Fatalf("control bytes reached the terminal: %q", out)
	}
	if !strings.Contains(out, "ok line") {
		t.Fatalf("newline not flattened: %q", out)
	}
}

func TestJSONLSplitsArrays(t *testing.T) {
	var b bytes.Buffer
	Printer{W: &b, Format: JSONL}.Print([]map[string]int{{"a": 1}, {"a": 2}})
	if b.String() != "{\"a\":1}\n{\"a\":2}\n" {
		t.Fatalf("%q", b.String())
	}
}
