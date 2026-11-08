package problems

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The compatibility corpus (v2.0.2 PLAN 4a): the three example packs in
// problems/ plus one format 3 pack with starters, all synthetic. golden.json
// holds, for each pack, the ContentHash and a digest of the entries the
// exporter writes, both captured on main 42ef60b before any v2.0.2 change.
// Old packs must keep importing, exporting the same entries and hashing the
// same, so a stored content_hash and an old export never move.
const corpusDir = "testdata/corpus"

type corpusGolden struct {
	ContentHash  string `json:"content_hash"`
	ExportDigest string `json:"export_digest"`
}

func loadGolden(t *testing.T) map[string]corpusGolden {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]corpusGolden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// corpusSource returns the zip bytes of a corpus pack: a folder in problems/
// for the examples, a committed zip for the format 3 pack.
func corpusSource(t *testing.T, name string) []byte {
	t.Helper()
	if name == "format3-starters" {
		b, err := os.ReadFile(filepath.Join(corpusDir, "format3-starters.zip"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return zipDir(t, filepath.Join(examples, name))
}

// exportPack imports data and exports it again the way the server does,
// returning the content hash and the exported zip.
func exportPack(t *testing.T, data []byte) (string, []byte) {
	t.Helper()
	pk, err := ReadPack(data)
	if err != nil {
		t.Fatal(err)
	}
	p := pk.Problem
	blobs := map[string][]byte{}
	for i := range p.Tests {
		in, out := pk.Inputs[p.Tests[i].Idx], pk.Outputs[p.Tests[i].Idx]
		p.Tests[i].InputSHA, p.Tests[i].OutputSHA = SHA(in), SHA(out)
		blobs[SHA(in)], blobs[SHA(out)] = in, out
	}
	var buf bytes.Buffer
	if err := WritePack(&buf, &p, func(sha string) ([]byte, error) { return blobs[sha], nil }); err != nil {
		t.Fatal(err)
	}
	return ContentHash(&p), buf.Bytes()
}

// zipDigest hashes every entry's name and bytes in archive order. It ignores
// the compressor's framing, so a Go upgrade cannot fail the test.
func zipDigest(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		fmt.Fprintf(h, "%s\x00%d\x00", f.Name, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestCorpusGoldenContentHash(t *testing.T) {
	golden := loadGolden(t)
	for _, name := range []string{"a-plus-b", "pair-with-sum", "average-speed", "format3-starters"} {
		hash, exported := exportPack(t, corpusSource(t, name))
		want, ok := golden[name]
		if !ok {
			t.Fatalf("%s: no golden entry", name)
		}
		if hash != want.ContentHash {
			t.Errorf("%s: content hash moved: got %s, want %s", name, hash, want.ContentHash)
		}
		if got := zipDigest(t, exported); got != want.ExportDigest {
			t.Errorf("%s: export entries changed: got %s, want %s", name, got, want.ExportDigest)
		}
	}
}

// An export imports again and exports the same entries: nothing is lost or
// reordered on the way through.
func TestCorpusRoundTripIsIdentical(t *testing.T) {
	for _, name := range []string{"a-plus-b", "pair-with-sum", "average-speed", "format3-starters"} {
		h1, first := exportPack(t, corpusSource(t, name))
		h2, second := exportPack(t, first)
		if h1 != h2 || zipDigest(t, first) != zipDigest(t, second) {
			t.Errorf("%s: import then export then import then export changed the pack", name)
		}
	}
	// The committed format 3 pack is exactly what the exporter writes.
	src := corpusSource(t, "format3-starters")
	_, out := exportPack(t, src)
	if zipDigest(t, src) != zipDigest(t, out) {
		t.Error("format3-starters.zip is not what today's export writes")
	}
}
