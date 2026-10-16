//go:build unix

package judge

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// openBoxFile opens a file the sandboxed program may have created or
// replaced. O_NOFOLLOW refuses a final symlink (which would otherwise make
// the judge read, say, its own /proc/self/environ), O_NONBLOCK keeps a FIFO
// from hanging the lane, and the fstat check rejects anything that is not a
// regular file. Every read of a box path goes through here.
func openBoxFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errNotRegular
		}
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, errNotRegular
	}
	return f, nil
}
