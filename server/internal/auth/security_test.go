package auth_test

// Tests for the security council's identity items (2026-10-18): the run
// mode record (S2), Google re-authentication on shared PCs (S5), and the
// owner's account actions (S6).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

func TestRecordModeUndoesTestingMode(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	// First start with the record: no mode stored yet, so every session
	// ends (the database may have run in testing mode before).
	if _, _, _, err := s.TestSignIn(ctx, "early@gmail.com", "", auth.Admin, "", ""); err != nil {
		t.Fatal(err)
	}
	ch, err := s.RecordMode(ctx, "testing")
	if err != nil || !ch.Changed || ch.Previous != "" || ch.SessionsEnded != 1 || ch.Demoted != 0 {
		t.Fatalf("first record in testing: %+v %v", ch, err)
	}
	if ch, err := s.RecordMode(ctx, "testing"); err != nil || ch.Changed || ch.Previous != "testing" {
		t.Fatalf("restart in the same mode changed something: %+v %v", ch, err)
	}

	// In testing mode anyone becomes an admin or owner from the form.
	_, tok, _, err := s.TestSignIn(ctx, "intruder@gmail.com", "", auth.Owner, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.TestSignIn(ctx, "ta@college.edu", "", auth.Admin, "", ""); err != nil {
		t.Fatal(err)
	}
	// A real admin with a Google link keeps the role.
	if _, err := s.GrantRole(ctx, "prof@college.edu", auth.Admin); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignIn(ctx, id("prof@college.edu"), "", ""); err != nil {
		t.Fatal(err)
	}

	ch, err = s.RecordMode(ctx, "production")
	if err != nil || !ch.Changed || ch.Previous != "testing" {
		t.Fatalf("switch to production: %+v %v", ch, err)
	}
	if ch.Demoted != 3 || ch.SessionsEnded != 3 {
		// intruder, ta and early@gmail.com are demoted; three sessions end.
		t.Errorf("switch to production: demoted %d, ended %d; want 3 and 3", ch.Demoted, ch.SessionsEnded)
	}
	if _, _, err := s.Authenticate(ctx, tok); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("testing-mode session survived the switch: %v", err)
	}
	roles := map[string]auth.Role{}
	rows, _ := s.DB.QueryContext(ctx, "SELECT email, role FROM users")
	for rows.Next() {
		var e string
		var r auth.Role
		rows.Scan(&e, &r)
		roles[e] = r
	}
	rows.Close()
	want := map[string]auth.Role{"intruder@gmail.com": auth.Student, "ta@college.edu": auth.Student,
		"early@gmail.com": auth.Student, "prof@college.edu": auth.Admin, ownerEmail: auth.Owner}
	for e, r := range want {
		if roles[e] != r {
			t.Errorf("%s is %s after the switch, want %s", e, roles[e], r)
		}
	}
	// The demoted intruder, on a domain that is not allowed, cannot get in
	// with a real Google account either.
	if _, _, _, err := s.SignIn(ctx, id("intruder@gmail.com"), "", ""); !errors.Is(err, auth.ErrDomain) {
		t.Errorf("demoted testing-mode owner signing in with Google: %v, want ErrDomain", err)
	}

	// Production to dev also counts as a change: sessions end, and nobody
	// is left to demote.
	if ch, err := s.RecordMode(ctx, "dev"); err != nil || !ch.Changed || ch.Demoted != 0 {
		t.Errorf("switch to dev: %+v %v", ch, err)
	}
}

func TestAccountActions(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	owner, _, _, err := s.SignIn(ctx, id(ownerEmail), "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, tok1, _, _ := s.SignIn(ctx, id("a@college.edu"), "", "")
	_, tok2, _, _ := s.SignIn(ctx, id("a@college.edu"), "", "")
	var ce *auth.ChangeError

	a, n, err := s.EndSessions(ctx, "A@College.edu ")
	if err != nil || n != 2 || a.Email != "a@college.edu" {
		t.Fatalf("end sessions: %+v %d %v", a, n, err)
	}
	for _, tok := range []string{tok1, tok2} {
		if _, _, err := s.Authenticate(ctx, tok); !errors.Is(err, auth.ErrNoSession) {
			t.Errorf("session survived end sessions: %v", err)
		}
	}
	if _, _, err := s.EndSessions(ctx, "nobody@college.edu"); !errors.As(err, &ce) || ce.Kind != "not_found" {
		t.Errorf("end sessions of an unknown email: %v", err)
	}

	_, tok3, _, _ := s.SignIn(ctx, id("a@college.edu"), "", "")
	if a, n, err := s.SetDisabled(ctx, "a@college.edu", true, owner.ID); err != nil || !a.Disabled || n != 1 {
		t.Fatalf("disable: %+v %d %v", a, n, err)
	}
	if _, _, err := s.Authenticate(ctx, tok3); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("disabled account's session accepted: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, id("a@college.edu"), "", ""); !errors.Is(err, auth.ErrDisabled) {
		t.Errorf("disabled account signed in: %v", err)
	}
	if _, _, _, err := s.TestSignIn(ctx, "a@college.edu", "", auth.Student, "", ""); !errors.Is(err, auth.ErrDisabled) {
		t.Errorf("disabled account used test sign-in: %v", err)
	}
	list, err := s.DisabledAccounts(ctx)
	if err != nil || len(list) != 1 || list[0].Email != "a@college.edu" {
		t.Errorf("disabled list: %+v %v", list, err)
	}
	if _, _, err := s.SetDisabled(ctx, "a@college.edu", false, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignIn(ctx, id("a@college.edu"), "", ""); err != nil {
		t.Errorf("re-enabled account cannot sign in: %v", err)
	}

	if _, _, err := s.SetDisabled(ctx, ownerEmail, true, 0); !errors.As(err, &ce) || ce.Kind != "conflict" {
		t.Errorf("disabling the configured owner: %v", err)
	}
	other, err := s.GrantRole(ctx, "second@college.edu", auth.Owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetDisabled(ctx, "second@college.edu", true, other.ID); !errors.As(err, &ce) || ce.Kind != "conflict" {
		t.Errorf("an owner disabling themselves: %v", err)
	}
}

func TestProviderAsksForAFreshSignIn(t *testing.T) {
	idp := authtest.New(t)
	if _, err := exchange(t, idp, nil); err != nil {
		t.Fatalf("fresh sign-in: %v", err)
	}
	if q := idp.LastAuthQuery; q.Get("prompt") != "select_account" || q.Get("max_age") != "300" {
		t.Errorf("auth request: prompt %q max_age %q, want select_account and 300", q.Get("prompt"), q.Get("max_age"))
	}
	// The previous student's Google sign-in, an hour old, does not count.
	if _, err := exchange(t, idp, func(c map[string]any) { c["auth_time"] = time.Now().Add(-time.Hour).Unix() }); err == nil {
		t.Error("token with an hour-old auth_time accepted")
	}
	if _, err := exchange(t, idp, func(c map[string]any) { c["auth_time"] = time.Now().Add(-4 * time.Minute).Unix() }); err != nil {
		t.Errorf("token with a 4-minute-old auth_time refused: %v", err)
	}
	// A provider that does not state auth_time is still accepted.
	if _, err := exchange(t, idp, func(c map[string]any) { delete(c, "auth_time") }); err != nil {
		t.Errorf("token without auth_time refused: %v", err)
	}
}
