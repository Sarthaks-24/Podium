package problems

// AI edit guard (v2.1.0 stage A): keys a reply leaves out are restored from
// the snapshot, a mode change and the other policy refusals have their own
// error type, and every reply shape ends in a draft or a clean refusal.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func yamlOf(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not yaml: %v\n%s", err, raw)
	}
	return m
}

// A reply that leaves a key out gets the snapshot's value back, key by key,
// and the draft names what was kept.
func TestRestoreOmittedKeys(t *testing.T) {
	s, p := editTestBank(t, "largest-gap")
	snap, _ := s.EditSnapshot(context.Background(), p.ID)
	snapYAML := string(snap.Files["problem.yaml"])
	for _, c := range []struct{ key, kept string }{
		{"generator", "the test generator"}, {"brute", "the brute force"}, {"validator", "the input validator"},
		{"checker", "the checker"}, {"samples", "the samples"}, {"time_limit_ms", "the limits"},
		{"memory_limit_mb", "the limits"}, {"title", "other settings"}, {"subtasks", "other settings"},
	} {
		reply, kept := restoreOmitted(snap.Files["problem.yaml"], []byte(dropKey(snapYAML, c.key)), nil)
		got, want := yamlOf(t, reply), yamlOf(t, snap.Files["problem.yaml"])
		if fmt.Sprint(got[c.key]) != fmt.Sprint(want[c.key]) || len(kept) != 1 || kept[0] != c.kept {
			t.Errorf("%s: restored %v want %v, kept %v", c.key, got[c.key], want[c.key], kept)
		}
	}
}

func TestRestoreOmittedGeneratorArgsOfEachSubtask(t *testing.T) {
	snapYAML := []byte("title: T\nsubtasks:\n  - points: 40\n    tests: 3-5\n    generator_args: [n=1]\n  - points: 60\n    tests: 6-8\n    generator_args: [n=2]\n")
	same := []byte("title: T\nsubtasks:\n  - points: 40\n    tests: 3-5\n  - points: 60\n    tests: 6-8\n    generator_args: [n=9]\n")
	out, kept := restoreOmitted(snapYAML, same, nil)
	subs := yamlOf(t, out)["subtasks"].([]any)
	if fmt.Sprint(subs[0].(map[string]any)["generator_args"]) != "[n=1]" || fmt.Sprint(subs[1].(map[string]any)["generator_args"]) != "[n=9]" ||
		len(kept) != 1 || kept[0] != "the generator settings of each subtask" {
		t.Errorf("subtasks %v kept %v", subs, kept)
	}
	// A different number of subtasks is a deliberate change: nothing is guessed.
	one := []byte("title: T\nsubtasks:\n  - points: 100\n    tests: 3-8\n")
	if out, kept := restoreOmitted(snapYAML, one, nil); string(out) != string(one) || len(kept) != 0 {
		t.Errorf("restored into a changed subtask list: %s %v", out, kept)
	}
}

// A reply that sends the file a key names, or tests.yaml for the generator
// keys, is judged as sent.
func TestRestoreOmittedSkipsKeysTheReplyDealtWith(t *testing.T) {
	snapYAML := []byte("title: T\ngenerator: gen.py\nbrute: brute.py\nsubtasks:\n  - points: 1\n    tests: 1\n    generator_args: [n=1]\n")
	reply := []byte("title: T\nsubtasks:\n  - points: 1\n    tests: 1\n")
	for _, c := range []struct {
		name string
		sent map[string][]byte
		kept []string
	}{
		{"nothing sent", nil, []string{"the test generator", "the generator settings of each subtask", "the brute force"}},
		{"tests.yaml", map[string][]byte{"tests.yaml": nil}, []string{"the brute force"}},
		{"generator file", map[string][]byte{"gen.py": nil}, []string{"the generator settings of each subtask", "the brute force"}},
		{"brute file", map[string][]byte{"brute.py": nil}, []string{"the test generator", "the generator settings of each subtask"}},
	} {
		_, kept := restoreOmitted(snapYAML, reply, c.sent)
		if fmt.Sprint(kept) != fmt.Sprint(c.kept) {
			t.Errorf("%s: kept %v, want %v", c.name, kept, c.kept)
		}
	}
}

func TestRestoreOmittedLeavesUnreadableYAMLToTheParser(t *testing.T) {
	snapYAML := []byte("title: T\ngenerator: gen.py\n")
	for _, bad := range []string{"", "title: [", "- a\n- b\n", "just text"} {
		if out, kept := restoreOmitted(snapYAML, []byte(bad), nil); string(out) != bad || len(kept) != 0 {
			t.Errorf("%q: %q %v", bad, out, kept)
		}
	}
}

func TestMergeEditKeepsGeneratorAndNamesWhatWasKept(t *testing.T) {
	s, p := editTestBank(t, "largest-gap")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	reply := strings.Replace(dropKey(string(snap.Files["problem.yaml"]), "generator"), "title: Largest gap", "title: Largest gap between neighbours", 1)
	merged, kept, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(reply)})
	if err != nil {
		t.Fatal(err)
	}
	m := yamlOf(t, merged["problem.yaml"])
	if m["generator"] != "generator.py" || m["title"] != "Largest gap between neighbours" || fmt.Sprint(kept) != "[the test generator]" {
		t.Errorf("merged %v kept %v", m, kept)
	}
	if string(merged["tests.yaml"]) != string(snap.Files["tests.yaml"]) {
		t.Error("the plan changed")
	}
}

// A reply that only left keys out changes nothing once they are back.
func TestMergeEditOfOnlyDroppedKeysIsNoChange(t *testing.T) {
	s, p := editTestBank(t, "largest-gap")
	snap, _ := s.EditSnapshot(context.Background(), p.ID)
	_, _, err := s.MergeEdit(context.Background(), snap, map[string][]byte{"problem.yaml": []byte(dropKey(string(snap.Files["problem.yaml"]), "generator"))})
	var pe *EditPolicyError
	if !errors.As(err, &pe) || !strings.Contains(pe.Problems[0], "did not change anything") {
		t.Errorf("%v", err)
	}
}

// For any set of top-level keys dropped from a valid problem.yaml, the merge
// has the snapshot's settings for those keys.
func TestMergeEditDroppedKeysProperty(t *testing.T) {
	s, p := editTestBank(t, "largest-gap")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	snapKeys := yamlOf(t, snap.Files["problem.yaml"])
	keys := slices.Sorted(maps.Keys(snapKeys))
	rng := rand.New(rand.NewPCG(1, 2))
	var sets [][]string
	for _, k := range keys {
		sets = append(sets, []string{k})
	}
	for i := 0; i < 60; i++ {
		var set []string
		for _, k := range keys {
			if rng.IntN(3) == 0 {
				set = append(set, k)
			}
		}
		sets = append(sets, set)
	}
	for _, set := range sets {
		text := string(snap.Files["problem.yaml"])
		for _, k := range set {
			text = dropKey(text, k)
		}
		merged, _, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(text), "statement.md": []byte("A new statement.\n")})
		if err != nil {
			t.Errorf("dropped %v: %v", set, err)
			continue
		}
		got := yamlOf(t, merged["problem.yaml"])
		for _, k := range set {
			if fmt.Sprint(got[k]) != fmt.Sprint(snapKeys[k]) {
				t.Errorf("dropped %v: %s is %v, want %v", set, k, got[k], snapKeys[k])
			}
		}
	}
}

func TestMergeEditChangingTheModeIsAPolicyRefusal(t *testing.T) {
	s, p := editTestBank(t, "array-sum")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	if snap.Mode != ModeFunction {
		t.Fatalf("setup: mode %q", snap.Mode)
	}
	// Leaving mode out is not a change.
	_, kept, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(dropKey(string(snap.Files["problem.yaml"]), "mode")), "statement.md": []byte("New text.\n")})
	if err != nil || len(kept) != 1 {
		t.Errorf("dropping mode: %v %v", err, kept)
	}
	// Writing a different one is.
	yamlText := strings.Replace(string(snap.Files["problem.yaml"]), "mode: function", "mode: whole", 1)
	_, _, err = s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(yamlText)})
	var pe *EditPolicyError
	if !errors.As(err, &pe) || !pe.ModeChange {
		t.Errorf("changing the mode: %v", err)
	}
	// And the other way round.
	ws, wp := editTestBank(t, "a-plus-b")
	wsnap, _ := ws.EditSnapshot(ctx, wp.ID)
	_, _, err = ws.MergeEdit(ctx, wsnap, map[string][]byte{"problem.yaml": []byte("mode: function\n" + string(wsnap.Files["problem.yaml"]))})
	if !errors.As(err, &pe) || !pe.ModeChange {
		t.Errorf("whole to function: %v", err)
	}
}

// Policy refusals have their own type; parser refusals stay *PackError.
func TestMergeEditRefusalKinds(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	yamlText := string(snap.Files["problem.yaml"])
	many := map[string][]byte{}
	for i := 0; i <= MaxEditFiles; i++ {
		many[fmt.Sprintf("f%d.py", i)] = []byte("x")
	}
	for _, c := range []struct {
		name   string
		reply  map[string][]byte
		policy bool
	}{
		{"test data", map[string][]byte{"tests/01.in": []byte("1\n")}, true},
		{"not a pack file", map[string][]byte{"notes.txt": []byte("x")}, true},
		{"slug", map[string][]byte{"problem.yaml": []byte(strings.Replace(yamlText, "slug: a-plus-b", "slug: other", 1))}, true},
		{"41 files", many, true},
		{"nothing changed", map[string][]byte{"statement.md": snap.Files["statement.md"]}, true},
		{"bad yaml", map[string][]byte{"problem.yaml": []byte("title: [")}, false},
		{"empty problem.yaml", map[string][]byte{"problem.yaml": {}}, false},
		{"unknown key", map[string][]byte{"problem.yaml": []byte(yamlText + "colour: red\n")}, false},
	} {
		_, _, err := s.MergeEdit(ctx, snap, c.reply)
		var pol *EditPolicyError
		var pack *PackError
		if c.policy != errors.As(err, &pol) || c.policy == errors.As(err, &pack) {
			t.Errorf("%s: %T %v", c.name, err, err)
		}
	}
}

// A custom checker's problem.yaml without `checker` keeps the checker.
func TestMergeEditKeepsACustomChecker(t *testing.T) {
	s, p := editTestBank(t, "pair-with-sum")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	merged, kept, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(dropKey(string(snap.Files["problem.yaml"]), "checker")), "statement.md": []byte("New.\n")})
	if err != nil || yamlOf(t, merged["problem.yaml"])["checker"] != "custom" || fmt.Sprint(kept) != "[the checker]" {
		t.Fatalf("%v %v", err, kept)
	}
	pk, err := s.parseMerged(ctx, snap, merged)
	if err != nil || pk.Problem.Checker != CheckerCustom || pk.Problem.CheckerSource == "" {
		t.Errorf("parsed checker: %v", err)
	}
}

// Editing a function problem's title keeps its files and its offered languages.
func TestMergeEditOfAFunctionProblemKeepsItsFiles(t *testing.T) {
	s, p := editTestBank(t, "array-sum")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	text := strings.Replace(string(snap.Files["problem.yaml"]), "title: ", "title: New ", 1)
	merged, _, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	for name := range snap.Files {
		if name != "problem.yaml" && string(merged[name]) != string(snap.Files[name]) {
			t.Errorf("%s changed", name)
		}
	}
}

// A function problem's reply that leaves out the locked code keys gets them back.
func TestMergeEditOfAFunctionProblemWithoutLockedCode(t *testing.T) {
	s, p := editTestBank(t, "array-sum")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	text := dropKey(dropKey(string(snap.Files["problem.yaml"]), "locked_top"), "locked_bottom")
	merged, kept, err := s.MergeEdit(ctx, snap, map[string][]byte{"problem.yaml": []byte(text), "statement.md": []byte("New.\n")})
	if err != nil {
		t.Fatal(err)
	}
	m := yamlOf(t, merged["problem.yaml"])
	if m["locked_top"] == nil || m["locked_bottom"] == nil || fmt.Sprint(kept) != "[the locked code]" {
		t.Errorf("merged %v kept %v", m, kept)
	}
}
