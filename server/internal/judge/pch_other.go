//go:build !unix

package judge

import "os"

// ownerOf: only Unix has owners; the check is skipped elsewhere.
func ownerOf(os.FileInfo) (int, bool) { return 0, false }

// sharedWritable: Windows modes carry no group or other bits.
func sharedWritable(os.FileInfo) bool { return false }
