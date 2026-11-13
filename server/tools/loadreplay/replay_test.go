package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const repoRoot = "../../.."

func loadRepoCorpus(t *testing.T) *Corpus {
	t.Helper()
	c, err := LoadCorpus(repoRoot, filepath.Join(repoRoot, "tools/loadtest/corpus/corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCorpusManifest(t *testing.T) {
	c := loadRepoCorpus(t)
	var runs, subs int
	verdicts := map[string]bool{}
	for _, p := range c.Programs {
		runs += p.Runs
		subs += p.Submits
		for _, v := range p.Expected {
			verdicts[v] = true
		}
		has := strings.Contains(p.Source, "<bits/stdc++.h>")
		if p.Heavy != (has && p.Language == "cpp17") {
			t.Errorf("%s: heavy = %v but bits/stdc++.h included = %v", p.ID, p.Heavy, has)
		}
		if p.NearLimit && (p.Expected["run"] != "AC" || p.Expected["submit"] != "AC") {
			t.Errorf("%s: a near-limit program must be accepted, got %v", p.ID, p.Expected)
		}
	}
	if runs != 30 || subs != 21 {
		t.Errorf("one round holds %d Runs and %d submissions, want 30 and 21", runs, subs)
	}
	if share := float64(runs) / float64(runs+subs); share < 0.55 || share > 0.62 {
		t.Errorf("Runs are %.0f%% of a round; the measured share is 58%%", 100*share)
	}
	for _, want := range []string{"AC", "WA", "CE", "RE", "TLE"} {
		if !verdicts[want] {
			t.Errorf("no program expects %s", want)
		}
	}
	langs := map[string]bool{}
	heavy, near := 0, 0
	for _, p := range c.Programs {
		langs[p.Language] = true
		if p.Heavy {
			heavy++
		}
		if p.NearLimit {
			near++
		}
	}
	if len(langs) != 4 || heavy < 3 || near < 4 {
		t.Errorf("languages %v, heavy %d, near-limit %d", langs, heavy, near)
	}
}

func TestLoadCorpusRefusals(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("a.cpp", "int main(){}")
	for name, m := range map[string]string{
		"no expected": `{"programs":[{"id":"x","problem":"p","language":"cpp17","file":"a.cpp","runs":1,"expected":{}}]}`,
		"repeated id": `{"programs":[{"id":"x","file":"a.cpp"},{"id":"x","file":"a.cpp"}]}`,
		"no file":     `{"programs":[{"id":"x","file":"missing.cpp"}]}`,
		"bad json":    `{`,
	} {
		if _, err := LoadCorpus(dir, write("m.json", m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := LoadCorpus(dir, filepath.Join(dir, "none.json")); err == nil {
		t.Error("missing manifest accepted")
	}
}

func TestSequenceIsFixed(t *testing.T) {
	c := loadRepoCorpus(t)
	a, b := c.Sequence(3), c.Sequence(3)
	if len(a) != 3*51 {
		t.Fatalf("%d items", len(a))
	}
	count := map[string]int{}
	for i := range a {
		if a[i].Program.ID != b[i].Program.ID || a[i].Kind != b[i].Kind || a[i].Seq != i {
			t.Fatalf("item %d differs between two calls", i)
		}
		count[a[i].Program.ID+"/"+a[i].Kind]++
	}
	for _, p := range c.Programs {
		if count[p.ID+"/run"] != 3*p.Runs || count[p.ID+"/submit"] != 3*p.Submits {
			t.Errorf("%s: %d runs, %d submits in 3 rounds", p.ID, count[p.ID+"/run"], count[p.ID+"/submit"])
		}
	}
	// The first pass visits the programs in manifest order.
	if a[0].Program.ID != c.Programs[0].ID || a[0].Kind != "run" || a[1].Kind != "submit" {
		t.Errorf("first items: %s %s, %s %s", a[0].Program.ID, a[0].Kind, a[1].Program.ID, a[1].Kind)
	}
}

func TestSchedule(t *testing.T) {
	const n = 300
	total, burst := 15*time.Minute, 5*time.Minute
	off, first := Schedule(n, total, burst)
	for i := 1; i < n; i++ {
		if off[i] < off[i-1] {
			t.Fatalf("offsets go backwards at %d", i)
		}
	}
	if off[n-1] >= total || off[0] != 0 {
		t.Errorf("offsets run from %v to %v", off[0], off[n-1])
	}
	if off[first] < total-burst-time.Second || (first > 0 && off[first-1] >= total-burst) {
		t.Errorf("the burst starts at item %d, offset %v", first, off[first])
	}
	steadyRate := float64(first) / (total - burst).Seconds()
	burstRate := float64(n-first) / burst.Seconds()
	if r := burstRate / steadyRate; r < 2.9 || r > 3.1 {
		t.Errorf("burst is %.2f times as dense, want 3", r)
	}
	again, firstAgain := Schedule(n, total, burst)
	if firstAgain != first || again[100] != off[100] {
		t.Error("the schedule is not fixed")
	}
	all, f := Schedule(10, time.Minute, 5*time.Minute)
	if f != 0 || len(all) != 10 {
		t.Errorf("a burst longer than the replay: first %d", f)
	}
}

func TestVerdictOf(t *testing.T) {
	cases := []struct {
		kind, body, verdict string
		ms                  int64
		final               bool
	}{
		{"submit", `{"state":"queued"}`, "", 0, false},
		{"submit", `{"state":"running","verdict":""}`, "", 0, false},
		{"submit", `{"state":"done","verdict":"WA","max_time_ms":120}`, "WA", 120, true},
		{"submit", `{"state":"error","error":"boom"}`, "", 0, true},
		{"run", `{"state":"queued","results":[]}`, "", 0, false},
		{"run", `{"state":"done","results":[{"verdict":"AC","time_ms":10},{"verdict":"AC","time_ms":30}]}`, "AC", 30, true},
		{"run", `{"state":"done","results":[{"verdict":"AC"},{"verdict":"TLE"},{"verdict":"WA"}]}`, "TLE", 0, true},
		{"run", `{"state":"done","compile_output":"error","results":[{"verdict":""}]}`, "CE", 0, true},
		{"run", `{"state":"error","error":"boom"}`, "", 0, true},
		{"run", `not json`, "", 0, false},
	}
	for _, c := range cases {
		v, ms, final, _ := verdictOf(c.kind, []byte(c.body))
		if v != c.verdict || ms != c.ms || final != c.final {
			t.Errorf("%s %s: got %q %d %v", c.kind, c.body, v, ms, final)
		}
	}
}

// fakeServer plays the parts of the API the driver uses. Results are
// final on the second read. The first submission is refused once with 429.
type fakeServer struct {
	mu       sync.Mutex
	next     int64
	reads    map[string]int
	verdicts map[int64]string // by id: what the job says
	refused  atomic.Bool
	closeAt  time.Time
	failCE   bool
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, code int, v any) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /api/auth/test-signin", func(w http.ResponseWriter, r *http.Request) { ok(w, 200, map[string]any{}) })
	mux.HandleFunc("GET /api/admin/problems", func(w http.ResponseWriter, r *http.Request) {
		ok(w, 200, map[string]any{"problems": []map[string]any{{"id": 1, "slug": "a-plus-b"}, {"id": 2, "slug": "average-speed"},
			{"id": 3, "slug": "pair-with-sum"}, {"id": 4, "slug": "largest-gap"}}})
	})
	mux.HandleFunc("POST /api/admin/contests", func(w http.ResponseWriter, r *http.Request) {
		var edit struct {
			EndsAt int64 `json:"ends_at"`
		}
		json.NewDecoder(r.Body).Decode(&edit)
		f.closeAt = time.UnixMilli(edit.EndsAt)
		ok(w, 201, map[string]any{"id": 7, "slug": "replay"})
	})
	mux.HandleFunc("PUT /api/admin/contests/7/problems", func(w http.ResponseWriter, r *http.Request) { ok(w, 200, map[string]any{}) })
	mux.HandleFunc("PUT /api/admin/contests/7", func(w http.ResponseWriter, r *http.Request) { ok(w, 200, map[string]any{}) })
	mux.HandleFunc("POST /api/contests/replay/start", func(w http.ResponseWriter, r *http.Request) { ok(w, 200, map[string]any{}) })
	send := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Language, Source string }
			json.NewDecoder(r.Body).Decode(&body)
			if kind == "submit" && f.refused.CompareAndSwap(false, true) {
				w.Header().Set("Retry-After", "1")
				ok(w, 429, map[string]any{"code": "rate_limited"})
				return
			}
			f.mu.Lock()
			f.next++
			id := f.next
			f.verdicts[id] = strings.TrimSpace(strings.SplitN(body.Source, "\n", 2)[0])
			f.mu.Unlock()
			ok(w, 202, map[string]any{"id": id})
		}
	}
	mux.HandleFunc("POST /api/contests/replay/problems/{label}/run", send("run"))
	mux.HandleFunc("POST /api/contests/replay/problems/{label}/submit", send("submit"))
	read := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			f.mu.Lock()
			f.reads[path]++
			n := f.reads[path]
			var id int64
			fmt.Sscanf(path[strings.LastIndex(path, "/")+1:], "%d", &id)
			v := f.verdicts[id]
			f.mu.Unlock()
			if n < 2 {
				ok(w, 200, map[string]any{"state": "running", "results": []any{}})
				return
			}
			if kind == "run" {
				if v == "// ce" {
					ok(w, 200, map[string]any{"state": "done", "compile_output": "bad", "results": []map[string]any{{"verdict": ""}}})
					return
				}
				ok(w, 200, map[string]any{"state": "done", "results": []map[string]any{{"verdict": "AC", "time_ms": 40}}})
				return
			}
			ok(w, 200, map[string]any{"state": "done", "verdict": "AC", "max_time_ms": 90})
		}
	}
	mux.HandleFunc("GET /api/runs/{id}", read("run"))
	mux.HandleFunc("GET /api/submissions/{id}", read("submit"))
	return mux
}

func TestDriverReplay(t *testing.T) {
	f := &fakeServer{reads: map[string]int{}, verdicts: map[int64]string{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := &Corpus{Programs: []Program{
		{ID: "ok", Problem: "a-plus-b", Language: "cpp17", Source: "// ok\n", Heavy: true, Runs: 1, Submits: 2, Expected: map[string]string{"run": "AC", "submit": "AC"}},
		{ID: "ce", Problem: "average-speed", Language: "java21", Source: "// ce\n", Runs: 1, Expected: map[string]string{"run": "CE"}},
	}}
	items := c.Sequence(1)
	// ok/run, ok/submit, ce/run, ok/submit
	if len(items) != 4 {
		t.Fatalf("%d items", len(items))
	}
	d := &Driver{Base: srv.URL, Origin: "http://localhost:5173", Students: 2, Poll: 5 * time.Millisecond, Timeout: 20 * time.Second, Tail: 0}
	offsets := []time.Duration{0, 10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
	recs, err := d.Replay(context.Background(), c, items, offsets, 2, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("%d records", len(recs))
	}
	for i, r := range recs {
		if r.Verdict != r.Expected || r.ID == 0 || r.DoneMS < r.SubmittedMS || r.Detail != "" {
			t.Errorf("record %d: %+v", i, r)
		}
		if r.Rush != (i >= 2) {
			t.Errorf("record %d rush = %v", i, r.Rush)
		}
		if r.Graded != (r.Kind == "submit") {
			t.Errorf("record %d graded = %v for a %s inside the contest", i, r.Graded, r.Kind)
		}
	}
	if recs[0].TimeMS != 40 || recs[1].TimeMS != 90 || recs[2].Verdict != "CE" || !recs[0].Heavy {
		t.Errorf("records %+v", recs)
	}
	// One submission was refused once with 429 and went through on retry.
	if !f.refused.Load() {
		t.Error("the 429 path was not used")
	}
}

// Requests after the close are practice, not graded.
func TestDriverMarksPracticeAfterTheClose(t *testing.T) {
	f := &fakeServer{reads: map[string]int{}, verdicts: map[int64]string{}}
	f.refused.Store(true)
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := &Corpus{Programs: []Program{{ID: "ok", Problem: "a-plus-b", Language: "cpp17", Source: "// ok\n", Submits: 2, Expected: map[string]string{"submit": "AC"}}}}
	d := &Driver{Base: srv.URL, Origin: "o", Students: 1, Poll: 5 * time.Millisecond, Timeout: 20 * time.Second, Tail: 300 * time.Millisecond}
	// The replay is 400 ms long, so the contest closes at 100 ms.
	recs, err := d.Replay(context.Background(), c, c.Sequence(1), []time.Duration{0, 300 * time.Millisecond}, 0, 400*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !recs[0].Graded || recs[1].Graded {
		t.Errorf("graded = %v, %v; want true, false", recs[0].Graded, recs[1].Graded)
	}
}

func TestDriverReportsWhatItCannotDo(t *testing.T) {
	// A server that is not in testing mode.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 403) }))
	defer srv.Close()
	d := &Driver{Base: srv.URL, Students: 1, Poll: time.Millisecond, Timeout: time.Second}
	if _, err := d.Replay(context.Background(), &Corpus{}, nil, nil, 0, time.Minute); err == nil || !strings.Contains(err.Error(), "testing-mode") {
		t.Errorf("err = %v", err)
	}
	// A problem that was never seeded.
	f := &fakeServer{reads: map[string]int{}, verdicts: map[int64]string{}}
	srv2 := httptest.NewServer(f.handler())
	defer srv2.Close()
	d.Base = srv2.URL
	c := &Corpus{Programs: []Program{{ID: "x", Problem: "unseeded", Language: "cpp17", Runs: 1, Expected: map[string]string{"run": "AC"}}}}
	if _, err := d.Replay(context.Background(), c, nil, nil, 0, time.Minute); err == nil || !strings.Contains(err.Error(), "seed.py") {
		t.Errorf("err = %v", err)
	}
}

func TestSummarizeAndWrite(t *testing.T) {
	recs := []Record{
		{Program: "a", Kind: "run", Expected: "AC", Verdict: "AC", TimeMS: 10},
		{Program: "a", Kind: "run", Expected: "AC", Verdict: "AC", TimeMS: 30},
		{Program: "b", Kind: "submit", Expected: "TLE", Verdict: "AC"},
		{Program: "c", Kind: "run", Expected: "AC", Detail: "no verdict before the timeout"},
	}
	var out bytes.Buffer
	if bad := summarize(&out, recs); bad != 2 {
		t.Errorf("bad = %d", bad)
	}
	for _, want := range []string{"ok   a", "slowest test 30 ms", "BAD  b", "BAD  c", "none (no verdict", "2 of 4"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, out.String())
		}
	}
	dir := filepath.Join(t.TempDir(), "run1")
	if err := writeRecords(dir, recs); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "verdicts.jsonl"))
	if err != nil || strings.Count(string(b), "\n") != 4 {
		t.Fatalf("file %q %v", b, err)
	}
	// The field names are the ones cmd/loadcompare reads.
	for _, k := range []string{`"seq"`, `"program"`, `"kind"`, `"graded"`, `"heavy"`, `"rush"`, `"expected"`, `"verdict"`, `"id"`, `"submitted_ms"`, `"done_ms"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("record lacks %s", k)
		}
	}
	if err := writeRecords(filepath.Join(dir, "verdicts.jsonl", "x"), recs); err == nil {
		t.Error("an impossible directory was accepted")
	}
}

func TestOnly(t *testing.T) {
	c := loadRepoCorpus(t)
	o, err := c.Only([]string{"apb-ce", "lg-std-ac"})
	if err != nil || len(o.Programs) != 2 || o.Programs[0].ID != "apb-ce" {
		t.Fatalf("%v %+v", err, o)
	}
	if _, err := c.Only([]string{"nope"}); err == nil {
		t.Error("an unknown id was accepted")
	}
}

func TestUniqueSource(t *testing.T) {
	cpp := &Program{Language: "cpp17", Source: "int main(){}\n"}
	py := &Program{Language: "python3", Source: "print(1)\n"}
	if uniqueSource(cpp, 1) == uniqueSource(cpp, 2) || uniqueSource(cpp, 1) != uniqueSource(cpp, 1) {
		t.Error("sources must differ per request and repeat for one request")
	}
	if !strings.HasPrefix(uniqueSource(cpp, 1), cpp.Source) || !strings.HasSuffix(uniqueSource(cpp, 7), "// replay 7\n") {
		t.Errorf("cpp: %q", uniqueSource(cpp, 7))
	}
	if !strings.HasSuffix(uniqueSource(py, 7), "# replay 7\n") {
		t.Errorf("python: %q", uniqueSource(py, 7))
	}
}

// The driver sends the unique source: two requests of one program reach
// the server with different text.
func TestDriverSendsUniqueSources(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	f := &fakeServer{reads: map[string]int{}, verdicts: map[int64]string{}}
	f.refused.Store(true)
	h := f.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/run") {
			b, _ := io.ReadAll(r.Body)
			var body struct{ Source string }
			json.Unmarshal(b, &body)
			mu.Lock()
			seen[body.Source] = true
			mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := &Corpus{Programs: []Program{{ID: "ok", Problem: "a-plus-b", Language: "cpp17", Source: "// ok\n", Runs: 3, Expected: map[string]string{"run": "AC"}}}}
	d := &Driver{Base: srv.URL, Origin: "o", Students: 1, Poll: 5 * time.Millisecond, Timeout: 20 * time.Second}
	if _, err := d.Replay(context.Background(), c, c.Sequence(1), []time.Duration{0, 0, 0}, 3, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Errorf("%d distinct sources for 3 requests", len(seen))
	}
}
