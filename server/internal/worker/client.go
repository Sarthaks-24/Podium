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
	"strconv"

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

// Claim asks for one job of the given kinds; nil means none is waiting.
func (c *Client) Claim(ctx context.Context, req judgeapi.ClaimRequest) (*judgeapi.Claim, error) {
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
	}
	return nil, httpError("claim", resp)
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

// Cache keeps downloaded blobs under Dir/<sha[0:2]>/<sha>. A file is only
// stored after its content hashes to its name, so a corrupted download or
// a misbehaving server cannot plant the wrong test.
type Cache struct {
	Dir    string
	Client *Client
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
	if _, err := os.Stat(p); err == nil {
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
