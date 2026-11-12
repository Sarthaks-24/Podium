package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth"
)

const (
	hiddenMarker = "HIDDEN-TEST-MARKER-91c2"
	sampleMarker = "SAMPLE-MARKER-4d7e"
	pastedMarker = "PASTED-SOLUTION-MARKER-58b1"
)

func proofState(t *testing.T, s *Service, id int64) *ProofState {
	t.Helper()
	st, err := s.ProofState(context.Background(), id)
	if err != nil {
		t.Fatalf("ProofState: %v", err)
	}
	return st
}

func langOf(t *testing.T, st *ProofState, lang string) ProofLanguage {
	t.Helper()
	for _, l := range st.Languages {
		if l.Language == lang {
			return l
		}
	}
	t.Fatalf("no %s in %+v", lang, st.Languages)
	return ProofLanguage{}
}

// A problem nobody checked lists its four languages as not checked; after a
// round the state holds each language's outcome, why, the counts and the
// compiler lines, and never a checker message.
func TestProofStateReadsTheRound(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)

	st := proofState(t, s, p.ID)
	if st.Checking || st.Progress != nil || st.Applied != nil || len(st.Languages) != 4 {
		t.Fatalf("fresh state %+v", st)
	}
	for _, l := range st.Languages {
		if !l.Offered || l.Result != "none" || l.Source != "" {
			t.Errorf("fresh %s: %+v", l.Language, l)
		}
	}

	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, map[string]string{"java21/solution": "wa", "python3/starter": "ce", "node22/solution": "ie"})
	st = proofState(t, s, p.ID)
	if st.Checking || st.Progress != nil {
		t.Errorf("after the round: %+v", st)
	}
	if l := langOf(t, st, "cpp17"); l.Result != "passed" || !l.Offered || l.Source != "wizard" || !l.Current {
		t.Errorf("cpp17 %+v", l)
	}
	java := langOf(t, st, "java21")
	if java.Result != "failed" || java.Reason != "wrong_answer" || java.Failed != 6 || java.Total != 6 || java.Offered || len(java.Tests) != 6 {
		t.Errorf("java21 %+v", java)
	}
	py := langOf(t, st, "python3")
	if py.Result != "failed" || py.Reason != "compile_error" || !strings.Contains(py.Compiler, "class Solution is public") || py.Offered {
		t.Errorf("python3 %+v", py)
	}
	if l := langOf(t, st, "node22"); l.Result != "unchecked" || l.Reason != "internal_error" || l.Offered {
		t.Errorf("node22 %+v", l)
	}
	if a := st.Applied; a == nil || strings.Join(a.Removed, ",") != "java21,node22,python3" {
		t.Errorf("applied %+v", a)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), secretMessage) {
		t.Error("a checker message reached the proof state")
	}

	// A later edit of the content makes the result not current.
	e := editOf(p)
	e.TimeLimitMS++
	if _, err := s.Update(context.Background(), p.ID, e); err != nil {
		t.Fatal(err)
	}
	if l := langOf(t, proofState(t, s, p.ID), "cpp17"); l.Current || l.Result != "passed" {
		t.Errorf("after an edit: %+v", l)
	}
}

// While a round runs, checking is true and the progress counts its jobs and
// names the language being run.
func TestProofStateWhileChecking(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	n := queueRound(t, s, p, wizardSpec(t, s, p))

	st := proofState(t, s, p.ID)
	if !st.Checking || st.Progress == nil || st.Progress.Done != 0 || st.Progress.Total != n || st.Progress.Current != "" {
		t.Fatalf("queued: %+v %+v", st, st.Progress)
	}
	c, pl := claimProof(t, s)
	if st = proofState(t, s, p.ID); st.Progress.Current != pl.Proof.Language || st.Progress.Done != 0 {
		t.Errorf("leased %s: %+v", pl.Proof.Language, st.Progress)
	}
	if err := s.Jobs.Report(context.Background(), proofReport(c, pl, "")); err != nil {
		t.Fatal(err)
	}
	if st = proofState(t, s, p.ID); st.Progress.Done != 1 || st.Progress.Total != n || !st.Checking {
		t.Errorf("one done: %+v", st.Progress)
	}
	runProofs(t, s, nil)
	if st = proofState(t, s, p.ID); st.Checking || st.Progress != nil {
		t.Errorf("finished: %+v", st)
	}
}

// A draft in the generate chain is being checked before any proof job exists.
func TestProofStateCheckingFromTheChain(t *testing.T) {
	s := newGenBank(t)
	p, err := s.CreateDraft(context.Background(), funcDraft(t, s), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestInputGenerationProof(context.Background(), p.ID, 1, solutionTexts("java21", "python3", "node22")); err != nil {
		t.Fatal(err)
	}
	st := proofState(t, s, p.ID)
	if !st.Checking || st.Progress == nil || st.Progress.Total != 0 || st.Progress.Done != 0 {
		t.Errorf("chain: %+v %+v", st, st.Progress)
	}
}

func TestProofStateOfOtherProblems(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	whole := importExample(t, s, "a-plus-b")
	if st := proofState(t, s, whole.ID); st.Checking || len(st.Languages) != 0 || st.Languages == nil {
		t.Errorf("whole-program problem: %+v", st)
	}
	if _, err := s.ProofState(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing problem: %v", err)
	}
	p := proofProblem(t, s)
	if _, err := s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProofState(ctx, p.ID); !errors.Is(err, ErrDeleted) {
		t.Errorf("deleted problem: %v", err)
	}
}

func TestProofStateQueriesUseTheStateIndex(t *testing.T) {
	s := newProofBank(t, false)
	for name, q := range map[string]string{"state": proofStateSQL, "open jobs": openProofJobsSQL} {
		args := []any{ProofPriority, 1}
		rows, err := s.DB.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			rows.Scan(&id, &parent, &unused, &detail)
			plan = append(plan, detail)
		}
		rows.Close()
		if joined := strings.Join(plan, " | "); !strings.Contains(joined, "SEARCH jobs USING INDEX jobs_by_state (state=? AND priority=?)") || strings.Contains(joined, "SCAN jobs") {
			t.Errorf("%s does not search jobs_by_state by state and priority: %s", name, strings.Join(plan, " | "))
		}
	}
}

// tableHits counts the rows of every table with any column holding text.
func tableHits(t *testing.T, db *sql.DB, text string) map[string]int {
	t.Helper()
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		tables.Scan(&n)
		names = append(names, n)
	}
	tables.Close()
	hits := map[string]int{}
	for _, table := range names {
		cols, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		var where []string
		for cols.Next() {
			var c string
			cols.Scan(&c)
			where = append(where, `CAST("`+c+`" AS TEXT) LIKE '%`+text+`%'`)
		}
		cols.Close()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM "` + table + `" WHERE ` + strings.Join(where, " OR ")).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			hits[table] = n
		}
	}
	return hits
}

// A check queues one solution job for an offered language, never changes the
// problem whether it passes or fails, and keeps the pasted solution out of
// every table: it exists only in the blob store.
func TestCheckQueuesOneJobAndChangesNothing(t *testing.T) {
	for _, how := range []string{"", "wa"} {
		s := newProofBank(t, true)
		ctx := context.Background()
		p := proofProblem(t, s)
		revs := revCount(t, s, p.ID)
		if err := s.RequestLanguageProof(ctx, p.ID, 1, ProofRequest{Language: "java21", Solution: "// " + pastedMarker + "\nclass Solution {}"}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ?", ProofPriority); n != 1 {
			t.Fatalf("%d proof jobs, want one", n)
		}
		if st := proofState(t, s, p.ID); !st.Checking || st.Progress.Total != 1 {
			t.Errorf("state %+v", st)
		}
		var payload string
		s.DB.QueryRow("SELECT payload FROM jobs WHERE priority = ?", ProofPriority).Scan(&payload)
		pl, _ := decodeProof(json.RawMessage(payload))
		if pl.Proof.Source != ProofSourceCheck || pl.Proof.Language != "java21" || pl.Proof.Part != ProofPartSolution || pl.Proof.By != 1 || len(pl.Tests) != 6 {
			t.Errorf("payload %+v", pl.Proof)
		}
		runProofs(t, s, map[string]string{"java21/solution": how})
		if hits := tableHits(t, s.DB, pastedMarker); len(hits) > 0 {
			t.Errorf("the pasted solution is in tables %v", hits)
		}
		got, _ := s.Get(ctx, p.ID)
		if got.ContentHash != p.ContentHash || !got.Offers("java21") || revCount(t, s, p.ID) != revs || validateJobs(t, s) != 0 {
			t.Errorf("how %q: the problem changed", how)
		}
		want := "passed"
		if how != "" {
			want = "failed"
		}
		if l := langOf(t, proofState(t, s, p.ID), "java21"); l.Result != want || l.Source != "check" || !l.Offered {
			t.Errorf("how %q: %+v", how, l)
		}
		// The composed file is a blob (the job names it), the only place the text lives.
		if b, err := s.Blobs.Read(pl.Programs.Reference.SourceSHA, 1<<20); err != nil || !strings.Contains(string(b), pastedMarker) {
			t.Error("the composed solution is not the blob the job names")
		}
	}
}

func TestRequestLanguageProofRefusals(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	check := func(lang string) error {
		return s.RequestLanguageProof(ctx, p.ID, 1, ProofRequest{Language: lang, Solution: "x"})
	}

	if err := s.RequestLanguageProof(ctx, 9999, 1, ProofRequest{Language: "java21", Solution: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	whole := importExample(t, s, "a-plus-b")
	if err := s.RequestLanguageProof(ctx, whole.ID, 1, ProofRequest{Language: "java21", Solution: "x"}); !errors.Is(err, ErrNotFunction) || err.Error() != "Only a function problem has languages to check." {
		t.Errorf("whole program: %v", err)
	}

	// A check on a language that is not offered never adds it.
	s2 := newProofBank(t, true)
	javaGone, _ := javaRemoved(t, s2)
	var le *LanguageStateError
	err := s2.RequestLanguageProof(ctx, javaGone.ID, 1, ProofRequest{Language: "java21", Solution: "x"})
	if !errors.As(err, &le) || le.Offered || err.Error() != "Java 21 is not offered. Write its locked code and starting code and save, or use Fix Java 21 with AI." {
		t.Errorf("not offered: %v", err)
	}
	// A fix on an offered language.
	err = s.RequestLanguageProof(ctx, p.ID, 1, ProofRequest{Language: "java21", Solution: "x", Parts: &LanguageParts{"a", "b", "c"}})
	if !errors.As(err, &le) || !le.Offered || err.Error() != "Java 21 is already offered. Use Check Java 21 to test it." {
		t.Errorf("already offered: %v", err)
	}
	if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ?", ProofPriority); n != 0 {
		t.Fatalf("%d jobs after refusals", n)
	}

	// Not ready: a test without its output.
	if _, err := s.DB.Exec("UPDATE testcases SET output_sha = NULL WHERE problem_id = ? AND idx = 6", p.ID); err != nil {
		t.Fatal(err)
	}
	var nr *NotReadyError
	if err := check("java21"); !errors.As(err, &nr) {
		t.Errorf("not ready: %v", err)
	}
	if _, err := s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := check("java21"); !errors.Is(err, ErrDeleted) {
		t.Errorf("deleted: %v", err)
	}
}

// A press while a round is open is refused; the refusal sits in the same
// transaction as the queueing, so two presses at once queue one round.
func TestTwoChecksAtOnceQueueOneRound(t *testing.T) {
	for run := 0; run < 20; run++ {
		s := newProofBank(t, true)
		p := proofProblem(t, s)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = s.RequestLanguageProof(context.Background(), p.ID, 1, ProofRequest{Language: "java21", Solution: "x"})
			}()
		}
		wg.Wait()
		ok, checking := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrChecking):
				checking++
			default:
				t.Fatalf("run %d: %v", run, err)
			}
		}
		if ok != 1 || checking != 1 {
			t.Fatalf("run %d: %d queued, %d refused with checking", run, ok, checking)
		}
		if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ?", ProofPriority); n != 1 {
			t.Fatalf("run %d: %d proof jobs", run, n)
		}
	}
}

// A fix queues a solution job and a starter job from the AI's parts, refuses
// a problem that changed since the snapshot, and adds the language only on a
// pass.
func TestFixRequestAddsALanguageOnlyOnAPass(t *testing.T) {
	type tc struct {
		behave map[string]string
		added  bool
	}
	for name, c := range map[string]tc{
		"pass":                     {nil, true},
		"solution fails":           {map[string]string{"java21/solution": "wa"}, false},
		"starter does not compile": {map[string]string{"java21/starter": "ce"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProofBank(t, true)
			ctx := context.Background()
			p, _ := javaRemoved(t, s)
			parts := &LanguageParts{Top: "// top\n", Starter: "// starter\n", Bottom: "// bottom\n"}
			revs := revCount(t, s, p.ID)
			err := s.RequestLanguageProof(ctx, p.ID, 1, ProofRequest{Language: "java21", Solution: "// sol\n", Parts: parts, WantHash: p.ContentHash})
			if err != nil {
				t.Fatal(err)
			}
			if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ?", ProofPriority); n != 2 {
				t.Fatalf("%d jobs, want a solution and a starter job", n)
			}
			runProofs(t, s, c.behave)
			got, _ := s.Get(ctx, p.ID)
			if got.Offers("java21") != c.added {
				t.Fatalf("offered %v, want %v", got.Offers("java21"), c.added)
			}
			if c.added && (got.LockedTop["java21"] != parts.Top || got.Starters["java21"] != parts.Starter || got.LockedBottom["java21"] != parts.Bottom) {
				t.Errorf("parts not added: %q", got.LockedTop["java21"])
			}
			if !c.added && (revCount(t, s, p.ID) != revs || validateJobs(t, s) != 0) {
				t.Error("a failed fix changed the problem")
			}
			if c.added && applied(t, s).Added != "java21" {
				t.Errorf("applied %+v", applied(t, s))
			}
		})
	}

	// The problem changed after the snapshot: refused, nothing queued.
	s := newProofBank(t, true)
	p, _ := javaRemoved(t, s)
	err := s.RequestLanguageProof(context.Background(), p.ID, 1, ProofRequest{Language: "java21", Solution: "x",
		Parts: &LanguageParts{"a", "b", "c"}, WantHash: "stale-hash"})
	if !errors.Is(err, ErrChanged) || err.Error() != "The problem changed while the AI was working. Press Fix again." {
		t.Errorf("stale hash: %v", err)
	}
	if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ?", ProofPriority); n != 0 {
		t.Errorf("%d jobs queued for a stale fix", n)
	}
	// The fix's text is in no table either.
	if err := s.RequestLanguageProof(context.Background(), p.ID, 1, ProofRequest{Language: "java21", Solution: "// " + pastedMarker,
		Parts: &LanguageParts{"// top " + pastedMarker, "b", "c"}, WantHash: p.ContentHash}); err != nil {
		t.Fatal(err)
	}
	if hits := tableHits(t, s.DB, pastedMarker); len(hits) > 0 {
		t.Errorf("the AI's text is in tables %v", hits)
	}
	// A second press while the round is open.
	if err := s.RequestLanguageProof(context.Background(), p.ID, 1, ProofRequest{Language: "java21", Solution: "x",
		Parts: &LanguageParts{"a", "b", "c"}, WantHash: p.ContentHash}); !errors.Is(err, ErrChecking) {
		t.Errorf("second press: %v", err)
	}
}

// plantMarkers writes a marker into every test file's blob: the sample tests
// get one marker and every hidden test another.
func plantMarkers(t *testing.T, s *Service, p *Problem) {
	t.Helper()
	for _, test := range p.Tests {
		marker := hiddenMarker
		if p.IsSample(test.Idx) {
			marker = sampleMarker
		}
		for _, sha := range []string{test.InputSHA, test.OutputSHA} {
			if err := os.WriteFile(s.Blobs.path(sha), []byte(marker+" 1 2 3\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// The fix snapshot carries the statement, the reference, the samples and the
// language's last failure, and opens no hidden test.
func TestFixSnapshotHoldsOnlySamples(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p, _ := javaRemoved(t, s)
	plantMarkers(t, s, p)

	snap, err := s.FixSnapshot(ctx, p.ID, "java21")
	if err != nil {
		t.Fatal(err)
	}
	if snap.ContentHash != p.ContentHash || snap.Statement != p.Statement || snap.Reference != p.Reference || snap.Failure != nil {
		t.Errorf("snapshot %+v", snap)
	}
	if len(snap.Samples) != len(p.Samples) || len(snap.Samples) == 0 {
		t.Fatalf("%d samples, want %d", len(snap.Samples), len(p.Samples))
	}
	b, _ := json.Marshal(snap)
	if !strings.Contains(string(b), sampleMarker) || strings.Contains(string(b), hiddenMarker) {
		t.Errorf("samples missing or a hidden test is in the snapshot:\n%s", b)
	}

	// A sample file larger than 4 KB is cut.
	big := strings.Repeat("9", 10<<10)
	if err := os.WriteFile(s.Blobs.path(p.Tests[0].InputSHA), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if snap, err = s.FixSnapshot(ctx, p.ID, "java21"); err != nil || len(snap.Samples[0].Input) != 4<<10 {
		t.Errorf("large sample: %v %d", err, len(snap.Samples[0].Input))
	}

}

func TestFixSnapshotCarriesTheLastFailure(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	snap, err := s.FixSnapshot(ctx, p.ID, "java21")
	if err != nil {
		t.Fatal(err)
	}
	if f := snap.Failure; f == nil || f.Outcome != ProofFailed || f.Reason != "wrong_answer" || f.Failed != 6 || f.Verdicts["WA"] != 6 {
		t.Errorf("failure %+v", snap.Failure)
	}
}

func TestFixSnapshotRefusals(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	if _, err := s.FixSnapshot(ctx, 9999, "java21"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	whole := importExample(t, s, "a-plus-b")
	if _, err := s.FixSnapshot(ctx, whole.ID, "java21"); !errors.Is(err, ErrNotFunction) {
		t.Errorf("whole program: %v", err)
	}
	p := proofProblem(t, s)
	var le *LanguageStateError
	if _, err := s.FixSnapshot(ctx, p.ID, "java21"); !errors.As(err, &le) || !le.Offered {
		t.Errorf("offered: %v", err)
	}
	s2 := newProofBank(t, true)
	p2, _ := javaRemoved(t, s2)
	queueRound(t, s2, p2, ProofSpec{Source: ProofSourceCheck, Language: "cpp17", Solutions: map[string]string{"cpp17": mustBlob(t, s2, "x")}})
	if _, err := s2.FixSnapshot(ctx, p2.ID, "java21"); !errors.Is(err, ErrChecking) {
		t.Errorf("checking: %v", err)
	}
	s3 := newProofBank(t, true)
	p3, _ := javaRemoved(t, s3)
	s3.DB.Exec("UPDATE testcases SET output_sha = NULL WHERE problem_id = ? AND idx = 6", p3.ID)
	var nr *NotReadyError
	if _, err := s3.FixSnapshot(ctx, p3.ID, "java21"); !errors.As(err, &nr) {
		t.Errorf("not ready: %v", err)
	}
	s3.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p3.ID)
	if _, err := s3.FixSnapshot(ctx, p3.ID, "java21"); !errors.Is(err, ErrDeleted) {
		t.Errorf("deleted: %v", err)
	}
}

func mustBlob(t *testing.T, s *Service, text string) string {
	t.Helper()
	sha, err := s.Blobs.Write([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// --- handlers ---

func routeRequest(method, path string, body string, id int64, lang string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", itoa(id))
	r.SetPathValue("language", lang)
	return r.WithContext(auth.WithUser(r.Context(), auth.User{ID: 1, Role: auth.Organiser}))
}

func proofHandlers(s *Service) *Handlers {
	return &Handlers{Problems: s, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestGetProofHandler(t *testing.T) {
	s := newProofBank(t, true)
	h := proofHandlers(s)
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, map[string]string{"java21/solution": "wa"})

	w := httptest.NewRecorder()
	h.GetProof(w, routeRequest("GET", "/api/admin/problems/1/proof", "", p.ID, ""))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out struct {
		Checking  bool `json:"checking"`
		Languages []struct {
			Language string `json:"language"`
			Offered  bool   `json:"offered"`
			Result   string `json:"result"`
			Reason   string `json:"reason"`
			Failed   int    `json:"failed"`
			Total    int    `json:"total"`
			Current  bool   `json:"current"`
			Source   string `json:"source"`
		} `json:"languages"`
		Applied   map[string]any `json:"applied"`
		ServerNow int64          `json:"server_now"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ServerNow == 0 || len(out.Languages) != 4 || out.Applied["validating"] == nil {
		t.Fatalf("body %s: %v", w.Body, err)
	}
	if java := out.Languages[1]; java.Language != "java21" || java.Offered || java.Result != "failed" || java.Reason != "wrong_answer" || java.Failed != 6 || java.Total != 6 || !java.Current || java.Source != "wizard" {
		t.Errorf("java21 %+v", java)
	}
	if strings.Contains(w.Body.String(), secretMessage) {
		t.Error("a checker message is in the response")
	}

	for name, c := range map[string]struct {
		id   int64
		code int
		want string
	}{"missing": {9999, 404, "not_found"}} {
		w := httptest.NewRecorder()
		h.GetProof(w, routeRequest("GET", "/x", "", c.id, ""))
		if got, _ := errorBody(t, w); w.Code != c.code || got != c.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("id", "abc")
	h.GetProof(w, r)
	if code, _ := errorBody(t, w); w.Code != 404 || code != "not_found" {
		t.Errorf("bad id: %d %s", w.Code, w.Body)
	}
	s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p.ID)
	w = httptest.NewRecorder()
	h.GetProof(w, routeRequest("GET", "/x", "", p.ID, ""))
	if code, _ := errorBody(t, w); w.Code != 409 || code != "deleted" {
		t.Errorf("deleted: %d %s", w.Code, w.Body)
	}
}

func TestCheckLanguageHandler(t *testing.T) {
	post := func(h *Handlers, id int64, lang, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.CheckLanguage(w, routeRequest("POST", "/x", body, id, lang))
		return w
	}
	wantErr := func(t *testing.T, name string, w *httptest.ResponseRecorder, status int, code, msg string) {
		t.Helper()
		c, m := errorBody(t, w)
		if w.Code != status || c != code || m != msg {
			t.Errorf("%s: %d %s %q", name, w.Code, c, m)
		}
	}
	s := newProofBank(t, true)
	h := proofHandlers(s)
	p := proofProblem(t, s)

	const badSolution = "Paste a solution of at most 64 KB."
	wantErr(t, "language", post(h, p.ID, "cobol", `{"solution":"x"}`), 400, "bad_request", "Choose C++17, Java 21, Python 3 or JavaScript.")
	wantErr(t, "empty", post(h, p.ID, "java21", `{"solution":""}`), 400, "bad_request", badSolution)
	wantErr(t, "blank", post(h, p.ID, "java21", `{"solution":"  \n"}`), 400, "bad_request", badSolution)
	wantErr(t, "long", post(h, p.ID, "java21", `{"solution":"`+strings.Repeat("a", MaxSource+1)+`"}`), 400, "bad_request", badSolution)
	if w := post(h, p.ID, "java21", `{"solution":"x","extra":1}`); w.Code != 400 {
		t.Errorf("unknown field: %d", w.Code)
	}
	if w := post(h, p.ID, "java21", `{}`); w.Code != 400 {
		t.Errorf("no solution: %d", w.Code)
	}
	wantErr(t, "missing", post(h, 9999, "java21", `{"solution":"x"}`), 404, "not_found", "There is no such problem.")
	whole := importExample(t, s, "a-plus-b")
	wantErr(t, "whole", post(h, whole.ID, "java21", `{"solution":"x"}`), 409, "not_function", "Only a function problem has languages to check.")

	w := post(h, p.ID, "java21", `{"solution":"class Solution {}"}`)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 202 || out["state"] != "checking" || out["language"] != "java21" || out["server_now"] == nil {
		t.Fatalf("queued: %d %s", w.Code, w.Body)
	}
	wantErr(t, "checking", post(h, p.ID, "java21", `{"solution":"x"}`), 409, "checking", "Podium is still checking the languages of this problem.")
	if strings.Contains(w.Body.String(), "class Solution") {
		t.Error("the solution is in the response")
	}

	s2 := newProofBank(t, true)
	h2 := proofHandlers(s2)
	p2, _ := javaRemoved(t, s2)
	wantErr(t, "not offered", post(h2, p2.ID, "java21", `{"solution":"x"}`), 409, "not_offered",
		"Java 21 is not offered. Write its locked code and starting code and save, or use Fix Java 21 with AI.")
	s2.DB.Exec("UPDATE testcases SET output_sha = NULL WHERE problem_id = ? AND idx = 6", p2.ID)
	if c, _ := errorBody(t, post(h2, p2.ID, "cpp17", `{"solution":"x"}`)); c != "not_ready" {
		t.Errorf("not ready: %s", c)
	}
}

// No hidden test byte reaches the proof routes' responses.
func TestProofRoutesLeakNoHiddenTests(t *testing.T) {
	s := newProofBank(t, true)
	h := proofHandlers(s)
	p := proofProblem(t, s)
	plantMarkers(t, s, p)
	var bodies []string
	w := httptest.NewRecorder()
	h.CheckLanguage(w, routeRequest("POST", "/x", `{"solution":"x"}`, p.ID, "java21"))
	bodies = append(bodies, w.Body.String())
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	w = httptest.NewRecorder()
	h.GetProof(w, routeRequest("GET", "/x", "", p.ID, ""))
	bodies = append(bodies, w.Body.String())
	for _, b := range bodies {
		if strings.Contains(b, hiddenMarker) || strings.Contains(b, sampleMarker) {
			t.Errorf("test data in a response: %s", b)
		}
	}
}
