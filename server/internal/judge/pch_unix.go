//go:build unix

package judge

import (
	"os"
	"syscall"
)

// ownerOf is the uid that owns the file.
func ownerOf(fi os.FileInfo) (uid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// sharedWritable is true when the group or others can write to the file.
func sharedWritable(fi os.FileInfo) bool { return fi.Mode().Perm()&0o022 != 0 }
