//go:build !unix

package judge

import "os"

// openBoxFile is the non-Unix fallback, used only so the package builds and
// unit-tests on Windows. isolate exists only on Linux, where
// boxfile_unix.go applies.
func openBoxFile(path string) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return os.Open(path)
}
