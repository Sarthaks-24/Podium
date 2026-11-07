package problems

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

// The content hashes of the example packs as computed on main 42ef60b
// (v2.0.1), before function problems joined ContentHash. A contest pins a
// revision by this hash and validation is kept by it, so a change here
// would unvalidate every problem in the bank and move every pin. New
// fields join the hash only when they are set (v2.0.2, V202-32).
var pinnedHashes = map[string]string{
	"a-plus-b":      "5d619b5ed61a0990340b4f8146b036c76409dfa8ce1f4e0bce797bb20561107a",
	"average-speed": "10f8d64cddfb217546aa7fa83eb7aa6bee36411a3d7f4ded59706a33506c7bb7",
	"pair-with-sum": "1e3a250004e9f1c7b57f16099b89d26fdb6cdbc40c52b64a24c9a8ef77b74e1e",
}

func TestOldContentHashesDoNotMove(t *testing.T) {
	for slug, want := range pinnedHashes {
		pk, err := ReadPack(zipDir(t, filepath.Join(examples, slug)))
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		p := pk.Problem
		for i := range p.Tests {
			p.Tests[i].InputSHA = SHA(pk.Inputs[p.Tests[i].Idx])
			p.Tests[i].OutputSHA = SHA(pk.Outputs[p.Tests[i].Idx])
		}
		if got := ContentHash(&p); got != want {
			t.Errorf("%s: content hash %s, pinned %s", slug, got, want)
		}
		// Starters stay outside the hash: a whole-program problem with
		// starters keeps the same pin.
		p.Starters = map[string]string{"cpp17": "int main() {}\n"}
		if got := ContentHash(&p); got != want {
			t.Errorf("%s with starters: content hash %s, pinned %s", slug, got, want)
		}
	}
}

// A digest of each example pack as exported on main 42ef60b, plain
// (format 1) and with two starters (format 3): every file's name and bytes
// in archive order. Formats 1 and 3 must export byte for byte as they did,
// so an older server still reads them. The digest is over the unpacked
// files, not the zip, so a change in Go's deflate cannot break it.
var pinnedExports = map[string][2]string{
	"a-plus-b":      {"a93e2fef8dd5cb08d86f2c9ab77317bcc9972a662682d07e77c06a1f78d171ac", "102db54230c86f6b922647220f36e9e1ca48cf9596ab32c89503a67fdc22de8b"},
	"average-speed": {"05569b38c92e7ef875d3f5bcfe2f47bdac2931854ed289fb5c743a29c6051a0c", "8b4b3a72a4807e3f87a079b3c15de818444b110a723bbb2238ba5204555cbccd"},
	"pair-with-sum": {"ade0b6e36096039f1d21a2498d32fc8f512bb349b79b79649124eb9bb7b334a6", "11858df65b198185ffa8ee812a4be947c36c7170bebaf191a0de36e5ba076258"},
}

func TestOldPacksExportByteIdentical(t *testing.T) {
	for slug, want := range pinnedExports {
		pk, err := ReadPack(zipDir(t, filepath.Join(examples, slug)))
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		p := pk.Problem
		blobs := map[string][]byte{}
		for i := range p.Tests {
			in, out := pk.Inputs[p.Tests[i].Idx], pk.Outputs[p.Tests[i].Idx]
			p.Tests[i].InputSHA, p.Tests[i].OutputSHA = SHA(in), SHA(out)
			blobs[SHA(in)], blobs[SHA(out)] = in, out
		}
		read := func(sha string) ([]byte, error) { return blobs[sha], nil }
		for i, starters := range []map[string]string{nil, {"cpp17": "int main() {\n}\n", "python3": "import sys\n"}} {
			p.Starters = starters
			var buf bytes.Buffer
			if err := WritePack(&buf, &p, read); err != nil {
				t.Fatal(err)
			}
			if got := packDigest(t, buf.Bytes()); got != want[i] {
				t.Errorf("%s export %d: digest %s, pinned %s", slug, i, got, want[i])
			}
		}
	}
}

// packDigest hashes a pack's file names and contents in archive order.
func packDigest(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var all bytes.Buffer
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		fmt.Fprintf(&all, "%s %d\n", f.Name, len(b))
		all.Write(b)
	}
	return SHA(all.Bytes())
}
