package httpapi

// Deleting a problem over HTTP (v2.0.2 stage A): who may, the refusals and
// their exact copy, what still reads afterwards (boards, submissions, the
// pinned statement), and that every write route answers 409 deleted.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	copyUsedByAdmin   = "Only the site owner can delete a problem that has been used. Archive it instead."
	copyDeletedSubmit = "This problem was deleted. Your earlier submissions are still listed."
	copyDeletedWrite  = "This problem was deleted and cannot be changed."
)

// errIs asserts the status, the error code and the exact message.
func errIs(t *testing.T, what string, code int, out map[string]any, wantStatus int, wantCode, wantMessage string) {
	t.Helper()
	e, _ := out["error"].(map[string]any)
	if code != wantStatus || e["code"] != wantCode || (wantMessage != "" && e["message"] != wantMessage) {
		t.Errorf("%s: %d %v, want %d %s %q", what, code, out, wantStatus, wantCode, wantMessage)
	}
}

// deleteSite is a site with an owner, an admin, an organiser and a student.
func deleteSite(t *testing.T) (s *site, owner, admin, organiser, student *browser) {
	t.Helper()
	s = newSite(t)
	owner = s.signedIn("owner@college.edu", "Owner")
	for email, role := range map[string]string{"admin@college.edu": "admin", "organiser@college.edu": "organiser"} {
		if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": email, "role": role}, s.srv.URL); code >= 300 {
			t.Fatalf("make %s: %d %v", role, code, out)
		}
	}
	return s, owner, s.signedIn("admin@college.edu", "Admin"), s.signedIn("organiser@college.edu", "Organiser"), s.signedIn("asha@college.edu", "Asha")
}

func TestDeleteRoutesRolesAndBadInput(t *testing.T) {
	s, owner, admin, organiser, student := deleteSite(t)
	o := s.srv.URL
	id := s.importProblem(owner, "unused-p")
	del := fmt.Sprintf("/api/admin/problems/%d/delete", id)
	info := fmt.Sprintf("/api/admin/problems/%d/delete-info", id)

	for name, b := range map[string]*browser{"organiser": organiser, "student": student} {
		for _, c := range []struct{ method, path string }{{"POST", del}, {"GET", info}, {"GET", "/api/admin/problems/deleted"}} {
			code, out := b.do(c.method, c.path, nil, o)
			// Students are refused by the organiser-level admin prefix too.
			if code != 403 {
				t.Errorf("%s %s %s: %d %v", name, c.method, c.path, code, out)
			}
		}
	}
	if code, out := s.browser().do("POST", del, nil, o); code != 401 {
		t.Errorf("signed out: %d %v", code, out)
	}
	if got, _ := organiser.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, o); got != 200 {
		t.Errorf("the refused delete changed the problem: %d", got)
	}
	for _, bad := range []string{"abc", "0", "99999"} {
		code, out := admin.do("POST", "/api/admin/problems/"+bad+"/delete", nil, o)
		errIs(t, "bad id "+bad, code, out, 404, "not_found", "There is no such problem.")
	}
	if code, out := admin.do("POST", del, nil, o); code != 204 {
		t.Fatalf("admin deletes an unused problem: %d %v", code, out)
	}
	code, out := admin.do("POST", del, nil, o)
	errIs(t, "second delete", code, out, 409, "deleted", copyDeletedWrite)
}

// A used problem: an admin is refused, the owner is refused while a contest
// holding it runs (naming it) and allowed after it ended. Boards, students'
// submissions and the pinned statement still read, the practice workspace
// closes with the deleted copy, and the contest keeps the problem.
func TestDeleteAUsedProblemKeepsEndedContestsReadable(t *testing.T) {
	s, owner, admin, organiser, asha := deleteSite(t)
	o := s.srv.URL
	pa := s.importProblem(owner, "used-p")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	cid := s.createContest(owner, "wk", start, map[string]int64{"A": pa}, nil)
	s.at(start, 1)
	asha.do("POST", "/api/contests/wk/start", map[string]any{"accept_honour": true}, o)
	code, sub := s.submit(asha, "wk", "A", "correct")
	if code != 202 {
		t.Fatalf("submit: %d %v", code, sub)
	}
	subID := int64(sub["id"].(float64))
	del := fmt.Sprintf("/api/admin/problems/%d/delete", pa)

	code, out := admin.do("POST", del, nil, o)
	errIs(t, "admin on a used problem", code, out, 403, "not_allowed", copyUsedByAdmin)
	code, out = owner.do("POST", del, nil, o)
	errIs(t, "owner while the contest runs", code, out, 409, "in_unended_contest",
		"This problem is in contests that have not ended: wk. Remove it from them or wait until they end.")
	if code, info := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d/delete-info", pa), nil, o); code != 200 || info["used"] != true ||
		len(info["unended"].([]any)) != 1 || len(info["practice"].([]any)) != 0 {
		t.Errorf("delete-info while running: %d %v", code, info)
	}

	s.at(start, 121)
	code, info := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d/delete-info", pa), nil, o)
	practice, _ := info["practice"].([]any)
	if code != 200 || len(info["unended"].([]any)) != 0 || len(practice) != 1 || practice[0].(map[string]any)["slug"] != "wk" {
		t.Errorf("delete-info after the end: %d %v", code, info)
	}
	_, before := owner.do("GET", "/api/contests/wk/standings", nil, "")
	_, wsBefore := asha.do("GET", "/api/contests/wk/problems/A", nil, "")
	if wsBefore["can_submit"] != true {
		t.Fatalf("practice workspace before the delete: %v", wsBefore["can_submit"])
	}

	if code, out := owner.do("POST", del, nil, o); code != 204 {
		t.Fatalf("owner after the end: %d %v", code, out)
	}

	// The board is unchanged.
	_, after := owner.do("GET", "/api/contests/wk/standings", nil, "")
	if fmt.Sprint(before["rows"]) != fmt.Sprint(after["rows"]) {
		t.Errorf("board changed:\n%v\n%v", before["rows"], after["rows"])
	}
	// The student still sees the problem with its title and the flag.
	_, detail := asha.do("GET", "/api/contests/wk", nil, "")
	pj := detail["problems"].([]any)[0].(map[string]any)
	if pj["deleted"] != true || pj["title"] != "A plus B (used-p)" {
		t.Errorf("student contest problem: %v", pj)
	}
	// The practice workspace is closed with the deleted copy; the pinned
	// statement stays readable.
	code, ws := asha.do("GET", "/api/contests/wk/problems/A", nil, "")
	if code != 200 || ws["can_submit"] != false || ws["refusal"] != copyDeletedSubmit || ws["statement_md"] == "" || ws["title"] != "A plus B (used-p)" {
		t.Errorf("workspace of a deleted problem: %d can_submit=%v refusal=%v statement=%v", code, ws["can_submit"], ws["refusal"], ws["statement_md"] != "")
	}
	for _, step := range []string{"submit", "run"} {
		code, out := asha.do("POST", "/api/contests/wk/problems/A/"+step, map[string]any{"language": "cpp17", "source": "correct", "client_key": "k-" + step}, o)
		errIs(t, "student "+step, code, out, 409, "deleted", copyDeletedSubmit)
	}
	code, out = owner.do("POST", "/api/contests/wk/problems/A/submit", map[string]any{"language": "cpp17", "source": "correct"}, o)
	errIs(t, "organiser submit", code, out, 409, "deleted", copyDeletedSubmit)
	// An old submission still shows its code.
	if code, got := asha.do("GET", fmt.Sprintf("/api/submissions/%d", subID), nil, ""); code != 200 || got["source"] != "correct" {
		t.Errorf("old submission: %d %v", code, got)
	}
	// Rejudge answers the deleted copy.
	code, out = organiser.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", cid), map[string]any{"problem_label": "A"}, o)
	if code != 404 { // the organiser does not run this contest
		t.Errorf("organiser rejudge: %d %v", code, out)
	}
	code, out = owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", cid), map[string]any{"problem_label": "A"}, o)
	errIs(t, "rejudge", code, out, 409, "deleted", copyDeletedSubmit)

	// SetProblems still refuses to drop it from the shown contest; the
	// board is unchanged.
	code, out = owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), []map[string]any{}, o)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "stays in this contest") {
		t.Errorf("drop from a shown contest: %d %v", code, out)
	}
	_, still := owner.do("GET", "/api/contests/wk/standings", nil, "")
	if fmt.Sprint(before["rows"]) != fmt.Sprint(still["rows"]) {
		t.Errorf("board changed by the refused drop")
	}
	// The admin view says which problem was deleted.
	_, adminView := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", cid), nil, "")
	if ap := adminView["problems"].([]any)[0].(map[string]any); ap["deleted"] != true || ap["ready"] != false || ap["update_available"] != false {
		t.Errorf("admin contest problem: %v", ap)
	}
	// Adding the deleted problem to another contest is refused.
	other := s.createContest(owner, "later", start.Add(48*time.Hour), map[string]int64{"A": s.importProblem(owner, "fresh-p")}, nil)
	code, out = owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", other), []map[string]any{{"problem_id": pa, "label": "A"}}, o)
	if code != 400 || fieldErr(out, "problems") != "The problem for label A was deleted. Remove it from the contest." {
		t.Errorf("add a deleted problem: %d %v", code, out)
	}
}

// Every write route answers 409 deleted for a tombstone, and the lists skip it.
func TestDeletedProblemRefusesEveryWriteRoute(t *testing.T) {
	s, owner, admin, organiser, _ := deleteSite(t)
	o := s.srv.URL
	id := s.importProblem(owner, "gone-p")
	base := fmt.Sprintf("/api/admin/problems/%d", id)
	if code, out := admin.do("POST", base+"/delete", nil, o); code != 204 {
		t.Fatalf("delete: %d %v", code, out)
	}
	pack := packZip(t, "gone-p-deleted-"+fmt.Sprint(id), nil, false)
	tests := packZip(t, "x", nil, true)
	for name, c := range map[string]struct {
		method, path string
		body         any
	}{
		"open":        {"GET", base, nil},
		"edit":        {"PUT", base, map[string]any{"title": "x"}},
		"restore":     {"POST", base + "/restore", map[string]any{}},
		"export":      {"GET", base + "/export", nil},
		"validate":    {"POST", base + "/validate", nil},
		"generate":    {"POST", base + "/generate", map[string]any{}},
		"generator":   {"POST", base + "/generate-inputs", map[string]any{}},
		"tests":       {"PUT", base + "/tests", tests},
		"import over": {"POST", "/api/admin/problems/import?on_conflict=replace", pack},
	} {
		var code int
		var out map[string]any
		switch body := c.body.(type) {
		case []byte:
			code, out = organiser.raw(c.method, c.path, "application/zip", body, o)
		default:
			code, out = organiser.do(c.method, c.path, c.body, o)
		}
		errIs(t, name, code, out, 409, "deleted", copyDeletedWrite)
	}
	// The organisers' workspace outside a contest has nothing to serve.
	ws := fmt.Sprintf("/api/workspace/%d", id)
	code, out := organiser.do("GET", ws, nil, "")
	errIs(t, "bank workspace", code, out, 409, "deleted", copyDeletedSubmit)
	code, out = organiser.do("POST", ws+"/submit", map[string]any{"language": "cpp17", "source": "x"}, o)
	errIs(t, "bank submit", code, out, 409, "deleted", copyDeletedSubmit)
	_, list := organiser.do("GET", "/api/admin/problems?archived=1", nil, "")
	if n := len(list["problems"].([]any)); n != 0 {
		t.Errorf("the bank lists %d problems", n)
	}
	code, deleted := admin.do("GET", "/api/admin/problems/deleted", nil, "")
	rows, _ := deleted["problems"].([]any)
	if code != 200 || len(rows) != 1 {
		t.Fatalf("deleted list: %d %v", code, deleted)
	}
	row := rows[0].(map[string]any)
	if row["slug"] != "gone-p" || row["deleted_by"] != "admin@college.edu" || row["used"] != false || row["deleted_at"] == nil {
		t.Errorf("deleted row: %v", row)
	}
	// An old client that archives with DELETE still can: the route is unchanged.
	other := s.importProblem(owner, "kept-p")
	if code, out := organiser.do("DELETE", fmt.Sprintf("/api/admin/problems/%d", other), nil, o); code != 204 {
		t.Errorf("archive: %d %v", code, out)
	}
}

// The delete is in the site audit with the person and the original slug and
// no problem text.
func TestDeleteWritesOneAuditRowWithoutText(t *testing.T) {
	s, owner, admin, _, _ := deleteSite(t)
	o := s.srv.URL
	id := s.importProblem(owner, "audit-p")
	if code, out := admin.do("POST", fmt.Sprintf("/api/admin/problems/%d/delete", id), nil, o); code != 204 {
		t.Fatalf("delete: %d %v", code, out)
	}
	code, out := admin.do("GET", "/api/admin/site-audit?action=problem_deleted", nil, "")
	entries, _ := out["entries"].([]any)
	if code != 200 || len(entries) != 1 {
		t.Fatalf("audit: %d %v", code, out)
	}
	e := entries[0].(map[string]any)
	actor := e["actor"].(map[string]any)
	d, _ := e["details"].(map[string]any)
	if actor["email"] != "admin@college.edu" || d["slug"] != "audit-p" || d["problem_id"] != float64(id) {
		t.Errorf("entry %v", e)
	}
	if raw := fmt.Sprint(e); strings.Contains(raw, "Describe") || strings.Contains(raw, "statement") {
		t.Errorf("the audit row holds problem text: %v", raw)
	}
}
