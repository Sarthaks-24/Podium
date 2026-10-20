package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// An older judge (protocol 1, no field at all) is refused with 426 and
// the sentence naming both versions (B1.3).
func TestServerRefusesAnotherProtocol(t *testing.T) {
	r := newDrainRig(t)
	for _, body := range []string{
		`{"kinds":["validate"],"worker":"old","version":"v1.0.0","isolate":"2.7"}`,
		`{"kinds":["validate"],"worker":"new","version":"v9","isolate":"2.7","protocol":99}`,
	} {
		req, _ := http.NewRequest("POST", r.w.Client.BaseURL+"/api/judge/claim", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+drainToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUpgradeRequired || !strings.Contains(string(b), "judge protocol 2 needed, this judge speaks") ||
			!strings.Contains(string(b), "install the same release on both") {
			t.Errorf("%s: %d %s", body, resp.StatusCode, b)
		}
	}
}

// The judge stops on a 426 with the server's sentence instead of
// retrying: one claim per lane, then Drain returns the error.
func TestJudgeStopsOnProtocolMismatch(t *testing.T) {
	var claims atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims.Add(1)
		var req judgeapi.ClaimRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Protocol != judgeapi.ProtocolVersion {
			t.Errorf("claim sent protocol %d", req.Protocol)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		io.WriteString(w, `{"error":{"code":"protocol","message":"judge protocol 3 needed, this judge speaks 2: install the same release on both"}}`)
	}))
	defer srv.Close()
	var logs bytes.Buffer
	w := &Worker{Client: &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}, Lanes: 2,
		Log: slog.New(slog.NewTextHandler(&logs, nil))}
	done := make(chan error, 1)
	go func() { done <- w.Drain(context.Background(), context.Background()) }()
	select {
	case err := <-done:
		var pe *ProtocolError
		if !errors.As(err, &pe) || pe.Message != "judge protocol 3 needed, this judge speaks 2: install the same release on both" {
			t.Errorf("drain returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the judge kept running after a 426")
	}
	if n := claims.Load(); n > 2 {
		t.Errorf("%d claims: the judge retried", n)
	}
	if !strings.Contains(logs.String(), "install the same release on both") {
		t.Errorf("log: %s", logs.String())
	}
}

// A judge newer than the server: the old server refuses the unknown
// fields with 400, or has no toolchain route (404). Both stop the judge.
func TestJudgeStopsOnAnOlderServer(t *testing.T) {
	for _, c := range []struct {
		status int
		call   func(*Client) error
	}{
		{400, func(c *Client) error { _, err := c.Claim(context.Background(), judgeapi.ClaimRequest{}); return err }},
		{404, func(c *Client) error {
			_, err := c.ReportToolchain(context.Background(), judgeapi.Toolchain{})
			return err
		}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, `{"error":{"code":"bad_request","message":"The body is not valid JSON for this route."}}`)
		}))
		err := c.call(&Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()})
		srv.Close()
		var pe *ProtocolError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "older release than this judge") {
			t.Errorf("%d: %v", c.status, err)
		}
	}
}

// The toolchain report: distinct reports are stored once, and a claimed
// job carries the toolchain of the judge that claimed it (B1.4).
func TestToolchainStoredOnceAndOnEachJob(t *testing.T) {
	r := newDrainRig(t)
	ctx := context.Background()
	a := judgeapi.Toolchain{GCC: "g++ (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0", Java: "javac 21.0.8", Python: "Python 3.12.3",
		Node: "v22.20.0", Isolate: "The process isolator 2.7", Kernel: "6.8.0-85-generic"}
	b := a
	b.GCC = "g++ (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0"
	h1, err := r.w.Client.ReportToolchain(ctx, a)
	if err != nil || h1 != a.Hash() {
		t.Fatalf("report: %q %v", h1, err)
	}
	if h, _ := r.w.Client.ReportToolchain(ctx, a); h != h1 {
		t.Errorf("same toolchain, new hash %q", h)
	}
	h2, _ := r.w.Client.ReportToolchain(ctx, b)
	var n int
	r.jobs.DB.QueryRow("SELECT count(*) FROM toolchains").Scan(&n)
	if n != 2 || h2 == h1 {
		t.Errorf("%d toolchains stored, hashes %s %s", n, h1, h2)
	}
	if cur, err := r.jobs.CurrentToolchain(ctx); err != nil || cur == nil || cur.Hash != h2 || cur.Details.GCC != b.GCC {
		t.Errorf("current %+v %v", cur, err)
	}
	if _, err := r.w.Client.ReportToolchain(ctx, judgeapi.Toolchain{GCC: strings.Repeat("x", 300)}); err == nil {
		t.Error("an oversized report was stored")
	}

	id := r.enqueue(t)
	r.w.Toolchain = h1
	stop, stopClaims := context.WithCancel(ctx)
	done, _ := r.start(t, stop, ctx)
	stopClaims()
	close(r.release)
	<-done
	var hash string
	if err := r.jobs.DB.QueryRow("SELECT t.hash FROM jobs j JOIN toolchains t ON t.id = j.toolchain_id WHERE j.id = ?", id).Scan(&hash); err != nil || hash != h1 {
		t.Errorf("job toolchain %q %v", hash, err)
	}
}
