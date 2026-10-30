package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// The sign-in switch (stage O O6): closed, nobody below admin starts a
// session, a session already open keeps working, and admins and owners
// still get in; each change is audited once.
func TestSignInSwitch(t *testing.T) {
	s, _ := newService(t, "asha@college.edu", "ravi@college.edu")
	ctx := context.Background()
	if _, err := s.GrantRole(asOwner(ctx), "boss@college.edu", auth.Admin); err != nil {
		t.Fatal(err)
	}
	if open, err := s.SignInOpen(ctx); err != nil || !open {
		t.Fatalf("a new site: open %v %v, want open", open, err)
	}
	_, ashaToken, _, err := s.SignIn(ctx, id("asha@college.edu"), "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetSignInOpen(asOwner(ctx), false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignIn(ctx, id("ravi@college.edu"), "", ""); !errors.Is(err, auth.ErrSignInClosed) {
		t.Errorf("student while closed: %v, want ErrSignInClosed", err)
	}
	if _, _, err := s.Authenticate(ctx, ashaToken); err != nil {
		t.Errorf("an open session ended when sign-in closed: %v", err)
	}
	for _, e := range []string{"boss@college.edu", ownerEmail} {
		if _, _, _, err := s.SignIn(ctx, id(e), "", ""); err != nil {
			t.Errorf("%s while closed: %v", e, err)
		}
	}
	// Closing again changes nothing.
	if err := s.SetSignInOpen(asOwner(ctx), false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSignInOpen(asOwner(ctx), true); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignIn(ctx, id("ravi@college.edu"), "", ""); err != nil {
		t.Errorf("student after opening: %v", err)
	}
	rows, _, _ := s.SiteAudit(ctx, auth.SiteAuditFilter{})
	var got []string
	for _, r := range rows {
		if r.Action == auth.AuditSignInClosed || r.Action == auth.AuditSignInOpened {
			got = append(got, r.Action)
		}
	}
	if len(got) != 2 || got[0] != auth.AuditSignInOpened || got[1] != auth.AuditSignInClosed {
		t.Errorf("audit %v, want one close then one open", got)
	}
}
