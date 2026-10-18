package judge

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A sandboxed program owns its output file. Whatever it does to it, the
// result is output (empty or cut short), never a judge error: otherwise a
// program could fail chosen tests on purpose, a one-bit oracle.
func TestBoxOutputTheProgramBrokeIsEmptyNotFatal(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out")
	for name, src := range map[string]string{
		"missing":   filepath.Join(dir, "nope.txt"),
		"directory": dir,
	} {
		n, err := copyBoxOutput(src, dst, 1<<20)
		if err != nil || n != 0 {
			t.Errorf("%s: n=%d err=%v", name, n, err)
		}
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := filepath.Join(dir, "output.txt")
		os.WriteFile(locked, []byte("42\n"), 0o644)
		os.Chmod(locked, 0)
		n, err := copyBoxOutput(locked, dst, 1<<20)
		if err != nil || n != 0 {
			t.Errorf("chmod 000 output: n=%d err=%v", n, err)
		}
	}
}

type failingReader struct{ sent bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if !f.sent {
		f.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("permission denied")
}

func TestBoxReaderEndsAtAReadError(t *testing.T) {
	var sb strings.Builder
	n, err := io.Copy(&sb, &boxReader{r: &failingReader{}})
	if err != nil || n != 7 || sb.String() != "partial" {
		t.Errorf("n=%d err=%v got %q", n, err, sb.String())
	}
}

func TestBoxOutputHostWriteFailureIsFatal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "output.txt")
	os.WriteFile(src, []byte("1\n"), 0o644)
	if _, err := copyBoxOutput(src, filepath.Join(dir, "missing-dir", "out"), 1<<20); err == nil {
		t.Error("a destination that cannot be written must fail the job")
	}
}
