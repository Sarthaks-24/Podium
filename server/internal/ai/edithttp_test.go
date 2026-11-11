package ai

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func draftOf(user, problem int64, size int) *editDraft {
	return &editDraft{id: strings.Repeat("a", 5) + time.Now().Format("150405.000000000"), userID: user, problemID: problem,
		files: map[string][]byte{"statement.md": make([]byte, size)}, created: time.Now()}
}

func TestDraftsPerUserCapEvictsTheOldest(t *testing.T) {
	var e editDrafts
	var first *editDraft
	for i := 0; i < maxDraftsPerUser+2; i++ {
		d := draftOf(1, int64(i+1), 10)
		d.id = string(rune('a' + i))
		d.created = time.Now().Add(time.Duration(i) * time.Second)
		if i == 0 {
			first = d
		}
		if err := e.put(d); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := e.get(first.id, 1, first.problemID); ok {
		t.Error("the oldest draft was kept")
	}
	if n := len(e.m); n != maxDraftsPerUser {
		t.Errorf("%d drafts kept", n)
	}
}

func TestDraftsTotalBytesCapRefuses(t *testing.T) {
	var e editDrafts
	per := maxDraftBytes/3 + 1
	for i := 0; i < 2; i++ {
		d := draftOf(int64(i+1), 1, per)
		d.id = string(rune('a' + i))
		if err := e.put(d); err != nil {
			t.Fatal(err)
		}
	}
	d := draftOf(3, 1, per)
	d.id = "z"
	if err := e.put(d); err != errDraftsFull {
		t.Errorf("third draft: %v", err)
	}
}

func TestDraftsExpireOnGet(t *testing.T) {
	var e editDrafts
	d := draftOf(1, 1, 10)
	d.id = "old"
	d.created = time.Now().Add(-2 * editKeep)
	e.m = map[string]*editDraft{d.id: d}
	if _, ok := e.get("old", 1, 1); ok {
		t.Error("an expired draft was returned")
	}
	if len(e.m) != 0 {
		t.Error("an expired draft was not swept")
	}
}

func TestDraftIsSingleUse(t *testing.T) {
	var e editDrafts
	d := draftOf(1, 1, 10)
	d.id = "x"
	e.put(d)
	if _, ok := e.begin("x", 1, 1); !ok {
		t.Fatal("first begin refused")
	}
	if _, ok := e.begin("x", 1, 1); ok {
		t.Error("a draft being accepted was handed out twice")
	}
	e.release("x")
	if _, ok := e.begin("x", 1, 1); !ok {
		t.Error("a released draft was refused")
	}
}

// A draft stuck in accepting is dropped once its deadline passes, and an
// accept that is still within it is left alone.
func TestStuckAcceptingDraftIsSwept(t *testing.T) {
	var e editDrafts
	d := draftOf(1, 1, 10)
	d.id = "stuck"
	if err := e.put(d); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.begin("stuck", 1, 1); !ok {
		t.Fatal("begin refused a fresh draft")
	}
	e.mu.Lock()
	e.sweep()
	e.mu.Unlock()
	if _, ok := e.m["stuck"]; !ok {
		t.Fatal("a draft within its accept deadline was swept")
	}
	e.mu.Lock()
	d.acceptingSince = time.Now().Add(-acceptingKeep - time.Second)
	d.created = time.Now().Add(-editKeep - time.Second)
	e.sweep()
	e.mu.Unlock()
	if _, ok := e.m["stuck"]; ok {
		t.Error("a draft stuck in accepting was never swept")
	}
}

// sent_back counts summaries, paths and raw text against the cap and finds
// test data under any spelling of the folder.
func TestSentBackCapCountsEverythingAndHidesTestData(t *testing.T) {
	var files []EditFile
	for _, p := range []string{"tests/1.in", "./tests/1.in", "Tests/1.in", `tests\1.in`, "/tests/1.in", "keep.py"} {
		files = append(files, EditFile{Path: p, Content: "x"})
	}
	files = append(files, EditFile{Path: strings.Repeat("p", 150<<10), Content: ""})
	out := sentBack([]*EditAnswer{{Summary: strings.Repeat("s", 120<<10), Files: files}})
	a := out["answers"][0]
	total := len(a.Summary) + len(a.Raw)
	for _, f := range a.Files {
		total += len(f.Path) + len(f.Content)
		if strings.Contains(strings.ToLower(f.Path), "tests") {
			t.Errorf("test data shown: %q", f.Path)
		}
	}
	if total > maxSentBack || !a.Cut {
		t.Errorf("%d bytes shown, cut %v", total, a.Cut)
	}
}

func TestEditUserPromptCarriesTheReasonsOfARetry(t *testing.T) {
	p := EditUserPrompt(EditRequest{Instruction: "x", Fix: []string{"problem.yaml: bad"}})
	if !strings.Contains(p, "- problem.yaml: bad") {
		t.Errorf("the reasons are not in the prompt: %s", p)
	}
	if p := EditUserPrompt(EditRequest{Instruction: "x", Previous: "{}", Fix: []string{"r"}}); strings.Contains(p, "- r") {
		t.Error("the reasons are sent twice when the previous answer carries them as a turn")
	}
}

// A failure of the provider call that is not an *Error is logged with the key
// scrubbed and answered as 502 with a fixed sentence.
func TestEditRefusedScrubsAProviderFault(t *testing.T) {
	var logs bytes.Buffer
	d := &Handlers{Log: slog.New(slog.NewJSONHandler(&logs, nil))}
	const key = "PODIUMTESTSECRETKEY123456"
	w := httptest.NewRecorder()
	d.editRefused(w, httptest.NewRequest("POST", "/", nil), &providerFault{errors.New("transport broke with " + key)}, 1, 2,
		KeyEntry{Provider: "anthropic", Key: NewSecret(key)}, nil)
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "transport") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if !strings.Contains(logs.String(), `"msg":"ai edit failed"`) || strings.Contains(logs.String(), key) || !strings.Contains(logs.String(), "[redacted]") {
		t.Errorf("log: %s", logs.String())
	}
}
