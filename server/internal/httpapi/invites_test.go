package httpapi

// Invite-only contests over HTTP (v4.01 stage I): the gate, the picker's
// candidates, adding by id and by paste, removal timing and the audience
// change rule.

import (
	"fmt"
	"testing"
	"time"
)

func TestInviteOnlyContest(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "lead@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("grant organiser: %d %v", code, out)
	}
	// Listed but never signed in: invitable all the same.
	if code, out := owner.do("POST", "/api/admin/people/import", map[string]any{"text": "p.rao@gmail.com\nnever@college.edu", "dry_run": false}, s.srv.URL); code >= 300 {
		t.Fatalf("import: %d %v", code, out)
	}
	lead := s.signedIn("lead@college.edu", "Lead")
	asha := s.signedIn("asha@college.edu", "Asha")
	ravi := s.signedIn("ravi@college.edu", "Ravi")
	problem := s.importProblem(owner, "invite-problem")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "invite-only", start, map[string]int64{"A": problem}, map[string]any{"audience": "invited"})
	if code, out := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/organisers", id), map[string]any{"email": "lead@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("add lead: %d %v", code, out)
	}
	base := fmt.Sprintf("/api/admin/contests/%d/invites", id)
	page := "/api/contests/invite-only"

	if code, _ := asha.do("GET", page, nil, ""); code != 404 {
		t.Errorf("uninvited student page: %d, want 404", code)
	}
	if _, list := asha.do("GET", "/api/contests", nil, ""); len(list["upcoming"].([]any)) != 0 {
		t.Errorf("uninvited student list: %v", list["upcoming"])
	}
	if code, _ := asha.do("GET", base+"/candidates", nil, ""); code != 404 && code != 403 {
		t.Errorf("student reads candidates: %d", code)
	}

	// Picker: staff ids are skipped.
	code, out := lead.do("POST", base, map[string]any{"user_ids": []int64{asha.userID(), owner.userID()}}, s.srv.URL)
	if code != 200 || out["added"] != float64(1) {
		t.Fatalf("invite by id: %d %v", code, out)
	}
	if code, _ := asha.do("GET", page, nil, ""); code != 200 {
		t.Errorf("invited student page: %d, want 200", code)
	}
	if code, _ := ravi.do("GET", page, nil, ""); code != 404 {
		t.Errorf("other student page: %d, want 404", code)
	}

	// Candidates: enabled students only, with the invited flag.
	_, cands := lead.do("GET", base+"/candidates", nil, "")
	byEmail := map[string]bool{}
	for _, c := range cands["candidates"].([]any) {
		m := c.(map[string]any)
		byEmail[m["email"].(string)] = m["invited"].(bool)
	}
	if invited, ok := byEmail["asha@college.edu"]; !ok || !invited {
		t.Errorf("candidates: asha %v %v", ok, invited)
	}
	for _, staff := range []string{"owner@college.edu", "lead@college.edu"} {
		if _, ok := byEmail[staff]; ok {
			t.Errorf("candidates include staff %s", staff)
		}
	}
	if _, ok := byEmail["never@college.edu"]; !ok {
		t.Error("candidates leave out a listed student who never signed in")
	}

	// Paste: a dry run writes nothing; the reasons are uniform; Gmail dots match.
	paste := map[string]any{"text": "Ravi@College.edu\nlead@college.edu\nnobody@college.edu\nasha@college.edu\nprao@gmail.com\n", "dry_run": true}
	code, preview := lead.do("POST", base+"/paste", paste, s.srv.URL)
	if code != 200 || fmt.Sprint(preview["added"]) != "[ravi@college.edu prao@gmail.com]" || fmt.Sprint(preview["already_invited"]) != "[asha@college.edu]" {
		t.Fatalf("paste preview: %d %v", code, preview)
	}
	for _, m := range preview["unmatched"].([]any) {
		if m.(map[string]any)["reason"] != "Not a student on the sign-in list" {
			t.Errorf("unmatched reason: %v", m)
		}
	}
	if n := len(preview["unmatched"].([]any)); n != 2 {
		t.Errorf("unmatched lines: %d, want 2 (staff and unknown)", n)
	}
	if code, _ := ravi.do("GET", page, nil, ""); code != 404 {
		t.Errorf("a dry run invited ravi: %d", code)
	}
	paste["dry_run"] = false
	if code, out := lead.do("POST", base+"/paste", paste, s.srv.URL); code != 200 {
		t.Fatalf("paste: %d %v", code, out)
	}
	if code, _ := ravi.do("GET", page, nil, ""); code != 200 {
		t.Errorf("pasted student page: %d, want 200", code)
	}

	// Removal before the start is allowed.
	if code, _ := lead.do("DELETE", fmt.Sprintf("%s/%d", base, ravi.userID()), nil, s.srv.URL); code != 204 {
		t.Errorf("remove before start: %d", code)
	}
	if code, _ := ravi.do("GET", page, nil, ""); code != 404 {
		t.Errorf("removed student page: %d, want 404", code)
	}
	// A listed student who leaves the list loses the invite with the row.
	var neverID int64
	if err := s.db.QueryRow("SELECT id FROM users WHERE email = 'never@college.edu'").Scan(&neverID); err != nil {
		t.Fatal(err)
	}
	if code, out := lead.do("POST", base, map[string]any{"user_ids": []int64{neverID}}, s.srv.URL); code != 200 || out["added"] != float64(1) {
		t.Fatalf("invite never: %d %v", code, out)
	}
	if code, out := owner.do("POST", "/api/admin/people/remove", map[string]any{"email": "never@college.edu"}, s.srv.URL); code >= 300 {
		t.Fatalf("remove from list: %d %v", code, out)
	}
	var left int
	if err := s.db.QueryRow("SELECT count(*) FROM contest_invites WHERE user_id = ?", neverID).Scan(&left); err != nil || left != 0 {
		t.Errorf("invites after leaving the list: %d %v", left, err)
	}

	// While the contest runs, removal is refused with its reason, and
	// adding still works.
	s.at(start, 5)
	code, out = lead.do("DELETE", fmt.Sprintf("%s/%d", base, asha.userID()), nil, s.srv.URL)
	if code != 409 || fmt.Sprint(out["error"]) == "" {
		t.Errorf("remove while running: %d %v", code, out)
	}
	if code, out := lead.do("POST", base, map[string]any{"user_ids": []int64{ravi.userID()}}, s.srv.URL); code != 200 || out["added"] != float64(1) {
		t.Errorf("invite while running: %d %v", code, out)
	}
	var audits int
	if err := s.db.QueryRow(`SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action IN ('invites_added', 'invite_removed')`, id).Scan(&audits); err != nil || audits != 5 {
		t.Errorf("invite audit rows: %d %v, want 5", audits, err)
	}

	// A student promoted to organiser is judged as staff: view, not the invite.
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "ravi@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("promote ravi: %d %v", code, out)
	}
	if _, err := s.db.Exec("DELETE FROM contest_invites WHERE user_id = ?", ravi.userID()); err != nil {
		t.Fatal(err)
	}
	if code, p := ravi.do("GET", page, nil, ""); code != 200 || p["organiser"] != false {
		t.Errorf("promoted student page: %d %v, want 200 view", code, p["organiser"])
	}
}

// Opening a contest to everyone is always allowed; limiting it to invited
// students is refused once anyone has started.
func TestAudienceChangeRule(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	problem := s.importProblem(owner, "audience-problem")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "audience", start, map[string]int64{"A": problem}, nil)
	route := fmt.Sprintf("/api/admin/contests/%d", id)
	if code, out := owner.do("PUT", route, map[string]any{"audience": "invited"}, s.srv.URL); code != 200 || out["audience"] != "invited" {
		t.Fatalf("limit before anyone started: %d %v", code, out)
	}
	if code, out := owner.do("PUT", route, map[string]any{"audience": "everyone"}, s.srv.URL); code != 200 || out["audience"] != "everyone" {
		t.Fatalf("open again: %d %v", code, out)
	}
	s.at(start, 5)
	if code, out := asha.do("POST", "/api/contests/audience/start", map[string]any{"accept_honour": true}, s.srv.URL); code >= 300 {
		t.Fatalf("start: %d %v", code, out)
	}
	if code, _ := owner.do("PUT", route, map[string]any{"audience": "invited"}, s.srv.URL); code != 400 {
		t.Errorf("limit after a start: %d, want 400", code)
	}
	if code, _ := owner.do("PUT", route, map[string]any{"audience": "nobody"}, s.srv.URL); code != 400 {
		t.Errorf("unknown audience: %d, want 400", code)
	}
}
