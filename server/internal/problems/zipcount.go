package problems

import (
	"bytes"
	"encoding/binary"
)

// zipEntryCount reads the number of entries a zip declares in its end of
// central directory record (following the ZIP64 locator when present),
// without building the directory. archive/zip allocates every entry before
// returning, so a 32 MB zip declaring 700,000 tiny entries would cost a few
// hundred MB of RAM before any cap could apply. ok is false when no record
// is found; zip.NewReader then reports the damage.
func zipEntryCount(data []byte) (n uint64, ok bool) {
	const (
		eocdSig     = 0x06054b50
		eocdLen     = 22
		locatorSig  = 0x07064b50
		locatorLen  = 20
		zip64EOCD   = 0x06064b50
		maxComments = 0xffff
	)
	start := max(0, len(data)-eocdLen-maxComments)
	sig := []byte{0x50, 0x4b, 0x05, 0x06}
	i := bytes.LastIndex(data[start:], sig)
	if i < 0 {
		return 0, false
	}
	i += start
	if len(data)-i < eocdLen || binary.LittleEndian.Uint32(data[i:]) != eocdSig {
		return 0, false
	}
	n = uint64(binary.LittleEndian.Uint16(data[i+10:]))
	if n != 0xffff {
		return n, true
	}
	// ZIP64: the locator sits just before the classic record.
	l := i - locatorLen
	if l < 0 || binary.LittleEndian.Uint32(data[l:]) != locatorSig {
		return n, true
	}
	off := binary.LittleEndian.Uint64(data[l+8:])
	if off > uint64(len(data)-56) || binary.LittleEndian.Uint32(data[off:]) != zip64EOCD {
		return 0, false
	}
	return binary.LittleEndian.Uint64(data[off+32:]), true
}
