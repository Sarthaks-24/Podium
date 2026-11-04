package buildinfo

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSixLines(t *testing.T) {
	var b bytes.Buffer
	Write(&b)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("%d lines: %q", len(lines), b.String())
	}
	for i, key := range []string{"version ", "codename ", "commit ", "built ", "schema ", "protocol "} {
		if !strings.HasPrefix(lines[i], key) {
			t.Errorf("line %d %q, want %q...", i, lines[i], key)
		}
	}
	if Schema() == "" || lines[4] != "schema "+Schema() {
		t.Errorf("schema %q in %q", Schema(), lines[4])
	}
}

func TestRelease(t *testing.T) {
	defer func(v, c string) { Version, Codename = v, c }(Version, Codename)
	Version, Codename = "v2.0.0", ""
	if got := Release(); got != "v2.0.0" {
		t.Errorf("no codename: %q", got)
	}
	Codename = "Billota"
	if got := Release(); got != "v2.0.0 Billota" {
		t.Errorf("with codename: %q", got)
	}
}
