package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"sync"
	"time"
)

// Record is what the driver writes for each request. cmd/loadcompare reads
// the fields it knows; the rest are for people.
type Record struct {
	Seq         int    `json:"seq"`
	Program     string `json:"program"`
	Kind        string `json:"kind"`
	Language    string `json:"language"`
	Graded      bool   `json:"graded"`
	Heavy       bool   `json:"heavy"`
	NearLimit   bool   `json:"near_limit"`
	Rush        bool   `json:"rush"`
	Expected    string `json:"expected"`
	Verdict     string `json:"verdict"`
	ID          int64  `json:"id"`
	SubmittedMS int64  `json:"submitted_ms"`
	DoneMS      int64  `json:"done_ms"`
	TimeMS      int64  `json:"time_ms"` // slowest test, as the API reports it
	Detail      string `json:"detail,omitempty"`
}

// Driver sends a replay to a podium server in testing mode and waits for
// every verdict.
type Driver struct {
	Base, Origin string
	Students     int
	Poll         time.Duration // between two reads of a pending result
	Timeout      time.Duration // per request, from first attempt to verdict
	Tail         time.Duration // the contest closes this long before the replay ends
	transport    *http.Transport
}

type session struct {
	hc *http.Client
	d  *Driver
}

func (d *Driver) newSession() *session {
	jar, _ := cookiejar.New(nil)
	if d.transport == nil {
		d.transport = &http.Transport{MaxIdleConnsPerHost: 64}
	}
	return &session{hc: &http.Client{Jar: jar, Transport: d.transport, Timeout: 60 * time.Second}, d: d}
}

// call sends one JSON request and returns the status, headers and body.
func (s *session) call(ctx context.Context, method, path string, body any) (int, http.Header, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.d.Base+path, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Origin", s.d.Origin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, resp.Header, b, err
}

func (s *session) signIn(ctx context.Context, email, role string) error {
	code, _, b, err := s.call(ctx, "POST", "/api/auth/test-signin", map[string]string{"email": email, "name": email, "role": role, "next": "/"})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("test sign-in as %s: %d %s; is %s a testing-mode server?", email, code, b, s.d.Base)
	}
	return nil
}

// contest is a fresh contest holding the corpus's problems.
type contest struct {
	slug   string
	labels map[string]string // pack slug to label
	endsAt time.Time
}

// setup makes the contest the replay runs in: open to everyone, no gap
// between submissions, closing close from now.
func (d *Driver) setup(ctx context.Context, c *Corpus, close time.Duration) (*contest, error) {
	owner := d.newSession()
	if err := owner.signIn(ctx, "owner@podium.test", "owner"); err != nil {
		return nil, err
	}
	code, _, b, err := owner.call(ctx, "GET", "/api/admin/problems", nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("list problems: %d %s %v", code, b, err)
	}
	var list struct {
		Problems []struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	ids := map[string]int64{}
	for _, p := range list.Problems {
		ids[p.Slug] = p.ID
	}
	ct := &contest{labels: map[string]string{}}
	var problems []map[string]any
	for _, p := range c.Programs {
		if _, ok := ct.labels[p.Problem]; ok {
			continue
		}
		id, ok := ids[p.Problem]
		if !ok {
			return nil, fmt.Errorf("problem %s is missing; run tools/loadtest/seed.py first", p.Problem)
		}
		label := string(rune('A' + len(problems)))
		ct.labels[p.Problem] = label
		problems = append(problems, map[string]any{"problem_id": id, "label": label})
	}
	now := time.Now()
	ct.endsAt = now.Add(close)
	edit := map[string]any{
		"title": fmt.Sprintf("Replay %d", now.Unix()), "starts_at": now.Add(-5 * time.Second).UnixMilli(), "ends_at": ct.endsAt.UnixMilli(),
		"min_gap_s": 0, "max_per_problem": 1000, "audience": "everyone",
	}
	code, _, b, err = owner.call(ctx, "POST", "/api/admin/contests", edit)
	if err != nil || code != 201 {
		return nil, fmt.Errorf("create contest: %d %s %v", code, b, err)
	}
	var made struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(b, &made); err != nil {
		return nil, err
	}
	ct.slug = made.Slug
	if code, _, b, err = owner.call(ctx, "PUT", fmt.Sprintf("/api/admin/contests/%d/problems", made.ID), problems); err != nil || code != 200 {
		return nil, fmt.Errorf("set problems: %d %s %v", code, b, err)
	}
	if code, _, b, err = owner.call(ctx, "PUT", fmt.Sprintf("/api/admin/contests/%d", made.ID), map[string]any{"published": true}); err != nil || code != 200 {
		return nil, fmt.Errorf("publish: %d %s %v", code, b, err)
	}
	return ct, nil
}

// Replay sends items at their offsets and returns one record per item, in
// item order. total is the length of the replay; the contest closes Tail
// before it ends. A request that cannot be made or judged is recorded with
// an empty verdict and a detail, never dropped.
func (d *Driver) Replay(ctx context.Context, c *Corpus, items []Item, offsets []time.Duration, firstBurst int, total time.Duration) ([]Record, error) {
	ct, err := d.setup(ctx, c, total-d.Tail)
	if err != nil {
		return nil, err
	}
	students := make([]*session, d.Students)
	for i := range students {
		students[i] = d.newSession()
		if err := students[i].signIn(ctx, fmt.Sprintf("replay%04d@load.test", i+1), "student"); err != nil {
			return nil, err
		}
		code, _, b, err := students[i].call(ctx, "POST", "/api/contests/"+ct.slug+"/start", map[string]bool{"accept_honour": true})
		if err != nil || code != 200 {
			return nil, fmt.Errorf("start contest: %d %s %v", code, b, err)
		}
	}
	recs := make([]Record, len(items))
	var wg sync.WaitGroup
	start := time.Now()
	for i, it := range items {
		select {
		case <-time.After(time.Until(start.Add(offsets[i]))):
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int, it Item) {
			defer wg.Done()
			recs[i] = d.one(ctx, students[i%len(students)], ct, it, i >= firstBurst)
		}(i, it)
	}
	wg.Wait()
	return recs, ctx.Err()
}

// one sends a request, retrying while the server asks it to wait, and polls
// until the result is final.
func (d *Driver) one(ctx context.Context, s *session, ct *contest, it Item, rush bool) Record {
	p := it.Program
	rec := Record{Seq: it.Seq, Program: p.ID, Kind: it.Kind, Language: p.Language, Heavy: p.Heavy, NearLimit: p.NearLimit,
		Rush: rush, Expected: p.Expected[it.Kind], SubmittedMS: time.Now().UnixMilli()}
	deadline := time.Now().Add(d.Timeout)
	path := fmt.Sprintf("/api/contests/%s/problems/%s/%s", ct.slug, ct.labels[p.Problem], it.Kind)
	body := map[string]any{"language": p.Language, "source": p.Source}
	if it.Kind == "submit" {
		body["client_key"] = fmt.Sprintf("replay-%d", it.Seq)
		body["typed_chars"] = 300
	}
	var accepted time.Time
	for {
		code, hdr, b, err := s.call(ctx, "POST", path, body)
		if err == nil && (code == 202 || code == 200) {
			accepted = time.Now()
			var out struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(b, &out) != nil || out.ID == 0 {
				rec.Detail = "no id in " + string(b)
				return rec
			}
			rec.ID = out.ID
			break
		}
		if err == nil && code == 429 && time.Now().Before(deadline) {
			wait, _ := strconv.Atoi(hdr.Get("Retry-After"))
			if !sleep(ctx, time.Duration(max(wait, 1))*time.Second) {
				rec.Detail = "stopped"
				return rec
			}
			continue
		}
		rec.Detail = fmt.Sprintf("send: %d %s %v", code, b, err)
		return rec
	}
	rec.Graded = it.Kind == "submit" && accepted.Before(ct.endsAt)
	read := fmt.Sprintf("/api/runs/%d", rec.ID)
	if it.Kind == "submit" {
		read = fmt.Sprintf("/api/submissions/%d", rec.ID)
	}
	for {
		if !sleep(ctx, d.Poll) {
			rec.Detail = "stopped"
			return rec
		}
		code, _, b, err := s.call(ctx, "GET", read, nil)
		if err == nil && code == 200 {
			if v, ms, final, detail := verdictOf(it.Kind, b); final {
				rec.Verdict, rec.TimeMS, rec.Detail, rec.DoneMS = v, ms, detail, time.Now().UnixMilli()
				return rec
			}
		}
		if time.Now().After(deadline) {
			rec.Detail = "no verdict before the timeout"
			return rec
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

var errNotJSON = errors.New("not json")

// verdictOf reads a Run or a Submission. A run's verdict is AC when every
// example passed and the first failing example's verdict otherwise; CE when
// it did not compile. final is false while the job is queued or running.
func verdictOf(kind string, b []byte) (verdict string, timeMS int64, final bool, detail string) {
	if kind == "submit" {
		var s struct {
			State, Verdict, Error string
			MaxTimeMS             int64 `json:"max_time_ms"`
		}
		if json.Unmarshal(b, &s) != nil {
			return "", 0, false, errNotJSON.Error()
		}
		switch s.State {
		case "done":
			return s.Verdict, s.MaxTimeMS, true, ""
		case "error":
			return "", 0, true, "judge error: " + s.Error
		}
		return "", 0, false, ""
	}
	var r struct {
		State, Error  string
		CompileOutput string `json:"compile_output"`
		Results       []struct {
			Verdict string `json:"verdict"`
			TimeMS  int64  `json:"time_ms"`
		} `json:"results"`
	}
	if json.Unmarshal(b, &r) != nil {
		return "", 0, false, errNotJSON.Error()
	}
	switch r.State {
	case "error":
		return "", 0, true, "judge error: " + r.Error
	case "done":
	default:
		return "", 0, false, ""
	}
	verdict = "AC"
	judged := 0
	for _, x := range r.Results {
		if x.Verdict != "" {
			judged++
		}
		timeMS = max(timeMS, x.TimeMS)
		if x.Verdict != "" && x.Verdict != "AC" && verdict == "AC" {
			verdict = x.Verdict
		}
	}
	if judged == 0 && r.CompileOutput != "" {
		return "CE", 0, true, ""
	}
	return verdict, timeMS, true, ""
}
