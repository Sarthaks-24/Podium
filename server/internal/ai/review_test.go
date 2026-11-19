package ai

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/problems"
)

func TestLoadAnswerFunctionRepairsAndChecks(t *testing.T) {
	p := FakeFunctionArraySum()
	p.Languages[1].Solution = strings.ReplaceAll(p.Languages[1].Solution, "\n", `\n`)
	pack, notes, reasons, err := loadAnswer(fnAnswer(p), StudentsWriteFunction, JavaListsArray)
	if err != nil || len(reasons) != 0 || pack == nil || !pack.function {
		t.Fatalf("pack %v notes %v reasons %v err %v", pack, notes, reasons, err)
	}
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), "line breaks") {
		t.Errorf("no repair note: %v", notes)
	}
	if len(pack.solutions) == 0 || pack.draft.Problem.Mode != problems.ModeFunction {
		t.Errorf("draft mode %q solutions %v", pack.draft.Problem.Mode, pack.solutions)
	}
}

func TestLoadAnswerNamesTheField(t *testing.T) {
	p := FakeFunctionArraySum()
	p.Subtasks[0].Points = 7
	_, _, reasons, err := loadAnswer(fnAnswer(p), StudentsWriteFunction, JavaListsArray)
	if err != nil || len(reasons) == 0 || !strings.Contains(strings.Join(reasons, " "), "add up to") {
		t.Errorf("reasons %v err %v", reasons, err)
	}
	// A missing key is a decode refusal that names the key.
	var m map[string]any
	_ = json.Unmarshal([]byte(fnAnswer(FakeFunctionArraySum())), &m)
	delete(m, "reference_cpp")
	b, _ := json.Marshal(m)
	pack, _, _, err := loadAnswer(string(b), StudentsWriteFunction, JavaListsArray)
	var e *Error
	if pack != nil || !errors.As(err, &e) || !strings.Contains(strings.Join(e.Reasons, " "), "reference_cpp") {
		t.Errorf("missing key: %v %v", pack, err)
	}
}

func TestLoadAnswerProgram(t *testing.T) {
	p := FakeMaxSubarray()
	p.Subtasks[0].GeneratorArgs = []string{"n=5,k=5"}
	b, _ := json.Marshal(p)
	pack, notes, reasons, err := loadAnswer(string(b), StudentsWriteProgram, "")
	if err != nil || len(reasons) != 0 || pack == nil || pack.function {
		t.Fatalf("pack %v reasons %v err %v", pack, reasons, err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "n=5") {
		t.Errorf("notes %v", notes)
	}
	if got := pack.draft.Problem.Subtasks[0].GeneratorArgs; len(got) != 2 {
		t.Errorf("args %v", got)
	}
}

func TestLoadAnswerRefusesBadKindAndOversize(t *testing.T) {
	if _, _, _, err := loadAnswer("{}", "both", ""); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, _, _, err := loadAnswer(strings.Repeat("a", maxReviewAnswer+1), StudentsWriteFunction, ""); err == nil {
		t.Error("an answer over the limit was accepted")
	}
}

func TestKeyShapedAnswerIsRefused(t *testing.T) {
	for _, k := range []string{"sk-" + "ant-api03-abcdefghijklmnop123456", "gsk" + "_abcdefghijklmnopqrstuv1234", "AIza" + "SyABCDEFGHIJKLMNOPQRSTUV"} {
		if !hasKeyShape(`{"statement_md": "see ` + k + ` here"}`) {
			t.Errorf("%s not refused", k)
		}
	}
	for _, ok := range []string{"ask-me-anything", "the disk-usage-of-every-machine-today", "task-description-of-the-problem", "AIzabc"} {
		if hasKeyShape(ok) {
			t.Errorf("%q refused", ok)
		}
	}
}

// legalAnswer builds the largest answer the checks accept, with every string
// made of characters JSON writes as two bytes (quote, backslash, newline).
func legalAnswer() *FunctionPack {
	p := FakeFunctionArraySum()
	fill := func(n int) string { return strings.Repeat("\"\\\n", n/3) }
	p.StatementMD = "## Input\n## Output\n## Constraints\n" + fill(problems.MaxStatement-40)
	p.Reference, p.Brute, p.Generator, p.Validator = fill(problems.MaxSource), fill(problems.MaxSource), fill(problems.MaxSource), fill(problems.MaxSource)
	for i := range p.Samples {
		p.Samples[i].Input, p.Samples[i].Output = fill(maxSample), fill(maxSample)
	}
	for i := range p.Languages {
		l := &p.Languages[i]
		l.LockedTop, l.Starter, l.LockedBottom = fill(problems.MaxStarter), fill(problems.MaxStarter), fill(problems.MaxStarter)
		l.Solution = fill(problems.MaxSource)
	}
	return p
}

func TestLargestLegalAnswerFitsBothLimits(t *testing.T) {
	text := fnAnswer(legalAnswer())
	if len(text) > maxReviewAnswer {
		t.Fatalf("the largest legal answer is %d bytes, over the %d limit", len(text), maxReviewAnswer)
	}
	body, _ := json.Marshal(map[string]string{"answer": text, "kind": "function", "java_lists": "array"})
	if len(body) > maxReviewBody {
		t.Fatalf("its request body is %d bytes, over the %d limit", len(body), maxReviewBody)
	}
	t.Logf("largest legal answer %d bytes, request body %d bytes (limits %d and %d)", len(text), len(body), maxReviewAnswer, maxReviewBody)
}

func reviewHandlers() *Handlers {
	return &Handlers{Wizard: &Wizard{Slots: &Slots{}}, Log: slog.New(slog.DiscardHandler)}
}

func newReviewRun(d *Handlers, function bool) *wizardRun {
	run := d.Wizard.start("generate", 7, 0, KeyEntry{Provider: ProviderFake}, nil)
	d.Wizard.mark(run, function, JavaListsArray, true)
	return run
}

func fnReq() Request { return Request{Pasted: "sum", JavaLists: JavaListsArray} }

func TestReviewRunKeepsTheAnswerAndCreatesNothing(t *testing.T) {
	d := reviewHandlers() // no Problems service: a draft would panic
	run := newReviewRun(d, true)
	good := func() (*FunctionPack, error) { return FakeFunctionArraySum(), nil }
	got := d.runGenerate(run, &stub{functionAnswers: []func() (*FunctionPack, error){good}}, fnReq(), true, 7, Secret{})
	if got.State != wizardReview || got.ProblemID != nil || got.Error != nil {
		t.Fatalf("run %+v", got)
	}
	v := getAnswerJSON(t, d, run.id, 7)
	if v.Kind != StudentsWriteFunction || v.JavaLists != JavaListsArray || len(v.Reasons) != 0 || v.Text == "" || v.Repairs == nil {
		t.Errorf("answer %+v", v)
	}
}

func TestReviewRunWithAFailedAnswerEndsInReview(t *testing.T) {
	d := reviewHandlers()
	run := newReviewRun(d, true)
	got := d.runGenerate(run, &stub{functionAnswers: []func() (*FunctionPack, error){unusable(`{"a":1}`), unusable(`{"a":2}`)}}, fnReq(), true, 7, Secret{})
	if got.State != wizardReview || got.Error != nil {
		t.Fatalf("run %+v", got)
	}
	v := getAnswerJSON(t, d, run.id, 7)
	if v.Text != `{"a":2}` || len(v.Reasons) == 0 {
		t.Errorf("answer %+v", v)
	}
}

func TestReviewRunEndsInErrorWhenTheProviderSentNothing(t *testing.T) {
	d := reviewHandlers()
	run := newReviewRun(d, true)
	down := func() (*FunctionPack, error) { return nil, &Error{Code: CodeUnavailable, Message: "down"} }
	got := d.runGenerate(run, &stub{functionAnswers: []func() (*FunctionPack, error){down}}, fnReq(), true, 7, Secret{})
	if got.State != wizardFailed || got.Error == nil {
		t.Errorf("run %+v", got)
	}
}

func TestReviewKeepsTwoMegabytesAndCutsAbove(t *testing.T) {
	d := reviewHandlers()
	run := newReviewRun(d, true)
	big := func() (*FunctionPack, error) {
		return nil, &Error{Code: CodeInvalidAnswer, Message: "x", Reasons: []string{"r"}, answer: strings.Repeat("a", maxReviewAnswer+10)}
	}
	d.runGenerate(run, &stub{functionAnswers: []func() (*FunctionPack, error){big, big}}, fnReq(), true, 7, Secret{})
	v := getAnswerJSON(t, d, run.id, 7)
	if !v.Cut || len(v.Text) != maxReviewAnswer || len(v.Repairs) != 0 {
		t.Errorf("cut %v len %d repairs %v", v.Cut, len(v.Text), v.Repairs)
	}
}

func TestAnswerAfterACheckFailureIsWhatTheModelSent(t *testing.T) {
	p := FakeFunctionArraySum()
	p.Languages[1].Solution = strings.ReplaceAll(p.Languages[1].Solution, "\n", `\n`)
	p.Subtasks[0].Points = 7 // fails the check after the repair
	raw := fnAnswer(p)
	d := reviewHandlers()
	run := newReviewRun(d, true)
	bad := func() (*FunctionPack, error) { return DecodeFunctionFor(raw, JavaListsArray) }
	d.runGenerate(run, &stub{functionAnswers: []func() (*FunctionPack, error){bad, bad}}, fnReq(), true, 7, Secret{})
	v := getAnswerJSON(t, d, run.id, 7)
	if v.Text != raw || len(v.Reasons) == 0 || len(v.Repairs) == 0 {
		t.Errorf("text kept %v, reasons %v, repairs %v", v.Text == raw, v.Reasons, v.Repairs)
	}
}

func getAnswerJSON(t *testing.T, d *Handlers, id string, user int64) wizardAnswerJSON {
	t.Helper()
	w := getAnswer(d, id, auth.User{ID: user, Role: auth.Organiser})
	var v wizardAnswerJSON
	if err := json.Unmarshal(w.Body.Bytes(), &v); w.Code != http.StatusOK || err != nil {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	return v
}

func TestDraftsRouteRefusals(t *testing.T) {
	d := reviewHandlers()
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/admin/ai/drafts", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(auth.WithUser(r.Context(), auth.User{ID: 7, Role: auth.Organiser}))
		w := httptest.NewRecorder()
		d.PostDraft(w, r)
		return w
	}
	enc := func(answer, kind, lists string) string {
		b, _ := json.Marshal(map[string]string{"answer": answer, "kind": kind, "java_lists": lists})
		return string(b)
	}
	if w := post(enc("{}", "both", "")); w.Code != 400 {
		t.Errorf("bad kind: %d", w.Code)
	}
	if w := post(enc("{}", "function", "sometimes")); w.Code != 400 {
		t.Errorf("bad lists: %d", w.Code)
	}
	if w := post(enc(strings.Repeat("a", maxReviewAnswer+1), "function", "array")); w.Code != 413 || !strings.Contains(w.Body.String(), "answer_too_large") {
		t.Errorf("answer over the limit: %d %s", w.Code, w.Body)
	}
	if w := post(`{"answer":"` + strings.Repeat(`\"`, maxReviewBody/2+10) + `","kind":"function"}`); w.Code != 413 || !strings.Contains(w.Body.String(), "answer_too_large") {
		t.Errorf("body over the limit: %d", w.Code)
	}
	w := post(enc(`{"x": "sk-`+`ant-api03-abcdefghijklmnop123456"}`, "function", "array"))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_answer") || strings.Contains(w.Body.String(), "abcdefghijklmnop") {
		t.Errorf("key-shaped answer: %d %s", w.Code, w.Body)
	}
	w = post(enc("not json", "function", "array"))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_answer") {
		t.Errorf("not json: %d %s", w.Code, w.Body)
	}
	release, _ := d.Wizard.Slots.Take(7)
	if w := post(enc(fnAnswer(FakeFunctionArraySum()), "function", "array")); w.Code != 429 {
		t.Errorf("slot taken: %d", w.Code)
	}
	release()
}

// TestKeptAnswerMemory measures what one kept 2 MB answer holds in memory
// and what one read of it allocates. Run with -v to see the figures.
func TestKeptAnswerMemory(t *testing.T) {
	d := reviewHandlers()
	heap := func() uint64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	before := heap()
	text := fnAnswer(legalAnswer())
	text += strings.Repeat(" ", maxReviewAnswer-len(text)) // exactly 2 MB
	run := newReviewRun(d, true)
	d.Wizard.finish(run, func(r *wizardRun) { r.state, r.answer = wizardReview, clipText(text, maxReviewAnswer) })
	text = ""
	kept := heap() - before
	read := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if w := getAnswer(d, run.id, auth.User{ID: 7}); w.Code != 200 {
				b.Fatal(w.Code)
			}
		}
	})
	t.Logf("kept 2 MB answer: %d bytes (%.2f MB) held on the heap while the run is kept; one read of it allocates %.1f MB, freed after the response",
		kept, float64(kept)/(1<<20), float64(read.AllocedBytesPerOp())/(1<<20))
	if kept > 3<<20 {
		t.Errorf("a kept answer holds %d bytes", kept)
	}
	runtime.KeepAlive(run)
}
