package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// shortPolls shrinks the idle poll and the stop grace for a test.
func shortPolls(t *testing.T, poll, grace time.Duration) {
	t.Helper()
	oldPoll, oldGrace := idlePoll, stopGrace
	idlePoll, stopGrace = poll, grace
	t.Cleanup(func() { idlePoll, stopGrace = oldPoll, oldGrace })
}

// claimLog is a stand-in server that records each claim and answers 204
// after hold.
type claimLog struct {
	mu    sync.Mutex
	reqs  []judgeapi.ClaimRequest
	times []time.Time
}

func (c *claimLog) server(t *testing.T, hold time.Duration, status int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req judgeapi.ClaimRequest
		json.NewDecoder(r.Body).Decode(&req)
		c.mu.Lock()
		c.reqs = append(c.reqs, req)
		c.times = append(c.times, time.Now())
		c.mu.Unlock()
		select {
		case <-time.After(hold):
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *claimLog) snapshot() ([]judgeapi.ClaimRequest, []time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]judgeapi.ClaimRequest(nil), c.reqs...), append([]time.Time(nil), c.times...)
}

func runLane(t *testing.T, url string, d time.Duration) {
	t.Helper()
	w := &Worker{Client: &Client{BaseURL: url, Token: "t", HTTP: http.DefaultClient}, Lanes: 1, Name: "w",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	stop, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	w.Drain(stop, context.Background())
}

func TestTheClaimAsksTheServerToHold(t *testing.T) {
	var log claimLog
	srv := log.server(t, 0, http.StatusNoContent)
	runLane(t, srv.URL, 50*time.Millisecond)
	reqs, _ := log.snapshot()
	if len(reqs) == 0 || reqs[0].WaitS != judgeapi.MaxClaimWait || reqs[0].Protocol != judgeapi.ProtocolVersion {
		t.Fatalf("claims %+v, want wait_s %d and protocol %d", reqs, judgeapi.MaxClaimWait, judgeapi.ProtocolVersion)
	}
}

// After a held claim ends empty the lane asks again at once; the hold was
// the wait. After an empty answer that came back at once (a server that did
// not hold) it still waits out the idle poll, so it cannot spin.
func TestNoSleepAfterAHeldEmptyClaimButAnInstantOneStillWaits(t *testing.T) {
	shortPolls(t, 100*time.Millisecond, time.Second)
	for _, tc := range []struct {
		name        string
		hold        time.Duration
		minGap, max time.Duration
	}{
		{"held longer than the idle poll", 130 * time.Millisecond, 0, 60 * time.Millisecond},
		{"answered at once", 0, 100 * time.Millisecond, 400 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log claimLog
			srv := log.server(t, tc.hold, http.StatusNoContent)
			runLane(t, srv.URL, 450*time.Millisecond)
			_, times := log.snapshot()
			if len(times) < 2 {
				t.Fatalf("%d claims", len(times))
			}
			gap := times[1].Sub(times[0]) - tc.hold
			if gap < tc.minGap || gap > tc.max {
				t.Errorf("second claim %v after the first answered, want %v to %v", gap, tc.minGap, tc.max)
			}
		})
	}
}

func TestErrorsStillBackOff(t *testing.T) {
	shortPolls(t, 100*time.Millisecond, time.Second)
	var log claimLog
	srv := log.server(t, 0, http.StatusInternalServerError)
	runLane(t, srv.URL, 70*time.Millisecond)
	if reqs, _ := log.snapshot(); len(reqs) != 1 {
		t.Errorf("%d claims in 70 ms against a failing server, want 1 and then a back-off", len(reqs))
	}
}

// Stopping while a claim is held ends the hold at once: a drain is not slowed
// by up to 20 s.
func TestStopEndsAHeldClaimAndDrainReturns(t *testing.T) {
	shortPolls(t, 2*time.Second, 20*time.Millisecond)
	r := newDrainRig(t)
	stop, stopClaims := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.w.Drain(stop, context.Background()) }()
	for start := time.Now(); r.jobs.Held() != 1; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatal("the lane never held a claim")
		}
	}
	began := time.Now()
	stopClaims()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Drain returned %v", err)
		}
		if took := time.Since(began); took > 500*time.Millisecond {
			t.Errorf("Drain took %v to return after stop", took)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain did not return after stop while a claim was held")
	}
	if r.jobs.Held() != 0 {
		t.Errorf("held = %d after the drain", r.jobs.Held())
	}
}

// Two lanes hold at once; two jobs queued together reach one lane each.
func TestTwoHoldingLanesGetOneJobEach(t *testing.T) {
	r := newDrainRig(t)
	r.w.Lanes = 2
	stop, stopClaims := context.WithCancel(context.Background())
	defer stopClaims()
	done := make(chan error, 1)
	go func() { done <- r.w.Drain(stop, context.Background()) }()
	for start := time.Now(); r.jobs.Held() != 2; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatalf("held = %d, want both lanes holding", r.jobs.Held())
		}
	}
	a, b := r.enqueue(t), r.enqueue(t)
	got := map[int64]bool{}
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-r.started:
			got[id] = true
		case <-deadline:
			t.Fatalf("started %v, want both jobs %d and %d", got, a, b)
		}
	}
	if !got[a] || !got[b] {
		t.Errorf("started %v, want %d and %d", got, a, b)
	}
	close(r.release)
	stopClaims()
	<-done
}
