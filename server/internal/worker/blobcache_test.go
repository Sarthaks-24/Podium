package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blobServer serves fixed files on the judge API's blob route and counts
// downloads.
func blobServer(t *testing.T, files map[string][]byte) (*Client, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/api/judge/blob/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		n.Add(1)
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}, &n
}

func blob(size int, fill byte) (string, []byte) {
	b := []byte(strings.Repeat(string(fill), size))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b
}

func TestBlobCacheEvictsLeastRecentlyUsedNotHeld(t *testing.T) {
	ctx := context.Background()
	files := map[string][]byte{}
	var shas []string
	for _, fill := range []byte{'a', 'b', 'c', 'd'} {
		sha, b := blob(1000, fill)
		files[sha] = b
		shas = append(shas, sha)
	}
	client, downloads := blobServer(t, files)
	c := &Cache{Dir: t.TempDir(), Client: client, MaxBytes: 2500}

	// Job 1 uses a, b and c; a is the oldest, then b, then c.
	for i, sha := range shas[:3] {
		p, err := c.Path(ctx, Lease{JobID: 1}, sha)
		if err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-time.Duration(3-i) * time.Hour)
		os.Chtimes(p, ts, ts)
	}
	// Over the cap, but job 1 still holds everything.
	c.Evict()
	for _, sha := range shas[:3] {
		if !cached(c, sha) {
			t.Fatalf("a held blob was evicted")
		}
	}

	// Job 2 holds b. Once job 1 ends, eviction goes down to 90 % of the
	// cap (2250 bytes), oldest first, skipping b.
	if _, err := c.Path(ctx, Lease{JobID: 2}, shas[1]); err != nil {
		t.Fatal(err)
	}
	c.Release(1)
	c.Evict()
	if cached(c, shas[0]) || !cached(c, shas[1]) || !cached(c, shas[2]) {
		t.Errorf("after eviction: a=%v b=%v c=%v, want a gone and b, c kept", cached(c, shas[0]), cached(c, shas[1]), cached(c, shas[2]))
	}
	if c.size != 2000 {
		t.Errorf("tracked size %d, want 2000", c.size)
	}

	// A hit marks the blob as recently used; a new download adds to the
	// tracked size, and the next eviction removes the least recent idle one.
	before := downloads.Load()
	if _, err := c.Path(ctx, Lease{JobID: 3}, shas[1]); err != nil || downloads.Load() != before {
		t.Fatalf("cached blob downloaded again: %v", err)
	}
	if _, err := c.Path(ctx, Lease{JobID: 3}, shas[3]); err != nil {
		t.Fatal(err)
	}
	if c.size != 3000 {
		t.Errorf("tracked size %d after a download, want 3000", c.size)
	}
	c.Release(2)
	c.Release(3)
	c.Evict()
	if cached(c, shas[2]) || !cached(c, shas[1]) || !cached(c, shas[3]) {
		t.Errorf("second eviction: b=%v c=%v d=%v, want c gone", cached(c, shas[1]), cached(c, shas[2]), cached(c, shas[3]))
	}

	// An evicted blob downloads again when a job needs it.
	before = downloads.Load()
	if _, err := c.Path(ctx, Lease{JobID: 4}, shas[0]); err != nil || downloads.Load() != before+1 {
		t.Errorf("evicted blob not fetched again: %v", err)
	}
}

func TestBlobCacheUnderCapDoesNotWalk(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir, MaxBytes: 1 << 20}
	c.Evict() // first call scans an empty directory
	if !c.scanned || c.size != 0 {
		t.Fatalf("scan: scanned=%v size=%d", c.scanned, c.size)
	}
	// A file appearing behind the cache's back is not seen until the
	// tracked total says the cap may be passed.
	sha, b := blob(10, 'x')
	os.MkdirAll(filepath.Join(dir, sha[:2]), 0o700)
	os.WriteFile(filepath.Join(dir, sha[:2], sha), b, 0o600)
	c.Evict()
	if c.size != 0 {
		t.Errorf("walked the directory while under the cap")
	}
}

func cached(c *Cache, sha string) bool {
	_, err := os.Stat(filepath.Join(c.Dir, sha[:2], sha))
	return err == nil
}
