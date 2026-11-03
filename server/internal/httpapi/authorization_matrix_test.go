package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestContestAuthorizationMatrix pins the contest-level policy before its
// implementation. The same actors are exercised at every kind of door; C
// and I slices fill in the rows that are not supported yet.
func TestContestAuthorizationMatrix(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "admin@college.edu", "role": "admin"}, s.srv.URL); code >= 300 {
		t.Fatalf("make admin: %d %v", code, out)
	}
	for _, email := range []string{"listed@college.edu", "unlisted@college.edu", "removed@college.edu"} {
		if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": email, "role": "organiser"}, s.srv.URL); code >= 300 {
			t.Fatalf("make organiser %s: %d %v", email, code, out)
		}
	}
	admin := s.signedIn("admin@college.edu", "Admin")
	listed := s.signedIn("listed@college.edu", "Listed")
	unlisted := s.signedIn("unlisted@college.edu", "Unlisted")
	removed := s.signedIn("removed@college.edu", "Removed")
	student := s.signedIn("asha@college.edu", "Asha")
	otherStudent := s.signedIn("bilal@college.edu", "Bilal")
	problem := s.importProblem(owner, "matrix-problem")
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	contestID := s.createContest(owner, "matrix-contest", start, map[string]int64{"A": problem}, nil)
	listedID := listed.userID()
	if _, err := s.db.Exec("INSERT INTO contest_organisers (contest_id, user_id, added_by, added_at) VALUES (?, ?, ?, ?)", contestID, listedID, owner.userID(), s.clock.now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// The removed organiser managed the contest a moment ago: nothing may
	// cache that.
	organisers := fmt.Sprintf("/api/admin/contests/%d/organisers", contestID)
	if code, out := owner.do("POST", organisers, map[string]any{"email": "removed@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("add removed: %d %v", code, out)
	}
	if code, _ := removed.do("GET", fmt.Sprintf("/api/admin/contests/%d", contestID), nil, ""); code != 200 {
		t.Fatalf("removed organiser before removal: %d", code)
	}
	if code, _ := owner.do("DELETE", fmt.Sprintf("%s/%d", organisers, removed.userID()), nil, s.srv.URL); code != 204 {
		t.Fatalf("remove organiser: %d", code)
	}
	if code, out := student.do("POST", "/api/contests/matrix-contest/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
		t.Fatalf("start for source fixture: %d %v", code, out)
	}
	code, submission := s.submit(student, "matrix-contest", "A", "correct")
	if code != http.StatusAccepted {
		t.Fatalf("submit for source fixture: %d %v", code, submission)
	}
	submissionID := int64(submission["id"].(float64))
	// These rows define the eventual role expectations. A later slice adds
	// the listed organiser to the contest; I1 adds invite-based student rows.
	actors := []struct {
		name   string
		user   *browser
		view   int
		manage int
	}{
		{"listed organiser", listed, http.StatusOK, http.StatusOK},
		{"unlisted organiser", unlisted, http.StatusOK, http.StatusNotFound},
		{"organiser just removed", removed, http.StatusOK, http.StatusNotFound},
		{"admin", admin, http.StatusOK, http.StatusOK},
		{"owner", owner, http.StatusOK, http.StatusOK},
		{"student", student, http.StatusOK, http.StatusNotFound},
		{"other student", otherStudent, http.StatusOK, http.StatusNotFound},
	}
	routes := []struct {
		name, method, path string
		class              string
	}{
		{"contest page", "GET", "/api/contests/matrix-contest", "view"},
		{"standings", "GET", "/api/contests/matrix-contest/standings", "view"},
		{"console", "GET", fmt.Sprintf("/api/admin/contests/%d", contestID), "manage"},
		{"export", "GET", fmt.Sprintf("/api/admin/contests/%d/export.csv", contestID), "manage"},
		{"source", "GET", fmt.Sprintf("/api/submissions/%d", submissionID), "source"},
		{"per-person submissions", "GET", fmt.Sprintf("/api/admin/people/%d/submissions", otherStudent.userID()), "people"},
		{"start", "POST", "/api/contests/matrix-contest/start", "participate"},
		{"stream", "GET", "/api/events", "stream"},
	}
	for _, actor := range actors {
		for _, route := range routes {
			if route.class == "participate" && actor.name != "unlisted organiser" && actor.name != "organiser just removed" && actor.name != "student" {
				continue
			}
			want := actor.view
			switch route.class {
			case "manage":
				want = actor.manage
				if actor.name == "student" || actor.name == "other student" {
					want = http.StatusForbidden // rejected by the route role before contest lookup.
				}
			case "source":
				want = http.StatusNotFound
				if actor.name == "student" || actor.name == "listed organiser" || actor.name == "admin" || actor.name == "owner" {
					want = http.StatusOK
				}
			case "people":
				want = http.StatusForbidden
				if actor.name == "admin" || actor.name == "owner" {
					want = http.StatusOK
				}
			case "participate":
				if actor.name != "student" {
					want = http.StatusNotFound
				}
			case "stream":
				continue // stream delivery is asserted by the event-specific matrix below.
			}
			body := map[string]any{}
			if route.name == "start" {
				body["accept_honour"] = true
			}
			code, _ := actor.user.do(route.method, route.path, body, s.srv.URL)
			if code != want {
				t.Errorf("%s on %s: got %d, want %d", actor.name, route.name, code, want)
			}
		}
	}
}

// TestInviteOnlyAuthorizationMatrix: on an invite-only contest the invited
// student participates, an uninvited one gets 404 at every door, a disabled
// one is signed out, and staff keep their levels whatever the invites say.
func TestInviteOnlyAuthorizationMatrix(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "unlisted@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("make organiser: %d %v", code, out)
	}
	unlisted := s.signedIn("unlisted@college.edu", "Unlisted")
	invited := s.signedIn("asha@college.edu", "Asha")
	uninvited := s.signedIn("bilal@college.edu", "Bilal")
	disabled := s.signedIn("chen@college.edu", "Chen")
	problem := s.importProblem(owner, "invite-matrix-problem")
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	contestID := s.createContest(owner, "invite-matrix", start, map[string]int64{"A": problem}, map[string]any{"audience": "invited"})
	if code, out := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/invites", contestID),
		map[string]any{"user_ids": []int64{invited.userID(), disabled.userID()}}, s.srv.URL); code != 200 || out["added"] != float64(2) {
		t.Fatalf("invite: %d %v", code, out)
	}
	if code, out := owner.do("POST", "/api/admin/accounts/disable", map[string]any{"email": "chen@college.edu"}, s.srv.URL); code >= 300 {
		t.Fatalf("disable: %d %v", code, out)
	}
	actors := []struct {
		name  string
		user  *browser
		view  int // page, standings
		start int // start, workspace
	}{
		{"invited student", invited, http.StatusOK, http.StatusOK},
		{"uninvited student", uninvited, http.StatusNotFound, http.StatusNotFound},
		{"disabled invited student", disabled, http.StatusUnauthorized, http.StatusUnauthorized},
		{"unlisted organiser", unlisted, http.StatusOK, http.StatusNotFound},
		{"owner", owner, http.StatusOK, http.StatusOK},
	}
	for _, a := range actors {
		for _, r := range []struct {
			method, path string
			want         int
		}{
			{"GET", "/api/contests/invite-matrix", a.view},
			{"GET", "/api/contests/invite-matrix/standings", a.view},
			{"POST", "/api/contests/invite-matrix/start", a.start},
			{"GET", "/api/contests/invite-matrix/problems/A", a.start},
		} {
			body := map[string]any{}
			if r.method == "POST" {
				body["accept_honour"] = true
			}
			if a.name == "owner" && r.method == "POST" {
				continue // staff do not start contests
			}
			if code, _ := a.user.do(r.method, r.path, body, s.srv.URL); code != r.want {
				t.Errorf("%s on %s %s: got %d, want %d", a.name, r.method, r.path, code, r.want)
			}
		}
	}
	if _, list := uninvited.do("GET", "/api/contests", nil, ""); len(list["live"].([]any)) != 0 {
		t.Errorf("uninvited student's contest list shows it: %v", list["live"])
	}
	if _, list := invited.do("GET", "/api/contests", nil, ""); len(list["live"].([]any)) != 1 {
		t.Errorf("invited student's contest list: %v", list["live"])
	}
}
