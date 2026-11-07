package problems

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
)

// Locked parts of a small function problem in two languages.
var (
	fnTop    = map[string]string{"cpp17": "#include <bits/stdc++.h>\nusing namespace std;\n", "python3": "import sys\n"}
	fnBottom = map[string]string{"cpp17": "int main() { long long n; cin >> n; cout << twice(n) << endl; }\n",
		"python3": "print(twice(int(sys.stdin.readline())))\n"}
	fnStubs = map[string]string{"cpp17": "long long twice(long long n) {\n    return 0;\n}\n", "python3": "def twice(n):\n    return 0\n"}
)

// functionEdit turns p into a function problem with a reference that is a
// bare function.
func functionEdit(p *Problem) Edit {
	e := editOf(p)
	mode := ModeFunction
	top, bottom, stubs := maps.Clone(fnTop), maps.Clone(fnBottom), maps.Clone(fnStubs)
	e.Mode, e.LockedTop, e.LockedBottom, e.Starters = &mode, &top, &bottom, &stubs
	e.Reference = Source{"cpp17", "long long twice(long long n) { return 2 * n; }\n"}
	e.Brute = &Source{"python3", "def twice(n):\n    return n + n\n"}
	return e
}

// A function problem saves its mode and locked parts, every revision records
// them, and an edit of the locked top alone makes a new revision (V202-32).
func TestFunctionProblemSavesAndMakesARevision(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Twice", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Update(ctx, p.ID, functionEdit(p))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(ctx, p.ID)
	if again.Mode != ModeFunction || !maps.Equal(again.LockedTop, fnTop) || !maps.Equal(again.LockedBottom, fnBottom) ||
		!maps.Equal(again.Starters, fnStubs) {
		t.Fatalf("stored mode %q top %v bottom %v starters %v", again.Mode, again.LockedTop, again.LockedBottom, again.Starters)
	}
	if langs := again.Offered(); strings.Join(langs, ",") != "cpp17,python3" {
		t.Errorf("offered %v", langs)
	}
	rev, err := s.CurrentRevision(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Mode != ModeFunction || !maps.Equal(rev.LockedTop, fnTop) || !maps.Equal(rev.LockedBottom, fnBottom) {
		t.Errorf("revision mode %q top %v bottom %v", rev.Mode, rev.LockedTop, rev.LockedBottom)
	}
	before := revCount(t, s, p.ID)
	e := editOf(got)
	top := maps.Clone(fnTop)
	top["python3"] = "import sys\nimport math\n"
	e.LockedTop = &top
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	if n := revCount(t, s, p.ID); n != before+1 {
		t.Errorf("a top-only edit made %d revisions, want 1", n-before)
	}
	if rev, _ := s.CurrentRevision(ctx, p.ID); rev.LockedTop["python3"] != "import sys\nimport math\n" {
		t.Errorf("newest revision top %q", rev.LockedTop["python3"])
	}
	// An editor that leaves the new fields out keeps them.
	e = editOf(got)
	if kept, err := s.Update(ctx, p.ID, e); err != nil || kept.Mode != ModeFunction || kept.LockedBottom["cpp17"] == "" {
		t.Errorf("save without the new fields: mode %q bottom %v (%v)", kept.Mode, kept.LockedBottom, err)
	}
	assertRevisions(t, s)
}

// sameRevision tells revisions apart by mode and by each locked part, even
// when the content hashes match.
func TestSameRevisionComparesLockedParts(t *testing.T) {
	base := Revision{ContentHash: "h", StatementRecorded: true, Mode: ModeFunction,
		LockedTop: map[string]string{"cpp17": "a\n"}, LockedBottom: map[string]string{"cpp17": "b\n"}}
	for name, change := range map[string]func(r *Revision){
		"mode":   func(r *Revision) { r.Mode = ModeWhole },
		"top":    func(r *Revision) { r.LockedTop = map[string]string{"cpp17": "c\n"} },
		"bottom": func(r *Revision) { r.LockedBottom = map[string]string{"cpp17": "c\n"} },
	} {
		other := base
		other.LockedTop, other.LockedBottom = maps.Clone(base.LockedTop), maps.Clone(base.LockedBottom)
		change(&other)
		if sameRevision(base, other) {
			t.Errorf("%s: a change was not a new revision", name)
		}
	}
	// A whole revision read from before 0014 equals one with an empty mode.
	old := Revision{ContentHash: "h", StatementRecorded: true}
	now := Revision{ContentHash: "h", StatementRecorded: true, Mode: ModeWhole, LockedTop: map[string]string{}, LockedBottom: map[string]string{}}
	if !sameRevision(old, now) {
		t.Error("a whole revision differs from its old form")
	}
}

// The rules for a function problem: each offered language has a locked
// top, a locked bottom and a stub; the reference and brute force are in
// offered languages; each part is at most 4 KB. A whole problem drops any
// locked parts sent with it.
func TestFunctionProblemRules(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Rules", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		edit  func(e *Edit)
		field string
		want  string
	}{
		"no language": {func(e *Edit) {
			*e.LockedTop, *e.LockedBottom, *e.Starters = map[string]string{}, map[string]string{}, map[string]string{}
		}, "locked_bottom", "Add the locked code for at least one language."},
		"top without bottom": {func(e *Edit) { delete(*e.LockedBottom, "python3") }, "locked_bottom",
			"Add a locked bottom for python3, or remove its locked top and starter."},
		"bottom without top": {func(e *Edit) { delete(*e.LockedTop, "python3") }, "locked_top",
			"Add a locked top for python3. Common imports go there."},
		"blank top": {func(e *Edit) { (*e.LockedTop)["python3"] = "  \n" }, "locked_top",
			"Add a locked top for python3. Common imports go there."},
		"no stub": {func(e *Edit) { delete(*e.Starters, "python3") }, "starters",
			"Add a starter for python3: the function a student fills in."},
		"stub only": {func(e *Edit) { (*e.Starters)["java21"] = "class Solution {}\n" }, "starters",
			"There is a starter for java21 but no locked code. Add its locked top and bottom, or remove the starter."},
		"unknown language": {func(e *Edit) { (*e.LockedTop)["cobol"] = "x" }, "locked_top",
			`There is no language "cobol". Use cpp17, java21, python3 or node22.`},
		"long top": {func(e *Edit) { (*e.LockedTop)["cpp17"] = strings.Repeat("x", MaxStarter+1) }, "locked_top",
			"The cpp17 locked top is longer than 4 KB."},
		"long bottom": {func(e *Edit) { (*e.LockedBottom)["cpp17"] = strings.Repeat("x", MaxStarter+1) }, "locked_bottom",
			"The cpp17 locked bottom is longer than 4 KB."},
		"reference not offered": {func(e *Edit) { e.Reference = Source{"java21", "class Solution {}"} }, "reference",
			"Write the reference in a language this problem offers: C++17 or Python 3."},
		"brute not offered": {func(e *Edit) { e.Brute = &Source{"node22", "function twice(n) { return 2 * n }"} }, "brute",
			"Write the brute force in a language this problem offers: C++17 or Python 3."},
		"bad mode": {func(e *Edit) { m := "half"; e.Mode = &m }, "mode", "Choose whole program or function."},
	} {
		e := functionEdit(p)
		tc.edit(&e)
		var fields FieldErrors
		if _, err := s.Update(ctx, p.ID, e); !errors.As(err, &fields) || fields[tc.field] != tc.want {
			t.Errorf("%s: got %v, want %s: %q", name, err, tc.field, tc.want)
		}
	}
	e := editOf(p)
	whole := ModeWhole
	top, bottom := maps.Clone(fnTop), maps.Clone(fnBottom)
	e.Mode, e.LockedTop, e.LockedBottom = &whole, &top, &bottom
	got, err := s.Update(ctx, p.ID, e)
	if err != nil || got.Mode != ModeWhole || len(got.LockedTop) != 0 || len(got.LockedBottom) != 0 {
		t.Errorf("whole problem kept locked parts: %v %v (%v)", got.LockedTop, got.LockedBottom, err)
	}
}

// The mode is fixed once a student has a submission, run or draft on the
// problem (V202-31); the locked parts can still change.
func TestModeLockedOnceUsed(t *testing.T) {
	for _, use := range []string{
		`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 1, 'submit', 'cpp17', 'x', 'x', 0)`,
		`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 1, 'run', 'cpp17', '', 'x', 0)`,
		`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, 1, 'cpp17', 'x', 0)`,
	} {
		s := newGenBank(t)
		ctx := context.Background()
		p, err := s.Create(ctx, "Locked", "", 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(use, p.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Update(ctx, p.ID, functionEdit(p)); !errors.Is(err, ErrModeLocked) {
			t.Errorf("%s: whole to function: %v, want ErrModeLocked", use[:30], err)
		}
		if ErrModeLocked.Error() != "This problem already has submissions or drafts, so its mode cannot change. Make a new problem instead." {
			t.Errorf("copy %q", ErrModeLocked)
		}
		// A save that keeps the mode still works.
		if _, err := s.Update(ctx, p.ID, editOf(p)); err != nil {
			t.Errorf("%s: save keeping the mode: %v", use[:30], err)
		}
	}
	// Before any use, the mode changes both ways, and a used function
	// problem can still have its locked top edited.
	s := newGenBank(t)
	ctx := context.Background()
	p, _ := s.Create(ctx, "Free", "", 1, "")
	fn, err := s.Update(ctx, p.ID, functionEdit(p))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, 1, 'cpp17', 'x', 0)`, p.ID); err != nil {
		t.Fatal(err)
	}
	e := editOf(fn)
	top := maps.Clone(fnTop)
	top["cpp17"] += "#include <cmath>\n"
	e.LockedTop = &top
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Errorf("top edit on a used function problem: %v", err)
	}
}
