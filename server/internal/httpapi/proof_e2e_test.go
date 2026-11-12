package httpapi

// The whole function-problem chain over HTTP: the wizard route with the fake
// provider, then every queued job played through the judge API (generate
// inputs, generate outputs, the proof jobs, the validation), answering each
// proof job as the canned variant says. At the end the problem offers what
// passed, the proof route says why the rest was removed, and the problem is
// validated only after the real validation ran.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// proofJob is the part of a validate payload this test reads.
type proofJob struct {
	judgeapi.ValidatePayload
	Proof *struct{ Round, Language, Part string } `json:"proof"`
}

// playJudge claims jobs through the judge API until none is waiting and
// answers them. how maps "language/part" to a behaviour of proofReportFor.
// onFirstProof runs once, when the first proof job is claimed and before it is
// answered; onValidation runs when the real validation is claimed.
func playJudge(t *testing.T, s *site, how map[string]string, onFirstProof, onValidation func()) {
	t.Helper()
	ctx := context.Background()
	kinds := []string{judgeapi.KindGenerateInputs, judgeapi.KindGenerate, judgeapi.KindValidate}
	first := true
	report := func(rep judgeapi.Report) {
		t.Helper()
		if resp, out := s.judgeCall("POST", "/api/judge/report", judgeToken, rep); resp.StatusCode >= 300 {
			t.Fatalf("report: %d %s", resp.StatusCode, out)
		}
	}
	upload := func(c judgeapi.Claim, body string) string {
		t.Helper()
		if ok, err := s.bank.Jobs.MayUpload(ctx, c.JobID, c.Lease); err != nil || !ok {
			t.Fatalf("upload refused: %v", err)
		}
		sha := problems.SHA([]byte(body))
		if err := s.bank.Blobs.UploadOutput(strings.NewReader(body), sha, problems.MaxPackFile); err != nil {
			t.Fatal(err)
		}
		s.bank.Jobs.RecordUpload(c.JobID, sha)
		return sha
	}
	for step := 0; step < 60; step++ {
		resp, out := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: kinds, Worker: "fake"})
		if resp.StatusCode == 204 {
			return
		}
		if resp.StatusCode != 200 {
			t.Fatalf("claim: %d %s", resp.StatusCode, out)
		}
		var c judgeapi.Claim
		json.Unmarshal(out, &c)
		rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
		switch c.Kind {
		case judgeapi.KindGenerateInputs:
			var pl judgeapi.GenerateInputsPayload
			json.Unmarshal(c.Payload, &pl)
			rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleGenerator, Verdict: "OK"}}
			for _, pt := range pl.Tests {
				sha := upload(c, fmt.Sprintf("1\n%d\n", pt.Idx))
				rep.Results = append(rep.Results, judgeapi.TestResult{Idx: pt.Idx, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: sha})
			}
		case judgeapi.KindGenerate:
			var pl judgeapi.GeneratePayload
			json.Unmarshal(c.Payload, &pl)
			rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}
			for _, in := range pl.Inputs {
				sha := upload(c, fmt.Sprintf("%d\n", in.Idx))
				rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
			}
		case judgeapi.KindValidate:
			var pl proofJob
			json.Unmarshal(c.Payload, &pl)
			if pl.Proof != nil {
				if first {
					first = false
					onFirstProof()
				}
				rep = proofReportFor(c, pl, how[pl.Proof.Language+"/"+pl.Proof.Part])
			} else {
				onValidation()
				rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "OK"}}
				roles := []string{judgeapi.RoleReference}
				if pl.Programs.Brute != nil {
					roles = append(roles, judgeapi.RoleBrute)
					rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleBrute, Verdict: "OK"})
				}
				if pl.Programs.Validator != nil {
					roles = append(roles, judgeapi.RoleValidator)
					rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleValidator, Verdict: "OK"})
				}
				for _, test := range pl.Tests {
					for _, role := range roles {
						rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: role, Verdict: "AC", TimeMS: 5, MemoryKB: 2048})
					}
				}
			}
		}
		report(rep)
	}
	t.Fatal("the judge never ran out of jobs")
}

// proofReportFor is the report of a proof job. how is "" (the solution is
// right, the untouched starting code is wrong), "wa", "ce" (does not compile)
// or "accepted" (a starter job accepted on every test).
func proofReportFor(c judgeapi.Claim, pl proofJob, how string) judgeapi.Report {
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
	if how == "ce" {
		rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "CE", Output: "Main.java:3: error: ';' expected"}}
		return rep
	}
	rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "OK"}}
	for _, test := range pl.Tests {
		v := "AC"
		if (pl.Proof.Part == "starter" && how != "accepted") || how == "wa" {
			v = "WA"
		}
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: judgeapi.RoleReference, Verdict: v, TimeMS: 5, MemoryKB: 1024})
	}
	return rep
}

func TestWizardFunctionRunEndToEnd(t *testing.T) {
	all := []string{"cpp17", "java21", "node22", "python3"}
	for _, tc := range []struct {
		name, variant string
		how           map[string]string
		removed       string
		reason        string
	}{
		{"every language passes", "", nil, "", ""},
		{"a wrong Java solution", ai.VariantJavaWrong, map[string]string{"java21/solution": "wa"}, "java21", "wrong_answer"},
		{"a Java starting code that does not compile", ai.VariantStarterBroken, map[string]string{"java21/starter": "ce"}, "java21", "compile_error"},
		{"a Python bottom that ignores the function", ai.VariantIgnoresFunction, map[string]string{"python3/starter": "accepted"}, "python3", "ignores_function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSiteMode(t, "testing")
			org := s.testSignIn("ta@college.edu", "organiser")
			if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
				t.Fatalf("fake key: %d %v", code, out)
			}
			topic := "Sum of an array"
			if tc.variant != "" {
				topic += " variant:" + tc.variant
			}
			code, started := org.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "brief",
				"brief": map[string]any{"topic": topic, "difficulty": "easy"}, "students_write": "function"}, s.srv.URL)
			if code != 202 {
				t.Fatalf("wizard: %d %v", code, started)
			}
			w := pollWizard(t, org, started["wizard_id"].(string))
			if w["state"] != "draft_created" {
				t.Fatalf("wizard: %v", w)
			}
			pid := idOf(w["problem_id"])
			var id int64
			fmt.Sscan(pid, &id)

			problem := func() map[string]any {
				_, p := org.do("GET", "/api/admin/problems/"+pid, nil, "")
				return p
			}
			if p := problem(); p["validated"] != false {
				t.Fatalf("a fresh draft is validated: %v", p["validated"])
			}
			sawRound, sawValidation := false, false
			playJudge(t, s, tc.how, func() {
				sawRound = true
				code, body := org.do("POST", "/api/admin/problems/"+pid+"/validate", nil, s.srv.URL)
				errIs2(t, "validate mid-round", code, body, 409, "checking", "Podium is still checking the languages of this problem.")
				if _, st := org.do("GET", proofPath(id), nil, ""); st["checking"] != true || st["progress"].(map[string]any)["total"] != float64(7) {
					t.Errorf("proof state mid-round: %v", st)
				}
			}, func() {
				sawValidation = true
				if p := problem(); p["validated"] != false {
					t.Errorf("validated before the real validation ran")
				}
				if _, st := org.do("GET", proofPath(id), nil, ""); st["checking"] != false {
					t.Errorf("still checking when the validation was claimed: %v", st)
				}
			})
			if !sawRound || !sawValidation {
				t.Fatalf("proof round seen %v, validation seen %v", sawRound, sawValidation)
			}

			p := problem()
			var offered []string
			for lang := range p["locked_bottom"].(map[string]any) {
				offered = append(offered, lang)
			}
			sort.Strings(offered)
			want := slices.DeleteFunc(slices.Clone(all), func(l string) bool { return l == tc.removed })
			if !slices.Equal(offered, want) {
				t.Errorf("offered %v, want %v", offered, want)
			}
			if p["validated"] != true {
				t.Errorf("not validated after the real validation: %v", p["validated"])
			}
			_, st := org.do("GET", proofPath(id), nil, "")
			if st["checking"] != false || st["progress"] != nil {
				t.Errorf("proof state at the end: %v", st)
			}
			for _, raw := range st["languages"].([]any) {
				l := raw.(map[string]any)
				lang := l["language"].(string)
				if l["source"] != "wizard" || l["current"] != true {
					t.Errorf("%s: %v", lang, l)
				}
				if lang == tc.removed {
					if l["result"] != "failed" || l["reason"] != tc.reason || l["offered"] != false {
						t.Errorf("%s: %v, want failed %s and not offered", lang, l, tc.reason)
					}
					if tc.reason == "compile_error" && !strings.Contains(fmt.Sprint(l["compiler"]), "';' expected") {
						t.Errorf("compiler lines: %v", l["compiler"])
					}
					if tc.reason == "wrong_answer" && (l["failed"] != l["total"] || l["total"].(float64) == 0) {
						t.Errorf("counts: %v", l)
					}
				} else if l["result"] != "passed" || l["offered"] != true {
					t.Errorf("%s: %v, want passed and offered", lang, l)
				}
			}
			applied, _ := st["applied"].(map[string]any)
			if applied == nil || applied["validating"] != true {
				t.Fatalf("applied: %v", st["applied"])
			}
			if got := fmt.Sprint(applied["removed"]); (tc.removed == "") != (got == "[]" || got == "<nil>") || (tc.removed != "" && got != "["+tc.removed+"]") {
				t.Errorf("applied removed %s", got)
			}
		})
	}
}
