//go:build !linux

package ops

// diskFree is unknown off Linux; the server only runs on Linux, and the Live
// tab says so for a local Windows build.
func diskFree(string) (uint64, bool) { return 0, false }
