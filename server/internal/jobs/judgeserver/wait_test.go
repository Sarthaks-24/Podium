package judgeserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

type waitKind struct{}

func (waitKind) CheckReport(json.RawMessage, judgeapi.Report) error { return nil }
func (waitKind) Blobs(json.RawMessage) ([]string, error)            { return nil, nil }
func (waitKind) MaxDuration(json.RawMessage) time.Duration          { return time.Minute }
func (waitKind) Finish(context.Context, *sql.Tx, *jobs.Job) error   { return nil }

const waitToken = "wait-test-token-wait-test-token-00001"

type waitRig struct {
	jobs *jobs.Service
	srv  *httptest.Server
}

func newWaitRig(t *testing.T, stop context.Context) *waitRig {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	j := &jobs.Service{DB: d, Kinds: map[string]jobs.Kind{judgeapi.KindValidate: waitKind{}}, LookEvery: time.Hour}
	api := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Jobs: j, Blobs: problems.Blobs{Dir: t.TempDir()}, Token: waitToken, Stop: stop})
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return &waitRig{jobs: j, srv: srv}
}

func (r *waitRig) claim(ctx context.Context, body string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", r.srv.URL+"/api/judge/claim", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+waitToken)
	return http.DefaultClient.Do(req)
}

func (r *waitRig) enqueue(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	tx, _ := r.jobs.DB.BeginTx(ctx, nil)
	id, err := r.jobs.Enqueue(ctx, tx, judgeapi.KindValidate, 10, map[string]int{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r.jobs.Wake()
	return id
}

func waitUntilHeld(t *testing.T, j *jobs.Service, n int) {
	t.Helper()
	for start := time.Now(); j.Held() != n; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatalf("held = %d, want %d", j.Held(), n)
		}
	}
}

const waitBody = `{"kinds":["validate"],"worker":"w","version":"v","isolate":"2","protocol":4,"wait_s":%d}`

func body(wait string) string {
	return strings.Replace(waitBody, "%d", wait, 1)
}

func TestWaitSOutOfRangeIsRefused(t *testing.T) {
	r := newWaitRig(t, nil)
	for _, w := range []string{"21", "-1", "3600"} {
		resp, err := r.claim(context.Background(), body(w))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(b), "bad_request") || !strings.Contains(string(b), "wait_s") {
			t.Errorf("wait_s %s: %d %s", w, resp.StatusCode, b)
		}
	}
}

// Without wait_s the claim answers at once, as before; a protocol 3 judge is
// refused before wait_s is looked at.
func TestAClaimWithoutWaitSAnswersAtOnce(t *testing.T) {
	r := newWaitRig(t, nil)
	start := time.Now()
	resp, err := r.claim(context.Background(), `{"kinds":["validate"],"worker":"w","version":"v","isolate":"2","protocol":4}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || time.Since(start) > 500*time.Millisecond {
		t.Errorf("%d after %v, want 204 at once", resp.StatusCode, time.Since(start))
	}
	resp, err = r.claim(context.Background(), `{"kinds":["validate"],"worker":"w","version":"v","isolate":"2","protocol":3,"wait_s":5}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("protocol 3: %d, want 426", resp.StatusCode)
	}
}

func TestAHeldClaimIsAnsweredWithTheJob(t *testing.T) {
	r := newWaitRig(t, nil)
	type answer struct {
		code int
		body string
	}
	got := make(chan answer, 1)
	go func() {
		resp, err := r.claim(context.Background(), body("5"))
		if err != nil {
			got <- answer{}
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got <- answer{resp.StatusCode, string(b)}
	}()
	waitUntilHeld(t, r.jobs, 1)
	id := r.enqueue(t)
	select {
	case a := <-got:
		if a.code != http.StatusOK || !strings.Contains(a.body, `"job_id":`+string(rune('0'+id))) {
			t.Errorf("answer %d %s, want 200 with job %d", a.code, a.body, id)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("the held claim was not answered within 100 ms of the enqueue")
	}
}

func TestShutdownAnswersHeldClaimsAtOnce(t *testing.T) {
	stop, shutdown := context.WithCancel(context.Background())
	r := newWaitRig(t, stop)
	code := make(chan int, 1)
	go func() {
		resp, err := r.claim(context.Background(), body("20"))
		if err != nil {
			code <- 0
			return
		}
		resp.Body.Close()
		code <- resp.StatusCode
	}()
	waitUntilHeld(t, r.jobs, 1)
	shutdown()
	select {
	case c := <-code:
		if c != http.StatusNoContent {
			t.Errorf("status %d, want 204", c)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("a held claim outlived the start of shutdown")
	}
}

func TestADisconnectedJudgeFreesItsHeldClaim(t *testing.T) {
	r := newWaitRig(t, nil)
	ctx, gone := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := r.claim(ctx, body("20")); err == nil {
			resp.Body.Close()
		}
	}()
	waitUntilHeld(t, r.jobs, 1)
	gone()
	<-done
	waitUntilHeld(t, r.jobs, 0)
}
