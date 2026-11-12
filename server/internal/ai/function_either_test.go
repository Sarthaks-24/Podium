package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// Stage F14: the "either" choice of the Java lists question.

func eitherAnswer(t *testing.T, edit func(m map[string]any)) string {
	t.Helper()
	b, _ := json.Marshal(FakeFunctionEither())
	var m map[string]any
	json.Unmarshal(b, &m)
	if edit != nil {
		edit(m)
	}
	b, _ = json.Marshal(m)
	return string(b)
}

func javaOf(m map[string]any) map[string]any {
	for _, l := range m["languages"].([]any) {
		if l := l.(map[string]any); l["language"] == "java21" {
			return l
		}
	}
	return nil
}

func TestEitherSchemaIsStrictAndAddsOneField(t *testing.T) {
	s := FunctionSchemaFor(JavaListsEither)
	assertStrictSchema(t, s)
	props := s["properties"].(map[string]any)
	if props["java_solution_arraylist"] == nil || props["languages"] == nil {
		t.Fatalf("properties %v", props)
	}
	// The other schemas are the function schema, key for key.
	base := FunctionSchema()
	for _, v := range []string{JavaListsArray, JavaListsArrayList, ""} {
		a, _ := json.Marshal(FunctionSchemaFor(v))
		b, _ := json.Marshal(base)
		if string(a) != string(b) {
			t.Errorf("schema for %q differs from the function schema", v)
		}
	}
	for k := range base["properties"].(map[string]any) {
		if props[k] == nil {
			t.Errorf("the either schema lacks %s", k)
		}
	}
	if len(props) != len(base["properties"].(map[string]any))+1 {
		t.Error("the either schema adds more than one property")
	}
}

func TestEitherDecodeIsStrict(t *testing.T) {
	good := eitherAnswer(t, nil)
	p, err := DecodeFunctionFor(good, JavaListsEither)
	if err != nil || p.JavaSolutionArrayList == nil || !strings.Contains(*p.JavaSolutionArrayList, "ArrayList<Integer>") {
		t.Fatalf("good answer: %+v %v", p, err)
	}
	for name, tc := range map[string]struct{ text, lists string }{
		"missing second solution":    {eitherAnswer(t, func(m map[string]any) { delete(m, "java_solution_arraylist") }), JavaListsEither},
		"unknown key":                {eitherAnswer(t, func(m map[string]any) { m["surprise"] = 1 }), JavaListsEither},
		"wrong type":                 {eitherAnswer(t, func(m map[string]any) { m["java_solution_arraylist"] = 4 }), JavaListsEither},
		"unknown key in a language":  {eitherAnswer(t, func(m map[string]any) { javaOf(m)["extra"] = "x" }), JavaListsEither},
		"the field in an array run":  {good, JavaListsArray},
		"the field in a list run":    {good, JavaListsArrayList},
		"the field in a default run": {good, ""},
	} {
		_, err := DecodeFunctionFor(tc.text, tc.lists)
		e, ok := err.(*Error)
		if !ok || e.Code != CodeInvalidAnswer || e.answer != tc.text || len(e.Reasons) == 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	// An array or ArrayList run decodes as before.
	plain := fnAnswer(FakeFunctionArraySum())
	for _, v := range []string{JavaListsArray, JavaListsArrayList} {
		if p, err := DecodeFunctionFor(plain, v); err != nil || p.JavaSolutionArrayList != nil {
			t.Errorf("%s: %+v %v", v, p, err)
		}
	}
	// An either run needs the field.
	if _, err := DecodeFunctionFor(plain, JavaListsEither); err == nil {
		t.Error("an either answer without java_solution_arraylist was accepted")
	}
}

func TestCheckFunctionForTwoForms(t *testing.T) {
	if r := CheckFunction(FakeFunctionEither()); len(r) > 0 {
		t.Fatalf("the canned pack is refused: %v", r)
	}
	d := ToDraftFunction(FakeFunctionEither())
	if errs := problems.CheckDraft(d); errs != nil {
		t.Fatalf("draft: %v", errs)
	}
	if c, err := problems.SplitStarter(d.Problem.Starters["java21"]); err != nil || len(c.Forms) != 2 {
		t.Fatalf("starting code: %+v %v", c, err)
	}
	sol := FakeFunctionEither().Solutions()
	if len(sol) != 4 || !strings.Contains(sol["java21"], "int[] a") || !strings.Contains(sol[problems.ProofJavaArrayListKey], "ArrayList<Integer> a") {
		t.Errorf("solutions %v", sol)
	}
	javaStarter := func(f func(l *FunctionLanguage)) *FunctionPack {
		p := FakeFunctionEither()
		for i := range p.Languages {
			if p.Languages[i].Language == "java21" {
				f(&p.Languages[i])
			}
		}
		return p
	}
	empty := ""
	one := "class Solution extends Base {\n    @Override\n    long solve(int[] a) { return 0; }\n}\n"
	for name, tc := range map[string]struct {
		pack *FunctionPack
		want string
	}{
		"no second solution": {func() *FunctionPack { p := FakeFunctionEither(); p.JavaSolutionArrayList = &empty; return p }(), "java_solution_arraylist is empty"},
		"one form only":      {javaStarter(func(l *FunctionLanguage) { l.Starter = one }), "both forms"},
		"one marker only":    {javaStarter(func(l *FunctionLanguage) { l.Starter = "// choice: int[]\n" + one }), "both forms"},
		"a near-miss marker": {javaStarter(func(l *FunctionLanguage) {
			l.Starter = strings.Replace(l.Starter, "// choice: int[]", "//choice: int[]", 1)
		}), "exactly as // choice: Name"},
		"a third form": {javaStarter(func(l *FunctionLanguage) { l.Starter += "// choice: long[]\n" + one }), "two choices at most"},
		"a pair that is not allowed": {javaStarter(func(l *FunctionLanguage) {
			l.Starter = strings.Replace(l.Starter, "// choice: ArrayList<Integer>", "// choice: ArrayList<Long>", 1)
		}), "one of each"},
		"an empty form": {javaStarter(func(l *FunctionLanguage) { l.Starter = "// choice: int[]\n// choice: ArrayList<Integer>\n" + one }), "under each choice line"},
		"a huge solution": {func() *FunctionPack {
			p := FakeFunctionEither()
			big := strings.Repeat("x", problems.MaxSource+1)
			p.JavaSolutionArrayList = &big
			return p
		}(), "java_solution_arraylist is longer than 64 KB"},
	} {
		got := strings.Join(CheckFunction(tc.pack), " | ")
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "java21") {
			t.Errorf("%s: reasons %q, want %q", name, got, tc.want)
		}
	}
	// A run that asked for one list type does not accept two forms.
	p := FakeFunctionEither()
	p.JavaSolutionArrayList = nil
	if got := strings.Join(CheckFunction(p), " | "); !strings.Contains(got, "one list type") {
		t.Errorf("two forms in a one-type run: %q", got)
	}
	// A broken or lone choice line in a one-type run is reported too, so the model gets its retry.
	for name, starter := range map[string]string{
		"a lone marker":      "// choice: int[]\nclass Solution extends Base {}\n",
		"a near-miss marker": "//choice: int[]\nclass Solution extends Base {}\n",
	} {
		p := FakeFunctionArraySum()
		for i := range p.Languages {
			if p.Languages[i].Language == "java21" {
				p.Languages[i].Starter = starter
			}
		}
		if got := strings.Join(CheckFunction(p), " | "); !strings.Contains(got, "java21: starter") {
			t.Errorf("%s in a one-type run: %q", name, got)
		}
	}
}

func TestEitherPromptGivesTheProvedPattern(t *testing.T) {
	either := BuildFunctionSystemPrompt(DefaultRules, "", JavaListsEither)
	for _, want := range []string{"class Missing extends RuntimeException", "class Base", "// choice: int[]", "// choice: ArrayList<Integer>",
		"@Override", "System.err", "java_solution_arraylist", "extends Base"} {
		if !strings.Contains(either, want) {
			t.Errorf("the either prompt lacks %q", want)
		}
	}
	array := BuildFunctionSystemPrompt(DefaultRules, "", JavaListsArray)
	list := BuildFunctionSystemPrompt(DefaultRules, "", JavaListsArrayList)
	for name, s := range map[string]string{"array": array, "arraylist": list} {
		for _, bad := range []string{"Base", "// choice", "java_solution_arraylist", "Missing"} {
			if strings.Contains(s, bad) {
				t.Errorf("the %s prompt mentions %q", name, bad)
			}
		}
	}
	if (Request{JavaLists: JavaListsEither}).functionSystem() != BuildFunctionSystemPrompt(fileRules(), "", JavaListsEither) {
		t.Error("an either request does not use the either prompt")
	}
}

// Every provider sends the either schema for an either run and the plain
// function schema for the other two choices.
func TestProvidersSendTheEitherSchema(t *testing.T) {
	brief := &Brief{Topic: "Sum of an array", Difficulty: "easy"}
	for name, r := range sendFunctionRequests(t, Request{Brief: brief, JavaLists: JavaListsEither}) {
		if props, _ := r.schema["properties"].(map[string]any); props["java_solution_arraylist"] == nil {
			t.Errorf("%s: the either schema was not sent", name)
		}
		if !strings.Contains(r.system, "class Base") {
			t.Errorf("%s: the either prompt was not sent", name)
		}
	}
	for _, lists := range []string{JavaListsArray, JavaListsArrayList} {
		for name, r := range sendFunctionRequests(t, Request{Brief: brief, JavaLists: lists}) {
			if props, _ := r.schema["properties"].(map[string]any); props["java_solution_arraylist"] != nil || strings.Contains(r.raw, "java_solution_arraylist") {
				t.Errorf("%s %s: the either field leaked into the request", lists, name)
			}
		}
	}
}

func TestEitherGenerationAndRetry(t *testing.T) {
	for _, name := range []string{VariantEitherListWrong, VariantEitherFormBroken} {
		p := FakeFunctionEitherVariant(name)
		if p == nil || p.JavaSolutionArrayList == nil {
			t.Fatalf("variant %s: %+v", name, p)
		}
		if r := CheckFunction(p); len(r) > 0 {
			t.Errorf("variant %s is refused by the checks: %v", name, r)
		}
	}
	if FakeFunctionEitherVariant("nope") != nil {
		t.Error("an unknown variant gave a pack")
	}
	bad := func() (*FunctionPack, error) {
		p := FakeFunctionEither()
		empty := ""
		p.JavaSolutionArrayList = &empty
		return p, nil
	}
	good := func() (*FunctionPack, error) { return FakeFunctionEither(), nil }
	s := &stub{functionAnswers: []func() (*FunctionPack, error){bad, good}}
	pack, err := GenerateFunction(context.Background(), s, Request{Pasted: "sum", JavaLists: JavaListsEither})
	if err != nil || pack.JavaSolutionArrayList == nil || len(s.reqs) != 2 || s.reqs[1].JavaLists != JavaListsEither || !strings.Contains(s.reqs[1].Fix[0], "java_solution_arraylist is empty") {
		t.Fatalf("%+v %v %+v", pack, err, s.reqs)
	}
}

// A fix writes one form with arrays: marker lines in its starting code are
// refused, so a fix never saves a choice that was not proved.
func TestCheckFixAnswerRefusesChoiceLines(t *testing.T) {
	a := &FixAnswer{LockedTop: "t", Starter: javaEitherStarter, LockedBottom: "b", Solution: "s"}
	if got := strings.Join(CheckFixAnswer(a), " | "); !strings.Contains(got, "starter: write one form") {
		t.Errorf("reasons %q", got)
	}
	a.Starter = "class Solution extends Base {}\n"
	if got := CheckFixAnswer(a); len(got) > 0 {
		t.Errorf("a plain starter was refused: %v", got)
	}
}

func TestFailureTextNamesTheForm(t *testing.T) {
	r := &problems.ProofResult{Outcome: problems.ProofFailed, Reason: "wrong_answer", Variant: "ArrayList<Integer>", Failed: 3, Total: 12, Verdicts: map[string]int{"WA": 3}}
	got := FailureText(r)
	if !strings.Contains(got, "This was the ArrayList<Integer> form.") || !strings.Contains(got, "3 of 12 tests failed") {
		t.Errorf("text %q", got)
	}
	r.Variant = ""
	if strings.Contains(FailureText(r), "form") {
		t.Error("a language with one form names a form")
	}
}
