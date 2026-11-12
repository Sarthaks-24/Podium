package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/problems"
)

func fnAnswer(p *FunctionPack) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func TestFunctionDecodeIsStrict(t *testing.T) {
	good := fnAnswer(FakeFunctionArraySum())
	p, err := DecodeFunction(good)
	if err != nil || len(p.Languages) != 4 || p.Brute == "" {
		t.Fatalf("good answer: %v %v", p, err)
	}

	// Each change below must be refused as an unusable answer.
	var base map[string]any
	json.Unmarshal([]byte(good), &base)
	change := func(f func(m map[string]any)) string {
		var m map[string]any
		json.Unmarshal([]byte(good), &m)
		f(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	for name, text := range map[string]string{
		"unknown key":          change(func(m map[string]any) { m["surprise"] = 1 }),
		"unknown nested key":   change(func(m map[string]any) { m["languages"].([]any)[0].(map[string]any)["extra"] = "x" }),
		"missing key":          change(func(m map[string]any) { delete(m, "brute_cpp") }),
		"missing nested key":   change(func(m map[string]any) { delete(m["languages"].([]any)[1].(map[string]any), "locked_top") }),
		"missing array item":   change(func(m map[string]any) { delete(m["subtasks"].([]any)[0].(map[string]any), "points") }),
		"wrong type":           change(func(m map[string]any) { m["time_limit_ms"] = "1000" }),
		"wrong nested type":    change(func(m map[string]any) { m["languages"].([]any)[0].(map[string]any)["starter"] = 7 }),
		"cut off":              good[:len(good)/2],
		"empty":                "",
		"a whole-program pack": fnAnswer2(FakePairSums()),
	} {
		_, err := DecodeFunction(text)
		e, ok := err.(*Error)
		if !ok || e.Code != CodeInvalidAnswer || e.answer != text || len(e.Reasons) == 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func fnAnswer2(p *WizardPack) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func TestFunctionSchemaIsStrict(t *testing.T) {
	s := FunctionSchema()
	assertStrictSchema(t, s)
	props := s["properties"].(map[string]any)
	var want []string
	rt := reflect.TypeOf(FunctionPack{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if strings.HasSuffix(tag, ",omitempty") { // java_solution_arraylist: only the "either" schema has it
			continue
		}
		want = append(want, strings.Split(tag, ",")[0])
	}
	if len(props) != len(want) {
		t.Errorf("schema has %d properties, the type %d", len(props), len(want))
	}
	for _, k := range want {
		if props[k] == nil {
			t.Errorf("schema lacks %s", k)
		}
	}
	lang := props["languages"].(map[string]any)["items"].(map[string]any)
	if got := lang["properties"].(map[string]any)["language"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"cpp17", "java21", "python3", "node22"}) {
		t.Errorf("language enum %v", got)
	}
}

func TestFakeFunctionPackPassesChecks(t *testing.T) {
	p := FakeFunctionArraySum()
	if r := CheckFunction(p); len(r) > 0 {
		t.Fatalf("check: %v", r)
	}
	d := ToDraftFunction(p)
	if errs := problems.CheckDraft(d); errs != nil {
		t.Fatalf("draft: %v", errs)
	}
	pr := d.Problem
	if pr.Mode != problems.ModeFunction || pr.Reference.Language != "cpp17" || pr.Brute == nil || pr.Brute.Language != "cpp17" ||
		pr.Generator == nil || pr.Generator.Language != "python3" || pr.Validator == nil {
		t.Fatalf("problem %+v", pr)
	}
	if got := strings.Join(pr.Offered(), ","); got != "cpp17,java21,node22,python3" {
		t.Errorf("offered %s", got)
	}
	for _, l := range p.Languages {
		if pr.LockedTop[l.Language] != l.LockedTop || pr.LockedBottom[l.Language] != l.LockedBottom || pr.Starters[l.Language] != l.Starter {
			t.Errorf("%s parts not carried", l.Language)
		}
	}
	if len(d.Samples) != 2 || len(pr.Plan) != 4 {
		t.Errorf("samples %d plan %d", len(d.Samples), len(pr.Plan))
	}
	// The solutions go to the proof, not to the draft: only languages other
	// than the reference need one.
	sol := p.Solutions()
	if len(sol) != 3 || sol["cpp17"] != "" || !strings.Contains(sol["java21"], "long s") || sol["python3"] == "" || sol["node22"] == "" {
		t.Errorf("solutions %v", sol)
	}
	if !reflect.DeepEqual(ToDraftFunction(p), d) {
		t.Error("ToDraftFunction is not deterministic")
	}
}

func TestFunctionVariants(t *testing.T) {
	base := FakeFunctionArraySum()
	if FakeFunctionVariant("no-such-variant") != nil {
		t.Error("an unknown variant returned a pack")
	}
	lang := func(p *FunctionPack, id string) FunctionLanguage {
		for _, l := range p.Languages {
			if l.Language == id {
				return l
			}
		}
		t.Fatalf("no %s", id)
		return FunctionLanguage{}
	}
	for _, c := range []struct {
		name, language string
		differs        func(a, b FunctionLanguage) bool
	}{
		{VariantJavaWrong, "java21", func(a, b FunctionLanguage) bool { return a.Solution != b.Solution }},
		{VariantStarterBroken, "java21", func(a, b FunctionLanguage) bool { return a.Starter != b.Starter }},
		{VariantExtraOutput, "python3", func(a, b FunctionLanguage) bool {
			return strings.Contains(b.LockedBottom, "done") && a.LockedBottom != b.LockedBottom
		}},
		{VariantIgnoresFunction, "python3", func(a, b FunctionLanguage) bool {
			return !strings.Contains(b.LockedBottom, "solve(") && strings.Contains(b.LockedBottom, "print(sum(a))") && a.Solution == b.Solution
		}},
		{VariantReferenceFails, "cpp17", func(a, b FunctionLanguage) bool { return a.Starter != b.Starter }},
	} {
		v := FakeFunctionVariant(c.name)
		if v == nil {
			t.Errorf("%s: no pack", c.name)
			continue
		}
		if r := CheckFunction(v); len(r) > 0 {
			t.Errorf("%s: a variant must still pass the checks: %v", c.name, r)
		}
		if !c.differs(lang(base, c.language), lang(v, c.language)) {
			t.Errorf("%s: %s part not changed", c.name, c.language)
		}
		for _, l := range base.Languages {
			if l.Language != c.language && l != lang(v, l.Language) {
				t.Errorf("%s: %s changed too", c.name, l.Language)
			}
		}
	}
	// The base pack is not changed by taking a variant of it.
	if !reflect.DeepEqual(base, FakeFunctionArraySum()) {
		t.Error("a variant changed the base pack")
	}
}

func TestCheckFunctionReasons(t *testing.T) {
	long := strings.Repeat("x", problems.MaxStarter+1)
	for _, c := range []struct {
		name string
		edit func(p *FunctionPack)
		want string
	}{
		{"no samples", func(p *FunctionPack) { p.Samples = nil }, "samples"},
		{"points", func(p *FunctionPack) { p.Subtasks[0].Points = 1 }, "add up to"},
		{"headings", func(p *FunctionPack) { p.StatementMD = "no sections" }, `no "## Input" section`},
		{"empty reference", func(p *FunctionPack) { p.Reference = " " }, "reference_cpp is empty"},
		{"empty brute", func(p *FunctionPack) { p.Brute = "" }, "brute_cpp is empty"},
		{"empty generator", func(p *FunctionPack) { p.Generator = "" }, "generator_python is empty"},
		{"empty validator", func(p *FunctionPack) { p.Validator = "" }, "validator_python is empty"},
		{"no languages", func(p *FunctionPack) { p.Languages = nil }, "one to four languages"},
		{"five languages", func(p *FunctionPack) { p.Languages = append(p.Languages, p.Languages[0]) }, "one to four languages"},
		{"duplicate language", func(p *FunctionPack) { p.Languages[2].Language = "java21" }, "java21 twice"},
		{"no cpp17", func(p *FunctionPack) { p.Languages = p.Languages[1:] }, "cpp17"},
		{"unknown language", func(p *FunctionPack) { p.Languages[1].Language = "rust" }, `"rust"`},
		{"empty locked top", func(p *FunctionPack) { p.Languages[1].LockedTop = "  \n" }, "java21: locked_top is empty"},
		{"empty locked bottom", func(p *FunctionPack) { p.Languages[2].LockedBottom = "" }, "python3: locked_bottom is empty"},
		{"empty starter", func(p *FunctionPack) { p.Languages[3].Starter = "" }, "node22: starter is empty"},
		{"long starter", func(p *FunctionPack) { p.Languages[0].Starter = long }, "cpp17: starter is longer than 4 KB"},
		{"long locked top", func(p *FunctionPack) { p.Languages[0].LockedTop = long }, "cpp17: locked_top is longer than 4 KB"},
		{"long locked bottom", func(p *FunctionPack) { p.Languages[1].LockedBottom = long }, "java21: locked_bottom is longer than 4 KB"},
		{"empty solution", func(p *FunctionPack) { p.Languages[1].Solution = "" }, "java21: solution is empty"},
		{"long solution", func(p *FunctionPack) { p.Languages[2].Solution = strings.Repeat("x", problems.MaxSource+1) }, "python3: solution is longer"},
	} {
		p := FakeFunctionArraySum()
		c.edit(p)
		r := strings.Join(CheckFunction(p), " | ")
		if !strings.Contains(r, c.want) {
			t.Errorf("%s: want %q in %q", c.name, c.want, r)
		}
	}
	// A cpp17-only pack is fine, and its C++ solution may be empty (the
	// reference stands in for it).
	p := FakeFunctionArraySum()
	p.Languages = p.Languages[:1]
	p.Languages[0].Solution = ""
	if r := CheckFunction(p); len(r) > 0 {
		t.Errorf("cpp17 only: %v", r)
	}
	// The editor's rules still apply to the draft the pack becomes.
	p = FakeFunctionArraySum()
	p.TimeLimitMS = 50
	if r := strings.Join(CheckFunction(p), " | "); !strings.Contains(r, "time_limit_ms") {
		t.Errorf("draft rules: %q", r)
	}
}

func TestFunctionPromptFollowsJavaLists(t *testing.T) {
	array := BuildFunctionSystemPrompt(DefaultRules, "", "array")
	list := BuildFunctionSystemPrompt(DefaultRules, "", "arraylist")
	if array == list {
		t.Fatal("the Java lists choice does not change the prompt")
	}
	if !strings.Contains(array, "int[]") || strings.Contains(array, "ArrayList<Integer>") {
		t.Errorf("array prompt:\n%s", array)
	}
	if !strings.Contains(list, "ArrayList<Integer>") || strings.Contains(list, "int[]") {
		t.Errorf("arraylist prompt:\n%s", list)
	}
	for _, s := range []string{array, list} {
		if !strings.HasPrefix(s, fixedPrompt+"\n"+DefaultRules) {
			t.Error("the fixed part and the rules do not come first")
		}
		for _, want := range []string{"bare C++ function", "brute_cpp", "locked_top", "locked_bottom", "starter", "solution", "never names a signature"} {
			if !strings.Contains(s, want) {
				t.Errorf("function rules lack %q", want)
			}
		}
		if strings.Contains(s, "brute_python") && !strings.Contains(s, "read \"function\"") {
			t.Error("the override of the program rules is missing")
		}
	}
	withPrefs := BuildFunctionSystemPrompt(DefaultRules, "Prefer short statements.", "array")
	if !strings.Contains(withPrefs, "<preferences>\nPrefer short statements.\n</preferences>") || !strings.HasSuffix(withPrefs, closingPrompt) {
		t.Errorf("preferences block:\n%s", withPrefs)
	}
	// A request with no System of its own and a function run gets the
	// function prompt, never the whole-program one.
	if got := (Request{JavaLists: "arraylist"}).functionSystem(); got != BuildFunctionSystemPrompt(fileRules(), "", "arraylist") {
		t.Errorf("default function system prompt:\n%s", got)
	}
	if got := (Request{System: "mine"}).functionSystem(); got != "mine" {
		t.Errorf("explicit system %q", got)
	}
}

func TestGenerateFunctionRetriesOnceWithReasons(t *testing.T) {
	bad := func() (*FunctionPack, error) {
		p := FakeFunctionArraySum()
		p.Languages[1].Starter = ""
		return p, nil
	}
	good := func() (*FunctionPack, error) { return FakeFunctionArraySum(), nil }
	s := &stub{functionAnswers: []func() (*FunctionPack, error){bad, good}}
	pack, err := GenerateFunction(context.Background(), s, Request{Pasted: "sum an array", JavaLists: "array"})
	if err != nil || pack.Slug != "array-sum" || len(s.reqs) != 2 {
		t.Fatalf("pack %v err %v calls %d", pack, err, len(s.reqs))
	}
	if r := s.reqs[1]; len(r.Fix) == 0 || !strings.Contains(r.Fix[0], "java21: starter is empty") || !strings.Contains(r.Previous, `"starter":""`) || r.JavaLists != "array" {
		t.Errorf("retry request %+v", r)
	}
	s = &stub{functionAnswers: []func() (*FunctionPack, error){bad, bad, good}}
	_, err = GenerateFunction(context.Background(), s, Request{Pasted: "x"})
	if e, ok := err.(*Error); !ok || e.Code != CodeInvalidAnswer || len(e.Reasons) == 0 || len(s.reqs) != 2 {
		t.Errorf("after two bad answers: %v (%d calls)", err, len(s.reqs))
	}
	cut := func() (*FunctionPack, error) { return nil, &Error{Code: CodeCutOff, Message: "cut"} }
	s = &stub{functionAnswers: []func() (*FunctionPack, error){cut, good}}
	if _, err := GenerateFunction(context.Background(), s, Request{Pasted: "x"}); err != nil || !s.reqs[1].Shorter {
		t.Errorf("cut off retry: %v", err)
	}
	if _, err := GenerateFunction(context.Background(), &stub{}, Request{}); err == nil {
		t.Error("an empty request was accepted")
	}
}

// functionBodies runs one function generation, with one retry, through each
// real client against a stand-in, and returns the system text, the schema and
// the messages of the first request of each client.
type sentRequest struct {
	system, user string
	schema       map[string]any
	messages     int
	raw          string
}

func sendFunctionRequests(t *testing.T, req Request) map[string]sentRequest {
	t.Helper()
	retryDelay = func(int) time.Duration { return time.Millisecond }
	pack := FakeFunctionArraySum()
	if req.JavaLists == JavaListsEither {
		pack = FakeFunctionEither()
	}
	answer := fnAnswer(pack)
	out := map[string]sentRequest{}
	text := func(v any) string { b, _ := json.Marshal(v); return string(b) }

	fa := &fakeAPI{status: 200, body: stream(answer, "end_turn", ""), sse: true}
	srvA := httptest.NewServer(fa)
	t.Cleanup(srvA.Close)
	pa, err := (&Anthropic{Key: NewSecret(testKey), Model: testModel, BaseURL: srvA.URL, HTTPClient: &http.Client{Timeout: 10 * time.Second}}).GenerateFunction(context.Background(), req)
	if err != nil || len(pa.Languages) != 4 {
		t.Fatalf("anthropic: %v", err)
	}
	b := fa.reqs[0].body
	msgs := b["messages"].([]any)
	out["anthropic"] = sentRequest{
		system:   b["system"].([]any)[0].(map[string]any)["text"].(string),
		user:     text(msgs[0]),
		schema:   b["output_config"].(map[string]any)["format"].(map[string]any)["schema"].(map[string]any),
		messages: len(msgs), raw: text(b)}

	so, baseO := newStandIn(t, ok(openAIAnswer(answer)))
	po, err := (&OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: baseO}).GenerateFunction(context.Background(), req)
	if err != nil || len(po.Languages) != 4 {
		t.Fatalf("openai: %v", err)
	}
	b = so.reqs[0].body
	in := b["input"].([]any)
	out["openai"] = sentRequest{system: b["instructions"].(string), user: text(in[0]),
		schema: b["text"].(map[string]any)["format"].(map[string]any)["schema"].(map[string]any), messages: len(in), raw: text(b)}

	sg, baseG := newStandIn(t, ok(geminiAnswer(answer, "STOP")))
	pg, err := (&Gemini{Key: NewSecret(testKey), Model: "gemini-x", BaseURL: baseG}).GenerateFunction(context.Background(), req)
	if err != nil || len(pg.Languages) != 4 {
		t.Fatalf("gemini: %v", err)
	}
	b = sg.reqs[0].body
	ct := b["contents"].([]any)
	out["gemini"] = sentRequest{system: b["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string), user: text(ct[0]),
		schema: b["generationConfig"].(map[string]any)["responseSchema"].(map[string]any), messages: len(ct), raw: text(b)}
	return out
}

func TestProvidersSendTheFunctionSchema(t *testing.T) {
	req := Request{Brief: &Brief{Topic: "Sum of an array", Difficulty: "easy"}, JavaLists: "arraylist"}
	for name, r := range sendFunctionRequests(t, req) {
		props, _ := r.schema["properties"].(map[string]any)
		if props["languages"] == nil || props["brute_cpp"] == nil || props["reference_cpp"] == nil || props["brute_python"] != nil {
			t.Errorf("%s: not the function schema: %v", name, props)
		}
		if !strings.Contains(r.system, "ArrayList<Integer>") || !strings.Contains(r.system, "bare C++ function") || !strings.HasPrefix(r.system, fixedPrompt) {
			t.Errorf("%s: not the function prompt:\n%s", name, r.system)
		}
		if !strings.Contains(r.user, "Sum of an array") || r.messages != 1 {
			t.Errorf("%s: user message %s (%d messages)", name, r.user, r.messages)
		}
	}
}

// No hidden test can reach a model: a function run's request holds only the
// system prompt, the schema, the admin's brief and, on the retry, the
// model's own earlier answer with the reasons. The bodies are rebuilt from
// those inputs and compared, and a marker standing for a hidden test input
// (set where the test data of a problem would sit) is not in any of them.
func TestFunctionRequestsCarryNoTestData(t *testing.T) {
	const marker = "HIDDEN-TEST-MARKER-7f3a"
	req := Request{Brief: &Brief{Topic: "Sum of an array", Difficulty: "easy"}, JavaLists: "array"}
	for name, r := range sendFunctionRequests(t, req) {
		if strings.Contains(r.raw, marker) {
			t.Errorf("%s: marker in the request", name)
		}
		if !strings.Contains(r.raw, "Sum of an array") {
			t.Errorf("%s: the brief is not in the request", name)
		}
	}
	// The request type has no field a test could be put in.
	var fields []string
	rt := reflect.TypeOf(Request{})
	for i := 0; i < rt.NumField(); i++ {
		fields = append(fields, rt.Field(i).Name)
	}
	if got := strings.Join(fields, ","); got != "Brief,Pasted,System,Previous,Fix,Shorter,JavaLists" {
		t.Errorf("Request fields %s: a new field must be reviewed for test data", got)
	}
}
