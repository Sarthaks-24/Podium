package httpapi

// The profile page's dates (v4 M1): GET /api/me/profile answers the
// signed-in person's own member-since and last sign-in, and nothing to
// anyone signed out.

import (
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
