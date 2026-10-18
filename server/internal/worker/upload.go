package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// maxUpload is the largest output a generate job uploads (the test-file
// cap in pack imports).
const maxUpload = 16 << 20

// Upload sends a generated output to the server under the job's lease and
// returns its hash. The server accepts uploads only for generate jobs and
// checks the hash itself.
func (c *Client) Upload(ctx context.Context, l Lease, data []byte) (string, error) {
	if len(data) > maxUpload {
		return "", fmt.Errorf("output is larger than %d MB", maxUpload>>20)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/api/judge/blob/"+sha, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(judgeapi.HeaderJob, strconv.FormatInt(l.JobID, 10))
	req.Header.Set(judgeapi.HeaderLease, l.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return sha, nil
	case http.StatusConflict:
		return "", ErrLease
	}
	return "", httpError("upload", resp)
}
