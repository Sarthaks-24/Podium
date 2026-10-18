package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

func TestCompileKeyDependsOnLanguageAndSource(t *testing.T) {
	cpp, py := judge.Languages["cpp17"], judge.Languages["python3"]
	src := []byte("int main(){}")
	if compileKey(cpp, src) != compileKey(cpp, src) {
		t.Error("key is not stable")
	}
	if compileKey(cpp, src) == compileKey(py, src) || compileKey(cpp, src) == compileKey(cpp, []byte("int main(){ }")) {
		t.Error("key ignores the language or the source")
	}
	if compileKey(cpp, src) == compileKey(judge.CheckerLanguage(), src) {
		t.Error("key ignores the compile budget")
	}
}

func TestEvictionKeepsRecentEntries(t *testing.T) {
	dir := t.TempDir()
	c := &CompileCache{Dir: dir, MaxBytes: 1500}
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Join(p, "bin"), 0o700)
		os.WriteFile(filepath.Join(p, "bin", "main"), make([]byte, 1000), 0o600)
		ts := time.Now().Add(-age)
		os.Chtimes(p, ts, ts)
	}
	write("old", time.Hour)
	write("older", 2*time.Hour)
	write("fresh", time.Minute)
	c.Evict()
	for name, want := range map[string]bool{"older": false, "old": false, "fresh": true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s kept=%v, want %v", name, err == nil, want)
		}
	}
}

func TestEvictionSkipsEntriesInUse(t *testing.T) {
	dir := t.TempDir()
	c := &CompileCache{Dir: dir, MaxBytes: 500}
	for _, name := range []string{"busy", "idle"} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Join(p, "bin"), 0o700)
		os.WriteFile(filepath.Join(p, "bin", "main"), make([]byte, 1000), 0o600)
		old := time.Now().Add(-time.Hour)
		os.Chtimes(p, old, old)
	}
	release := c.acquire("busy")
	c.Evict()
	if _, err := os.Stat(filepath.Join(dir, "busy")); err != nil {
		t.Error("an entry in use was evicted")
	}
	if _, err := os.Stat(filepath.Join(dir, "idle")); err == nil {
		t.Error("an idle entry over the cap was kept")
	}
	release()
	c.Evict()
	if _, err := os.Stat(filepath.Join(dir, "busy")); err == nil {
		t.Error("a released entry over the cap was kept")
	}
}
