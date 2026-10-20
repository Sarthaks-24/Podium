package buildinfo

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteFiveLines(t *testing.T) {
	var b bytes.Buffer
	Write(&b)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d lines: %q", len(lines), b.String())
	}
	for i, key := range []string{"version ", "commit ", "built ", "schema ", "protocol "} {
		if !strings.HasPrefix(lines[i], key) {
			t.Errorf("line %d %q, want %q...", i, lines[i], key)
		}
	}
	if Schema() == "" || lines[3] != "schema "+Schema() {
		t.Errorf("schema %q in %q", Schema(), lines[3])
	}
}
