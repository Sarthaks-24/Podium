package auth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// Sign-in by the list (V4-15, V4-18, stage O O3).
func TestSignInByTheList(t *testing.T) {
	s, _ := newService(t, "asha@college.edu", "ab@gmail.com", "cd@googlemail.com")
	ctx := context.Background()

	if _, _, _, err := s.SignIn(ctx, id("asha@college.edu"), "", ""); err != nil {
		t.Errorf("listed address: %v", err)
	}
	// Gmail ignores dots and treats googlemail.com as gmail.com.
	u, _, _, err := s.SignIn(ctx, id("a.b@gmail.com"), "", "")
	if err != nil || u.Email != "ab@gmail.com" {
		t.Errorf("dotted Gmail for an undotted entry: %+v %v; the row keeps its listed address", u, err)
	}
	if _, _, _, err := s.SignIn(ctx, id("c.d@gmail.com"), "", ""); err != nil {
		t.Errorf("gmail.com for a googlemail.com entry: %v", err)
	}
	// Dots are ignored only for Gmail, and nothing else is rewritten.
	for _, e := range []string{"a.sha@college.edu", "ab+x@gmail.com"} {
		if _, _, _, err := s.SignIn(ctx, id(e), "", ""); !errors.Is(err, auth.ErrNotListed) {
			t.Errorf("%s: %v, want ErrNotListed", e, err)
		}
	}

	// A refused address is recorded with a count, never as a user.
	for range 2 {
		s.SignIn(ctx, id("stranger@example.org"), "", "")
	}
	_, counts, refused, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counted := map[string]int{}
	for _, r := range refused {
		counted[r.Email] = r.Count
	}
	if len(refused) != 3 || counted["stranger@example.org"] != 2 {
		t.Errorf("refused: %+v, want a.sha, ab+x and stranger twice", refused)
	}
	if counts.OnList != 4 || counts.SignedIn != 3 || counts.NeverCame != 1 {
		t.Errorf("counts %+v, want 4 on the list, 3 signed in (owner never did), 1 never came", counts)
	}

	// Let in adds the address and forgets the refusal; the next sign-in works.
	if err := s.LetIn(asOwner(ctx), "Stranger@Example.org"); err != nil {
		t.Fatal(err)
	}
	if _, _, refused, _ := s.List(ctx); len(refused) != 2 {
		t.Errorf("after Let in, refused: %+v (only a.sha and ab+x remain)", refused)
	}
	if _, _, _, err := s.SignIn(ctx, id("stranger@example.org"), "", ""); err != nil {
		t.Errorf("after Let in: %v", err)
	}
}

// Removing someone deletes a row nobody signed in with, and disables and
// signs out one that did; either way the next sign-in is refused.
func TestRemoveFromTheList(t *testing.T) {
	s, _ := newService(t, "came@college.edu", "never@college.edu")
	ctx := asOwner(context.Background())
	_, token, _, _ := s.SignIn(ctx, id("came@college.edu"), "", "")

	deleted, err := s.Remove(ctx, "came@college.edu")
	if err != nil || deleted {
		t.Fatalf("remove someone who signed in: deleted %v, %v; want disabled", deleted, err)
	}
	if _, _, err := s.Authenticate(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("removed person's session still works: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, id("came@college.edu"), "", ""); !errors.Is(err, auth.ErrDisabled) {
		t.Errorf("removed person signing in again: %v", err)
	}
	deleted, err = s.Remove(ctx, "never@college.edu")
	if err != nil || !deleted {
		t.Fatalf("remove someone who never signed in: deleted %v, %v", deleted, err)
	}
	if _, _, _, err := s.SignIn(ctx, id("never@college.edu"), "", ""); !errors.Is(err, auth.ErrNotListed) {
		t.Errorf("deleted person signing in: %v", err)
	}
	var ce *auth.ChangeError
	for _, e := range []string{ownerEmail, "nobody@college.edu"} {
		if _, err := s.Remove(ctx, e); !errors.As(err, &ce) {
			t.Errorf("remove %s: %v, want a refusal", e, err)
		}
	}
}

// A paste is checked first (dry run), then saved with the same answer; a
// second paste adds nothing.
func TestImport(t *testing.T) {
	s, _ := newService(t, "ab@gmail.com")
	ctx := asOwner(context.Background())
	text := "new1@college.edu, NEW2@college.edu\nnew1@college.edu; a.b@gmail.com not-an-email\t<new3@college.edu>"
	dry, err := s.Import(ctx, text, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "added [new1@college.edu new2@college.edu new3@college.edu] duplicates [new1@college.edu a.b@gmail.com] invalid [not-an-email]"
	got := fmt.Sprintf("added %v duplicates %v invalid %v", dry.Added, dry.Duplicates, dry.Invalid)
	if got != want {
		t.Errorf("dry run:\n got %s\nwant %s", got, want)
	}
	if _, c, _, _ := s.List(ctx); c.OnList != 2 {
		t.Errorf("a dry run saved something: %+v", c)
	}
	saved, err := s.Import(ctx, text, false)
	if err != nil || fmt.Sprint(saved) != fmt.Sprint(dry) {
		t.Errorf("save: %+v %v, want the dry run's answer", saved, err)
	}
	again, _ := s.Import(ctx, text, false)
	if len(again.Added) != 0 {
		t.Errorf("second paste added %v", again.Added)
	}
	var ce *auth.ChangeError
	if _, err := s.Import(ctx, strings.Repeat("x@y.edu ", auth.MaxImport+1), true); !errors.As(err, &ce) || ce.Kind != "invalid" {
		t.Errorf("an oversized paste: %v", err)
	}
}

// The refused list keeps the 500 most recent addresses.
func TestRefusedListIsCapped(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	for i := range auth.MaxRefused + 1 {
		c.t = c.t.Add(time.Second)
		s.SignIn(ctx, id(fmt.Sprintf("x%d@example.org", i)), "", "")
	}
	_, _, refused, _ := s.List(ctx)
	if len(refused) != auth.MaxRefused {
		t.Fatalf("%d refused rows kept, want %d", len(refused), auth.MaxRefused)
	}
	for _, r := range refused {
		if r.Email == "x0@example.org" {
			t.Error("the oldest refusal was kept and a newer one dropped")
		}
	}
}

// An admin cannot plant a second row for a protected person by spelling
// their Gmail address with dots (security review 2026-10-31, M1): every
// path resolves the person through the same canonical match.
func TestGrantRoleFollowsGmailDots(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	if _, err := s.GrantRole(asOwner(ctx), "johnsmith@gmail.com", auth.Owner); err != nil {
		t.Fatal(err)
	}
	boss, err := s.GrantRole(asOwner(ctx), "boss@college.edu", auth.Admin)
	if err != nil {
		t.Fatal(err)
	}
	asAdmin := auth.WithActor(ctx, auth.Actor{ID: boss.ID, Email: boss.Email, Role: auth.Admin})
	var ce *auth.ChangeError
	if _, err := s.GrantRole(asAdmin, "john.smith@gmail.com", auth.Organiser); !errors.As(err, &ce) || ce.Kind != "forbidden" {
		t.Errorf("admin targets an owner through a dotted spelling: %v, want forbidden", err)
	}
	var n int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE email LIKE '%smith@gmail.com'").Scan(&n)
	if n != 1 {
		t.Errorf("%d rows for one Gmail mailbox, want 1", n)
	}
	// An owner granting through another spelling changes the one row.
	p, err := s.GrantRole(asOwner(ctx), "John.Smith@googlemail.com", auth.Organiser)
	if err != nil || p.Email != "johnsmith@gmail.com" {
		t.Errorf("owner grant through another spelling: %+v %v, want the listed row", p, err)
	}
}

// The configured owner is matched the same way: a dotted spelling in the
// configuration finds the row already listed instead of adding a second.
func TestBootstrapFollowsGmailDots(t *testing.T) {
	s, _ := newService(t, "priya@gmail.com")
	ctx := context.Background()
	s.OwnerEmail = "pri.ya@gmail.com"
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	var role string
	s.DB.QueryRowContext(ctx, "SELECT count(*), max(role) FROM users WHERE email LIKE 'pri%ya@gmail.com'").Scan(&n, &role)
	if n != 1 || role != "owner" {
		t.Errorf("%d rows, role %q; want the one listed row made owner", n, role)
	}
}
