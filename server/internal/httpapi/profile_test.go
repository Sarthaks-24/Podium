package httpapi

// The profile page's dates (v4 M1): GET /api/me/profile answers the
// signed-in person's own member-since and last sign-in, and nothing to
// anyone signed out.

import (
	"fmt"
	"testing"
	"time"
)

func TestMyProfileDates(t *testing.T) {
	s := newSite(t)
	before := time.Now().Add(-time.Minute).UnixMilli()
	asha := s.signedIn("asha@college.edu", "Asha")
	code, out := asha.do("GET", "/api/me/profile", nil, "")
	if code != 200 {
		t.Fatalf("profile: %d %v", code, out)
	}
	since, _ := out["member_since"].(float64)
	last, _ := out["last_sign_in"].(float64)
	if int64(last) < before || int64(since) > int64(last) {
		t.Errorf("dates: member since %v, last sign-in %v; want a sign-in just now, after the account was made", since, last)
	}
	if len(out) != 2 {
		t.Errorf("the profile carries only the two dates, got %v", out)
	}
	if code, _ := s.browser().do("GET", "/api/me/profile", nil, ""); code != 401 {
		t.Errorf("signed out: %d, want 401", code)
	}
}

// Your contests (v4 M2): only the contests the person entered, their own
// score, and a place only when the board students see is the full one.
func TestMyContests(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	held := s.createContest(owner, "held", start, map[string]int64{"A": pa}, map[string]any{"freeze_minutes": 30, "hold_reveal": true})
	s.createContest(owner, "open", start, map[string]int64{"A": pa}, nil)
	s.createContest(owner, "other", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")

	s.at(start, 5)
	enter := func(b *browser, slug, prog string) {
		t.Helper()
		if code, out := b.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
			t.Fatalf("start %s: %d %v", slug, code, out)
		}
		s.submit(b, slug, "A", prog)
	}
	enter(asha, "held", "correct")
	enter(asha, "open", "correct")
	enter(bilal, "held", "int32")
	enter(bilal, "open", "int32")
	enter(bilal, "other", "correct")
	s.judgeAll()
	s.at(start, 130)

	mine := func(b *browser) map[string]map[string]any {
		t.Helper()
		code, out := b.do("GET", "/api/me/contests", nil, "")
		if code != 200 {
			t.Fatalf("my contests: %d %v", code, out)
		}
		by := map[string]map[string]any{}
		for _, c := range out["contests"].([]any) {
			row := c.(map[string]any)
			by[row["slug"].(string)] = row
		}
		return by
	}
	got := mine(asha)
	if len(got) != 2 || got["other"] != nil {
		t.Fatalf("asha's contests: %v, want held and open only", got)
	}
	if got["open"]["score"] != float64(100) || got["open"]["max_score"] != float64(100) {
		t.Errorf("open: score %v of %v, want 100 of 100", got["open"]["score"], got["open"]["max_score"])
	}
	if got["open"]["place"] != float64(1) || got["open"]["of"] != float64(2) {
		t.Errorf("open: place %v of %v, want 1 of 2", got["open"]["place"], got["open"]["of"])
	}
	if _, ok := got["held"]["place"]; ok {
		t.Errorf("held before the reveal shows a place: %v", got["held"])
	}
	if got["held"]["score"] != float64(100) {
		t.Errorf("held: her own score %v, want 100 (a student always knows their own)", got["held"]["score"])
	}
	if b := mine(bilal); len(b) != 3 || b["open"]["place"] != float64(2) {
		t.Errorf("bilal's contests: %v, want three, second in open", b)
	}

	if code, out := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/reveal", held), nil, s.srv.URL); code != 200 {
		t.Fatalf("reveal: %d %v", code, out)
	}
	if got := mine(asha); got["held"]["place"] != float64(1) {
		t.Errorf("held after the reveal: %v, want place 1", got["held"])
	}
}
