package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/jobs/jobstest"
	"github.com/Sarthaks-24/podium/internal/jobs/judgeserver"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/schedule"
)

// A claim may be held up to MaxClaimWait seconds; no timeout of the judge
// listener may cut it first.
func TestJudgeServerTimeoutsOutlastAHeldClaim(t *testing.T) {
	srv := judgeServer("127.0.0.1:0", http.NotFoundHandler())
	hold := time.Duration(judgeapi.MaxClaimWait) * time.Second
	for name, d := range map[string]time.Duration{"ReadTimeout": srv.ReadTimeout, "WriteTimeout": srv.WriteTimeout, "IdleTimeout": srv.IdleTimeout} {
		if d < hold+10*time.Second {
			t.Errorf("%s is %v, shorter than a %v hold plus margin", name, d, hold)
		}
	}
}

// Shutdown ends held claims at once (they answer 204) instead of waiting
// for the 10 s it gives the servers, and the student API still gets its
// time to finish a request that is in flight.
func TestShutdownReleasesHeldClaimsAndStillDrainsTheStudentAPI(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Server{OwnerEmail: "owner@college.edu", Mode: config.ModeTesting, JudgeToken: config.TestingJudgeToken,
		TestsDir: filepath.Join(dir, "tests"), SessionSecret: []byte("0123456789abcdef0123456789abcdef")}
	svc, err := newServices(ctx, cfg, database, log)
	if err != nil {
		t.Fatal(err)
	}
	judgeSrv := newJudgeServer("", judgeserver.Deps{Log: log, Jobs: svc.jobs, Blobs: svc.blobs, Token: cfg.JudgeToken})
	inFlight := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(inFlight)
		time.Sleep(300 * time.Millisecond) // a student request that is still being served
		io.WriteString(w, "done")
	})}
	serve := func(s *http.Server) string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go s.Serve(l)
		return "http://" + l.Addr().String()
	}
	judgeURL, studentURL := serve(judgeSrv), serve(srv)

	claim := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", judgeURL+"/api/judge/claim",
			strings.NewReader(`{"kinds":["run"],"worker":"w","version":"v","isolate":"2","protocol":4,"wait_s":20}`))
		req.Header.Set("Authorization", "Bearer "+cfg.JudgeToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			claim <- 0
			return
		}
		resp.Body.Close()
		claim <- resp.StatusCode
	}()
	for start := time.Now(); svc.jobs.Held() != 1; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatal("the claim never started waiting")
		}
	}
	student := make(chan string, 1)
	go func() {
		resp, err := http.Get(studentURL)
		if err != nil {
			student <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		student <- string(b)
	}()
	<-inFlight

	began := time.Now()
	shutdown(log, schedule.Start(nil), judgeSrv, srv)
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("shutdown took %v with a claim held; held claims must end at once", took)
	}
	if code := <-claim; code != http.StatusNoContent {
		t.Errorf("the held claim ended with %d, want 204", code)
	}
	if got := <-student; got != "done" {
		t.Errorf("the student request in flight got %q, want it served to the end", got)
	}
}

// The server wires un-pausing a contest to the queue's wake.
func TestResumingAContestIsWiredToTheQueue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	cfg := config.Server{OwnerEmail: "owner@college.edu", Mode: config.ModeTesting,
		TestsDir: filepath.Join(dir, "tests"), SessionSecret: []byte("0123456789abcdef0123456789abcdef")}
	svc, err := newServices(ctx, cfg, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	answer := jobstest.Hold(t, svc.jobs, judgeapi.KindRun)
	if _, err := database.ExecContext(ctx, `INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('run', 10, 'queued', '{"inputs":[]}', 0)`); err != nil {
		t.Fatal(err)
	}
	svc.contests.Wake()
	if answer(100*time.Millisecond) == nil {
		t.Error("the contests service's Wake does not reach the queue")
	}
}
