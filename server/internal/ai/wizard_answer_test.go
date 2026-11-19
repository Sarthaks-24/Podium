package ai

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// answerRun runs one function wizard run whose two attempts give answers,
// and returns the handlers and the finished run's id.
func answerRun(t *testing.T, user int64, key Secret, answers ...func() (*FunctionPack, error)) (*Handlers, string) {
	t.Helper()
	d := &Handlers{Wizard: &Wizard{}, Log: slog.New(slog.DiscardHandler)}
	run := d.Wizard.start("generate", user, 0, KeyEntry{Provider: ProviderFake}, nil)
	d.runGenerate(run, &stub{functionAnswers: answers}, Request{Pasted: "sum", JavaLists: JavaListsArray}, true, user, key)
	return d, run.id
}

func unusable(text string) func() (*FunctionPack, error) {
	return func() (*FunctionPack, error) {
		return nil, &Error{Code: CodeInvalidAnswer, Message: "x", Reasons: []string{"bad shape"}, answer: text}
	}
}

func getAnswer(d *Handlers, id string, u auth.User) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/admin/ai/wizard/"+id+"/answer", nil)
	r.SetPathValue("id", id)
	r = r.WithContext(auth.WithUser(r.Context(), u))
	w := httptest.NewRecorder()
	d.GetWizardAnswer(w, r)
	return w
}

func TestWizardAnswerOnlyForItsOwner(t *testing.T) {
	d, id := answerRun(t, 7, Secret{}, unusable(`{"first": 1}`), unusable(`{"second": 2}`))
	w := getAnswer(d, id, auth.User{ID: 7, Role: auth.Organiser})
	var got wizardAnswerJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
		t.Fatalf("owner of the run: %d %s", w.Code, w.Body)
	}
	if got.Text != `{"second": 2}` || got.Cut || len(got.Reasons) != 1 || got.Reasons[0] != "bad shape" {
		t.Errorf("answer %+v", got)
	}
	for _, role := range []auth.Role{auth.Organiser, auth.Admin, auth.Owner} {
		if w := getAnswer(d, id, auth.User{ID: 8, Role: role}); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), noWizard) {
			t.Errorf("%s reading another person's run: %d %s", role, w.Code, w.Body)
		}
	}
	if w := getAnswer(d, "nope", auth.User{ID: 7}); w.Code != http.StatusNotFound {
		t.Errorf("missing run: %d", w.Code)
	}
	// The wizard event and GET of the run never carry the text.
	d.Wizard.mu.Lock()
	run := d.Wizard.runs[id]
	b, _ := json.Marshal(run.json())
	run.finishedAt = time.Now().Add(-wizardKeep - time.Minute)
	d.Wizard.mu.Unlock()
	if strings.Contains(string(b), "second") {
		t.Errorf("the run's JSON carries the answer: %s", b)
	}
	if w := getAnswer(d, id, auth.User{ID: 7}); w.Code != http.StatusNotFound {
		t.Errorf("after the keep time: %d", w.Code)
	}
}

func TestWizardAnswerUnfinishedRun(t *testing.T) {
	d := &Handlers{Wizard: &Wizard{}, Log: slog.New(slog.DiscardHandler)}
	run := d.Wizard.start("generate", 7, 0, KeyEntry{}, nil)
	if w := getAnswer(d, run.id, auth.User{ID: 7}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not_finished") {
		t.Errorf("unfinished run: %d %s", w.Code, w.Body)
	}
}

func TestWizardAnswerDropsTheKeyBeforeTheCut(t *testing.T) {
	const key = "KEYZZZ9876543210"
	// The key sits across the cut point, then once more after it.
	text := strings.Repeat("a", maxKeptAnswer-6) + key + strings.Repeat("b", 100) + key
	d, id := answerRun(t, 7, NewSecret(key), unusable(text), unusable(text))
	w := getAnswer(d, id, auth.User{ID: 7})
	var got wizardAnswerJSON
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if !got.Cut || len(got.Text) != maxKeptAnswer || strings.Contains(got.Text, "KEYZ") {
		t.Errorf("cut %v len %d, key part kept %v", got.Cut, len(got.Text), strings.Contains(got.Text, "KEYZ"))
	}
	if !strings.HasSuffix(got.Text, "a[redac") {
		t.Errorf("the redaction is not at the cut point: %q", got.Text[len(got.Text)-12:])
	}
}

func TestWizardAnswerSurvivesARetryTimeout(t *testing.T) {
	timeout := func() (*FunctionPack, error) { return nil, &Error{Code: CodeTimeout, Message: "slow"} }
	d, id := answerRun(t, 7, Secret{}, unusable(`{"first": 1}`), timeout)
	var got wizardAnswerJSON
	_ = json.Unmarshal(getAnswer(d, id, auth.User{ID: 7}).Body.Bytes(), &got)
	if got.Text != `{"first": 1}` {
		t.Errorf("answer after a retry timeout: %q", got.Text)
	}
}

func TestWizardAnswerReplacedByTheNextRun(t *testing.T) {
	d, id := answerRun(t, 7, Secret{}, unusable(`{"first": 1}`), unusable(`{"first": 2}`))
	other := d.Wizard.start("generate", 9, 0, KeyEntry{}, nil)
	_ = other
	var got wizardAnswerJSON
	_ = json.Unmarshal(getAnswer(d, id, auth.User{ID: 7}).Body.Bytes(), &got)
	if got.Text == "" {
		t.Fatal("another person's run dropped this answer")
	}
	d.Wizard.start("generate", 7, 0, KeyEntry{}, nil)
	got = wizardAnswerJSON{}
	_ = json.Unmarshal(getAnswer(d, id, auth.User{ID: 7}).Body.Bytes(), &got)
	if got.Text != "" {
		t.Errorf("a new run kept the old answer: %q", got.Text)
	}
}

func TestWizardAnswerOfADecodedPackIsTheProviderText(t *testing.T) {
	raw := fnAnswer(FakeFunctionArraySum())
	pack, err := DecodeFunctionFor(raw, JavaListsArray)
	if err != nil {
		t.Fatal(err)
	}
	if got := answerText(pack, nil); got != raw {
		t.Error("a decoded pack does not give back the provider's text")
	}
	if got := answerText(FakeFunctionArraySum(), nil); !strings.Contains(got, `"slug":"array-sum"`) {
		t.Errorf("a pack with no text: %q", got)
	}
}
