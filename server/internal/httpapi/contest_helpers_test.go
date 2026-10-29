package httpapi

// Helpers for the contest tests: problems imported under a chosen slug and
// marked validated, contests made over HTTP, and a fake judge that runs
// tiny "programs" against the real test data, so a test edit changes
// verdicts as it would with the real judge.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// packZip zips problems/a-plus-b with its slug and title changed and any
// file replaced by override (path relative to the pack).
func packZip(t *testing.T, slug string, override map[string]string, testsOnly bool) []byte {
	t.Helper()
	dir := "../../../problems/a-plus-b"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if testsOnly && !strings.HasPrefix(rel, "tests/") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if rel == "problem.yaml" {
			s := strings.Replace(string(b), "slug: a-plus-b", "slug: "+slug, 1)
			b = []byte(strings.Replace(s, "title: A plus B", "title: A plus B ("+slug+")", 1))
		}
		if o, ok := override[rel]; ok {
			b = []byte(o)
		}
		w, _ := zw.Create(rel)
		w.Write(b)
		return nil
	})
	zw.Close()
	return buf.Bytes()
}

// validate marks a problem validated at its current content, as a passing
// validation run would.
func (s *site) validate(id int64) {
	s.t.Helper()
	if _, err := s.db.Exec("UPDATE problems SET validated_hash = content_hash, validated_at = 1 WHERE id = ?", id); err != nil {
		s.t.Fatal(err)
	}
}

// importProblem imports a copy of a-plus-b under slug and validates it.
func (s *site) importProblem(owner *browser, slug string) int64 {
	s.t.Helper()
	code, out := owner.raw("POST", "/api/admin/problems/import", "application/zip", packZip(s.t, slug, nil, false), s.srv.URL)
	if code != 201 {
		s.t.Fatalf("import %s: %d %v", slug, code, out)
	}
	id := int64(out["id"].(float64))
	s.validate(id)
	return id
}

// signedIn returns a browser signed in as email with a display name.
func (s *site) signedIn(email, name string) *browser {
	b := s.browser()
	b.signIn(authtest.Person{Email: email, Verified: true, Name: name}, "/")
	return b
}

// createContest makes and publishes a contest over HTTP with the given
// settings on top of a fixed start at start lasting 120 minutes.
func (s *site) createContest(owner *browser, slug string, start time.Time, problems map[string]int64, settings map[string]any) int64 {
	s.t.Helper()
	// The slug comes from the title (V3-15), so the title is the slug the tests address.
	body := map[string]any{"title": slug, "starts_at": start.UnixMilli(),
		"ends_at": start.Add(120 * time.Minute).UnixMilli(), "min_gap_s": 0}
	for k, v := range settings {
		body[k] = v
	}
	code, out := owner.do("POST", "/api/admin/contests", body, s.srv.URL)
	if code != 201 {
		s.t.Fatalf("create contest: %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	var choices []map[string]any
	for label, pid := range problems {
		choices = append(choices, map[string]any{"problem_id": pid, "label": label})
	}
	// Labels in order, so A comes before B.
	for i := range choices {
		for j := i + 1; j < len(choices); j++ {
			if choices[j]["label"].(string) < choices[i]["label"].(string) {
				choices[i], choices[j] = choices[j], choices[i]
			}
		}
	}
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", id), choices, s.srv.URL); code != 200 {
		s.t.Fatalf("set problems: %d %v", code, out)
	}
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", id), map[string]any{"published": true}, s.srv.URL); code != 200 {
		s.t.Fatalf("publish: %d %v", code, out)
	}
	return id
}

// at sets the clock to minutes after start.
func (s *site) at(start time.Time, minutes float64) {
	s.clock.set(start.Add(time.Duration(minutes * float64(time.Minute))))
}

// runProgram is the fake judge's interpreter for the a-plus-b input format.
// "correct" adds exactly, "int32" wraps like a 32-bit int, "first" prints
// only the first answer.
func runProgram(prog, input string) string {
	f := strings.Fields(input)
	if len(f) == 0 {
		return ""
	}
	n, _ := strconv.Atoi(f[0])
	var out []string
	mod := new(big.Int).Lsh(big.NewInt(1), 32)
	half := new(big.Int).Lsh(big.NewInt(1), 31)
	for i := 0; i < n && 2+2*i < len(f); i++ {
		a, _ := new(big.Int).SetString(f[1+2*i], 10)
		b, _ := new(big.Int).SetString(f[2+2*i], 10)
		sum := new(big.Int).Add(a, b)
		if prog == "int32" {
			sum.Mod(sum, mod)
			if sum.Cmp(half) >= 0 {
				sum.Sub(sum, mod)
			}
		}
		out = append(out, sum.String())
		if prog == "first" {
			break
		}
	}
	return strings.Join(out, "\n")
}

func sameTokens(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// judgeAll plays the judge until the queue is empty and returns how many
// jobs it judged.
func (s *site) judgeAll() int {
	s.t.Helper()
	n := 0
	for {
		resp, out := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{"submit", "run"}, Worker: "fake"})
		if resp.StatusCode == 204 {
			return n
		}
		if resp.StatusCode != 200 {
			s.t.Fatalf("claim %d %s", resp.StatusCode, out)
		}
		var c judgeapi.Claim
		json.Unmarshal(out, &c)
		rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
		read := func(sha string) string {
			b, err := s.bank.Blobs.Read(sha, 1<<22)
			if err != nil {
				s.t.Fatalf("read blob: %v", err)
			}
			return string(b)
		}
		switch c.Kind {
		case "submit":
			var pl judgeapi.SubmitPayload
			json.Unmarshal(c.Payload, &pl)
			prog := read(pl.Program.SourceSHA)
			if prog == "ce" {
				rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE", Output: "error"}}
				break
			}
			rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}
			stopped, revealed := false, false
			for _, test := range pl.Tests {
				v := "WA"
				if stopped {
					v = "SK"
				} else if sameTokens(runProgram(prog, read(test.InputSHA)), read(test.OutputSHA)) {
					v = "AC"
				}
				if test.Sample && v == "WA" && pl.StopOnSampleFail {
					stopped = true
				}
				res := judgeapi.TestResult{Idx: test.Idx, Role: judgeapi.RoleProgram, Verdict: v, TimeMS: 137, MemoryKB: 5123}
				// As the worker does (D115): output only on the first
				// failing hidden test of a payload that asks to reveal.
				if pl.Reveal && !test.Sample && v == "WA" && !revealed {
					out := runProgram(prog, read(test.InputSHA))
					res.Output, revealed = out[:min(len(out), judgeapi.MaxRunOutput)], true
				}
				rep.Results = append(rep.Results, res)
			}
		case "run":
			var pl judgeapi.RunPayload
			json.Unmarshal(c.Payload, &pl)
			rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}
			// As the worker does (D115): an open test runs only while every
			// earlier checked input passed.
			stopOpen := false
			for _, in := range pl.Inputs {
				if in.Open && stopOpen {
					rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "SK"})
					continue
				}
				out := runProgram(read(pl.Program.SourceSHA), read(in.InputSHA))
				v := "AC"
				if in.ExpectedSHA != "" && !sameTokens(out, read(in.ExpectedSHA)) {
					v, stopOpen = "WA", true
				}
				rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: v, TimeMS: 137, Output: out})
			}
		}
		if resp, out := s.judgeCall("POST", "/api/judge/report", judgeToken, rep); resp.StatusCode != 200 {
			s.t.Fatalf("report %d %s", resp.StatusCode, out)
		}
		n++
	}
}

// submit sends one submission to a contest problem and judges the queue.
func (s *site) submit(b *browser, slug, label, prog string) (int, map[string]any) {
	s.t.Helper()
	code, out := b.do("POST", "/api/contests/"+slug+"/problems/"+label+"/submit",
		map[string]any{"language": "cpp17", "source": prog, "client_key": fmt.Sprintf("%s-%s-%d", label, prog, time.Now().UnixNano())}, s.srv.URL)
	if code == 202 {
		s.judgeAll()
	}
	return code, out
}

type boardRow struct {
	name    string
	rank    int
	total   int
	pending map[string]int // label -> pending count, when checked
}

// checkBoard compares a standings answer with the expected rows, in order.
func checkBoard(t *testing.T, what string, got map[string]any, want []boardRow) {
	t.Helper()
	rows, _ := got["rows"].([]any)
	if len(rows) != len(want) {
		t.Fatalf("%s: %d rows, want %d: %v", what, len(rows), len(want), got)
	}
	labels := got["labels"].([]any)
	for i, w := range want {
		r := rows[i].(map[string]any)
		name, rank, total := r["name"].(string), int(r["rank"].(float64)), int(r["total"].(float64))
		if name != w.name || rank != w.rank || total != w.total {
			t.Errorf("%s row %d: got %s rank %d total %d, want %s rank %d total %d", what, i+1, name, rank, total, w.name, w.rank, w.total)
		}
		cells := r["cells"].([]any)
		for li, l := range labels {
			cell := cells[li].(map[string]any)
			if p := int(cell["pending"].(float64)); p != w.pending[l.(string)] {
				t.Errorf("%s %s cell %s pending %d, want %d", what, w.name, l, p, w.pending[l.(string)])
			}
		}
	}
}
