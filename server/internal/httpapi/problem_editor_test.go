package httpapi

import (
	"strconv"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

// The editor's new-problem form sends a title only, and its Validate on a
// problem with missing outputs asks /generate to validate afterwards.
func TestProblemEditorCreateAndValidateWithOutputs(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")

	code, p := owner.do("POST", "/api/admin/problems", map[string]any{"title": "Largest of three"}, s.srv.URL)
	if code != 201 || p["slug"] != "largest-of-three" {
		t.Fatalf("create without a slug: %d %v", code, p)
	}
	id := strconv.Itoa(int(p["id"].(float64)))

	if code, body := owner.do("POST", "/api/admin/problems/"+id+"/generate", map[string]any{"all": true, "then_validate": true}, s.srv.URL); code != 400 {
		t.Errorf("all with then_validate: %d %v", code, body)
	}
	// No tests yet: refused with the reason, as Validate would be.
	if code, body := owner.do("POST", "/api/admin/problems/"+id+"/generate", map[string]any{"then_validate": true}, s.srv.URL); code != 409 || errCode(body) != "not_ready" {
		t.Errorf("then_validate without tests: %d %v", code, body)
	}
}
