package httpapi

import (
	"strconv"
	"testing"
)

// fnFields are the function problem fields of an editor save: C++ only.
func fnFields() map[string]any {
	return map[string]any{
		"mode":          "function",
		"locked_top":    map[string]string{"cpp17": "#include <bits/stdc++.h>\nusing namespace std;\n"},
		"locked_bottom": map[string]string{"cpp17": "int main() { long long n; cin >> n; cout << twice(n) << endl; }\n"},
		"starters":      map[string]string{"cpp17": "long long twice(long long n) {\n    return 0;\n}\n"},
		"reference":     map[string]string{"language": "cpp17", "source": "long long twice(long long n) { return 2 * n; }\n"},
	}
}

// editBody is the problem as GET returns it, as an editor save, with the
// given fields on top.
func editBody(p map[string]any, extra map[string]any) map[string]any {
	body := map[string]any{}
	for _, k := range []string{"title", "slug", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
		"checker_source", "reference", "brute", "validator", "tags", "difficulty", "samples", "subtasks"} {
		body[k] = p[k]
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// The editor saves a function problem's mode and locked parts; a save
// that leaves them out (an older editor) keeps them; a student cannot
// save; a bad part is a field error; and once a student has a draft the
// mode is fixed with 409 mode_locked and PLAN 3.8's words.
func TestFunctionProblemOverHTTP(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("org@college.edu", "organiser")
	code, p := org.do("POST", "/api/admin/problems", map[string]any{"title": "Twice"}, s.srv.URL)
	if code != 201 || p["mode"] != "whole" {
		t.Fatalf("create: %d mode %v", code, p["mode"])
	}
	id := strconv.Itoa(int(p["id"].(float64)))
	path := "/api/admin/problems/" + id

	code, got := org.do("PUT", path, editBody(p, fnFields()), s.srv.URL)
	if code != 200 || got["mode"] != "function" {
		t.Fatalf("save as function: %d %v", code, got)
	}
	if top, _ := got["locked_top"].(map[string]any); top["cpp17"] != "#include <bits/stdc++.h>\nusing namespace std;\n" {
		t.Errorf("locked_top %v", got["locked_top"])
	}
	if bottom, _ := got["locked_bottom"].(map[string]any); len(bottom) != 1 {
		t.Errorf("locked_bottom %v", got["locked_bottom"])
	}

	// Old client: no mode, no locked parts, no starters.
	code, kept := org.do("PUT", path, editBody(got, map[string]any{"statement_md": "Double it."}), s.srv.URL)
	if code != 200 || kept["mode"] != "function" || len(kept["locked_bottom"].(map[string]any)) != 1 {
		t.Errorf("old client save: %d mode %v bottom %v", code, kept["mode"], kept["locked_bottom"])
	}

	// Bad input: a language with a top and no bottom.
	bad := fnFields()
	bad["locked_top"] = map[string]string{"cpp17": "#include <cstdio>\n", "python3": "import sys\n"}
	code, body := org.do("PUT", path, editBody(got, bad), s.srv.URL)
	if fields, _ := body["error"].(map[string]any)["fields"].(map[string]any); code != 400 ||
		fields["locked_bottom"] != "Add a locked bottom for python3, or remove its locked top and starter." {
		t.Errorf("bad parts: %d %v", code, body)
	}

	// Refused role.
	student := s.testSignIn("stu@college.edu", "student")
	if code, body := student.do("PUT", path, editBody(got, fnFields()), s.srv.URL); code != 403 {
		t.Errorf("student save: %d %v", code, body)
	}

	// A draft fixes the mode.
	if _, err := s.db.Exec(`INSERT INTO drafts (user_id, problem_id, language, source, updated_at)
		SELECT id, ?, 'cpp17', 'x', 0 FROM users WHERE email = 'stu@college.edu'`, id); err != nil {
		t.Fatal(err)
	}
	code, body = org.do("PUT", path, editBody(got, map[string]any{"mode": "whole"}), s.srv.URL)
	e, _ := body["error"].(map[string]any)
	if code != 409 || e["code"] != "mode_locked" ||
		e["message"] != "This problem already has submissions or drafts, so its mode cannot change. Make a new problem instead." {
		t.Errorf("mode change after a draft: %d %v", code, body)
	}
}

// Removing a language a student has a draft in answers 409 mode_locked
// with the language named.
func TestUsedLanguageRemovalOverHTTP(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("org@college.edu", "organiser")
	_, p := org.do("POST", "/api/admin/problems", map[string]any{"title": "Twice"}, s.srv.URL)
	id := strconv.Itoa(int(p["id"].(float64)))
	path := "/api/admin/problems/" + id
	two := fnFields()
	two["locked_top"] = map[string]string{"cpp17": "#include <cstdio>\n", "python3": "import sys\n"}
	two["locked_bottom"] = map[string]string{"cpp17": "int main() {}\n", "python3": "print(twice(1))\n"}
	two["starters"] = map[string]string{"cpp17": "long long twice(long long n) {}\n", "python3": "def twice(n):\n    pass\n"}
	code, got := org.do("PUT", path, editBody(p, two), s.srv.URL)
	if code != 200 {
		t.Fatalf("save: %d %v", code, got)
	}
	s.testSignIn("stu@college.edu", "student")
	if _, err := s.db.Exec(`INSERT INTO drafts (user_id, problem_id, language, source, updated_at)
		SELECT id, ?, 'python3', 'x', 0 FROM users WHERE email = 'stu@college.edu'`, id); err != nil {
		t.Fatal(err)
	}
	code, body := org.do("PUT", path, editBody(got, fnFields()), s.srv.URL)
	e, _ := body["error"].(map[string]any)
	if code != 409 || e["code"] != "mode_locked" ||
		e["message"] != "This problem already has submissions in Python 3, so that language cannot be removed. Make a new problem instead." {
		t.Errorf("removing python3: %d %v", code, body)
	}
}
