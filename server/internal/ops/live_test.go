package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The newest backup's time comes from the "latest" link podium-backup
// keeps; a folder without one, or no folder, is "no backup yet".
func TestLastBackup(t *testing.T) {
	dir := t.TempDir()
	if _, ok := lastBackup(dir); ok {
		t.Error("a folder with no backups reported one")
	}
	if _, ok := lastBackup(""); ok {
		t.Error("no folder reported a backup")
	}
	if err := os.Mkdir(filepath.Join(dir, "2026-10-30T213000Z"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("2026-10-30T213000Z", filepath.Join(dir, "latest")); err != nil {
		t.Skipf("this machine cannot make a symbolic link: %v", err)
	}
	at, ok := lastBackup(dir)
	if want := time.Date(2026, 10, 30, 21, 30, 0, 0, time.UTC); !ok || !at.Equal(want) {
		t.Errorf("last backup %v %v, want %v", at, ok, want)
	}
}

func TestFileSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	if err := os.WriteFile(p, make([]byte, 3<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fileSize(p) >> 20; got != 3 {
		t.Errorf("size %d MB, want 3", got)
	}
	if got := fileSize(p + "-wal"); got != 0 {
		t.Errorf("a missing file has size %d", got)
	}
}
