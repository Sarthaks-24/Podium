// Package worker is podium-judge's side of the judge API (decision D44):
// it claims jobs from the server, fetches test files and sources by hash
// into a local cache, runs them in isolate through judge.Runner, and
// reports results as it goes. It never opens the database; everything it
// knows arrives over HTTP with the worker token.
package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// ErrLease means the server no longer recognises this job's lease (it
// expired and went to another worker, or the job was closed). The worker
// drops the job without further reports.
var ErrLease = errors.New("lease lost")

// maxBlob bounds a downloaded file; packs cap tests at 16 MB.
const maxBlob = 17 << 20

// Client talks to the server's judge API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (c *Client) do(ctx context.Context, method, path string, body any, headers ...string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}

// httpError describes a refused request without echoing headers (which
// carry the token).
func httpError(op string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%s: server answered %d: %s", op, resp.StatusCode, bytes.TrimSpace(b))
}

// ProtocolError means the server and this judge come from different
// releases (B1.3). Retrying cannot help, so the worker stops with the
// message; podium-judge exits with status 3, which its unit does not
// restart.
type ProtocolError struct{ Message string }

func (e *ProtocolError) Error() string { return e.Message }

// protocolError reads the server's sentence from a 426, or describes a
// refusal that means the server is older than this judge: a 400 to a
// body it does not know, or a 404 for a route it does not have.
func protocolError(op string, resp *http.Response) *ProtocolError {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body)
	if resp.StatusCode == http.StatusUpgradeRequired && body.Error.Message != "" {
		return &ProtocolError{Message: body.Error.Message}
	}
	return &ProtocolError{Message: fmt.Sprintf("%s: the server answered %d, so it runs an older release than this judge (protocol %d): install the same release on both",
		op, resp.StatusCode, judgeapi.ProtocolVersion)}
}

// Claim asks for one job of the given kinds; nil means none is waiting.
// It sends this build's protocol version.
func (c *Client) Claim(ctx context.Context, req judgeapi.ClaimRequest) (*judgeapi.Claim, error) {
	req.Protocol = judgeapi.ProtocolVersion
	resp, err := c.do(ctx, http.MethodPost, "/api/judge/claim", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var claim judgeapi.Claim
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&claim); err != nil {
			return nil, fmt.Errorf("claim: %w", err)
		}
		return &claim, nil
	case http.StatusUpgradeRequired, http.StatusBadRequest:
		return nil, protocolError("claim", resp)
	}
	return nil, httpError("claim", resp)
}

// ReportToolchain sends the host's toolchain (B1.4) and returns the hash
// to send with each claim.
func (c *Client) ReportToolchain(ctx context.Context, t judgeapi.Toolchain) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/judge/toolchain", judgeapi.ToolchainReport{Protocol: judgeapi.ProtocolVersion, Toolchain: t})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var reply judgeapi.ToolchainReply
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&reply); err != nil || !judgeapi.ValidSHA(reply.Hash) {
			return "", fmt.Errorf("toolchain: the server's answer has no hash")
		}
		return reply.Hash, nil
	case http.StatusUpgradeRequired, http.StatusBadRequest, http.StatusNotFound:
		return "", protocolError("toolchain report", resp)
	}
	return "", httpError("toolchain report", resp)
}

// refusedError is a report the server rejected as malformed (400): sending
// it again will not help.
type refusedError struct{ err error }

func (e *refusedError) Error() string { return e.err.Error() }

// Report sends results for a leased job.
func (c *Client) Report(ctx context.Context, rep judgeapi.Report) error {
	resp, err := c.do(ctx, http.MethodPost, "/api/judge/report", rep)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusConflict:
		return ErrLease
	case http.StatusBadRequest:
		return &refusedError{httpError("report", resp)}
	}
	return httpError("report", resp)
}

// DefaultBlobCacheBytes caps the blob cache when MaxBytes is not set.
const DefaultBlobCacheBytes = 2048 << 20

// Cache keeps downloaded blobs under Dir/<sha[0:2]>/<sha>. A file is only
// stored after its content hashes to its name, so a corrupted download or
// a misbehaving server cannot plant the wrong test.
//
// The cache is bounded: Evict removes the least recently used files above
// MaxBytes, never one a running job holds. A job holds every blob it asked
// for from Path or Read until Release(jobID).
type Cache struct {
	Dir      string
	Client   *Client
	MaxBytes int64

	mu    sync.Mutex
	inUse map[string]int            // sha -> number of jobs holding it
	held  map[int64]map[string]bool // job id -> the shas it holds
	// size is the bytes stored, known once scanned is true; downloads add
	// to it so Evict only walks the directory when the cap may be passed.
	size    int64
	scanned bool
}

// hold marks sha as needed by a job until Release.
func (c *Cache) hold(jobID int64, sha string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.held == nil {
		c.held, c.inUse = map[int64]map[string]bool{}, map[string]int{}
	}
	if c.held[jobID] == nil {
		c.held[jobID] = map[string]bool{}
	}
	if !c.held[jobID][sha] {
		c.held[jobID][sha] = true
		c.inUse[sha]++
	}
}

// Release lets Evict remove the blobs a finished job used.
func (c *Cache) Release(jobID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for sha := range c.held[jobID] {
		if c.inUse[sha]--; c.inUse[sha] <= 0 {
			delete(c.inUse, sha)
		}
	}
	delete(c.held, jobID)
}

// Evict removes least recently used blobs until the cache is under 90 %
// of its cap, skipping blobs a job holds. It walks the directory only
// when the stored total may be over the cap. The worker calls it after
// every job.
func (c *Cache) Evict() {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultBlobCacheBytes
	}
	if c.scanned && c.size <= limit {
		return
	}
	type entry struct {
		path, sha string
		used      time.Time
		size      int64
	}
	var all []entry
	var total int64
	dirs, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() || len(d.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(c.Dir, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !judgeapi.ValidSHA(f.Name()) {
				continue // a download in progress (.dl-*)
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			all = append(all, entry{filepath.Join(c.Dir, d.Name(), f.Name()), f.Name(), info.ModTime(), info.Size()})
			total += info.Size()
		}
	}
	c.size, c.scanned = total, true
	if total <= limit {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].used.Before(all[j].used) })
	target := limit / 10 * 9
	for _, e := range all {
		if c.size <= target {
			break
		}
		if c.inUse[e.sha] > 0 {
			continue
		}
		if os.Remove(e.path) == nil {
			c.size -= e.size
		}
	}
}

// Lease identifies the job a download is for; the server serves only the
// files that job needs.
type Lease struct {
	JobID int64
	Token string
}

// Path returns the local path of a blob, downloading it under lease if
// needed.
func (c *Cache) Path(ctx context.Context, l Lease, sha string) (string, error) {
	if !judgeapi.ValidSHA(sha) {
		return "", fmt.Errorf("blob name %q is not a SHA-256", sha)
	}
	p := filepath.Join(c.Dir, sha[:2], sha)
	// Held before the file is looked at, so Evict cannot remove it
	// between this check and the job using it.
	c.hold(l.JobID, sha)
	if _, err := os.Stat(p); err == nil {
		now := time.Now()
		os.Chtimes(p, now, now) // mark as recently used
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	resp, err := c.Client.do(ctx, http.MethodGet, "/api/judge/blob/"+sha, nil,
		judgeapi.HeaderJob, strconv.FormatInt(l.JobID, 10), judgeapi.HeaderLease, l.Token)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpError("blob "+sha[:12], resp)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".dl-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxBlob+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("blob %s: %w", sha[:12], err)
	}
	if n > maxBlob {
		return "", fmt.Errorf("blob %s is larger than %d MB", sha[:12], maxBlob>>20)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sha {
		return "", fmt.Errorf("blob %s arrived with hash %s", sha[:12], got[:12])
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.size += n
	c.mu.Unlock()
	return p, nil
}

// Read returns a cached blob's bytes, at most limit of them.
func (c *Cache) Read(ctx context.Context, l Lease, sha string, limit int64) ([]byte, error) {
	p, err := c.Path(ctx, l, sha)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("blob %s is larger than %d bytes", sha[:12], limit)
	}
	return b, nil
}
