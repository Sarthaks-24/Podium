package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
)

const day = 24 * time.Hour

var roleEmails = map[auth.Role]string{
	auth.Student: "stu@college.edu", auth.Organiser: "org@college.edu", auth.Admin: "adm@college.edu", auth.Owner: ownerEmail,
}

// keepFixture is a service with one person of each role and a recorder for
// the sessions the server ends.
type keepFixture struct {
	*testing.T
	s     *auth.Service
	c     *clock
	ended []string
}

func newKeepFixture(t *testing.T) *keepFixture {
	s, c := newService(t, roleEmails[auth.Student])
	f := &keepFixture{T: t, s: s, c: c}
	s.OnSessionsEnded = func(ids []string) { f.ended = append(f.ended, ids...) }
	ctx := asOwner(context.Background())
	for _, r := range []auth.Role{auth.Organiser, auth.Admin} {
		if _, err := s.GrantRole(ctx, roleEmails[r], r); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *keepFixture) policy(p auth.KeepPolicy) {
	f.Helper()
	if _, err := f.s.SetKeepPolicy(asOwner(context.Background()), p); err != nil {
		f.Fatal(err)
	}
}

func (f *keepFixture) signIn(role auth.Role, tick bool) (string, auth.Started) {
	f.Helper()
	_, tok, st, err := f.s.SignInKeep(context.Background(), id(roleEmails[role]), "", "", tick)
	if err != nil {
		f.Fatal(err)
	}
	return tok, st
}

func (f *keepFixture) row(token string) (remember bool, expires int64, lastSeen *int64) {
	f.Helper()
	var r int
	err := f.s.DB.QueryRowContext(context.Background(), "SELECT remember, expires_at, last_seen_at FROM sessions WHERE id = ?", hash(token)).Scan(&r, &expires, &lastSeen)
	if err != nil {
		f.Fatalf("session row: %v", err)
	}
	return r == 1, expires, lastSeen
}

func (f *keepFixture) alive(token string) bool {
	f.Helper()
	_, _, err := f.s.Authenticate(context.Background(), token)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		f.Fatal(err)
	}
	return err == nil
}

func (f *keepFixture) sessions() int {
	var n int
	f.s.DB.QueryRow("SELECT count(*) FROM sessions").Scan(&n)
	return n
}

func (f *keepFixture) audit(action string) int {
	var n int
	f.s.DB.QueryRow("SELECT count(*) FROM site_audit WHERE action = ?", action).Scan(&n)
	return n
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func all(m auth.KeepMode) auth.KeepPolicy {
	return auth.KeepPolicy{Students: m, Organisers: m, Admins: m, Owner: m, Days: 30}
}

// Each role gets its own setting; the others are set differently so a rule
// read from the wrong role shows.
func TestKeepWhoGivesTheRightCookieAndRowForEachRole(t *testing.T) {
	modes := []auth.KeepMode{auth.KeepNobody, auth.KeepChoose, auth.KeepEveryone}
	roles := []auth.Role{auth.Student, auth.Organiser, auth.Admin, auth.Owner}
	for _, role := range roles {
		for j, mode := range modes {
			for _, tick := range []bool{false, true} {
				f := newKeepFixture(t)
				p := auth.KeepPolicy{Days: 30}
				set := map[auth.Role]*auth.KeepMode{auth.Student: &p.Students, auth.Organiser: &p.Organisers, auth.Admin: &p.Admins, auth.Owner: &p.Owner}
				for k, r := range roles {
					if r == role {
						*set[r] = mode
					} else {
						*set[r] = modes[(j+1+k)%3]
					}
				}
				f.policy(p)
				tok, st := f.signIn(role, tick)
				wantKept := mode == auth.KeepEveryone || (mode == auth.KeepChoose && tick)
				wantIgnored := mode == auth.KeepNobody && tick
				remember, expires, seen := f.row(tok)
				name := string(role) + "/" + string(mode)
				if st.Remembered != wantKept || st.Ignored != wantIgnored || remember != wantKept {
					t.Errorf("%s tick=%v: remembered %v/%v ignored %v, want %v and ignored %v", name, tick, st.Remembered, remember, st.Ignored, wantKept, wantIgnored)
				}
				wantExp := f.c.t.Add(12 * time.Hour)
				if wantKept {
					wantExp = f.c.t.Add(30 * day)
					if seen == nil || *seen != f.c.t.UnixMilli() {
						t.Errorf("%s tick=%v: last_seen_at %v, want the sign-in time", name, tick, seen)
					}
				} else if seen != nil {
					t.Errorf("%s tick=%v: an ordinary session has last_seen_at %d", name, tick, *seen)
				}
				if expires != wantExp.UnixMilli() || !st.Expires.Equal(wantExp) {
					t.Errorf("%s tick=%v: expires_at %d / %v, want %v", name, tick, expires, st.Expires, wantExp)
				}
			}
		}
	}
}

func TestKeepSignInWithoutAChoiceIsOrdinary(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepChoose))
	_, tok, exp, err := f.s.SignIn(context.Background(), id(roleEmails[auth.Student]), "", "")
	if err != nil {
		t.Fatal(err)
	}
	remember, expires, _ := f.row(tok)
	if remember || !exp.Equal(f.c.t.Add(12*time.Hour)) || expires != exp.UnixMilli() {
		t.Errorf("an old client's sign-in: remembered %v, expires %v, want an ordinary session ending in 12 hours", remember, exp)
	}
}

func TestKeepTestSignInTakesTheSameChoice(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepChoose))
	ctx := context.Background()
	_, tok, st, err := f.s.TestSignInKeep(ctx, "any@college.edu", "", auth.Student, "", "", true)
	if err != nil || !st.Remembered {
		t.Fatalf("ticked: %+v %v", st, err)
	}
	if remember, _, _ := f.row(tok); !remember {
		t.Error("testing-mode session not remembered")
	}
	if _, _, st, err = f.s.TestSignInKeep(ctx, "any@college.edu", "", auth.Student, "", "", false); err != nil || st.Remembered {
		t.Fatalf("not ticked: %+v %v", st, err)
	}
	f.policy(all(auth.KeepNobody))
	if _, _, st, err = f.s.TestSignInKeep(ctx, "any@college.edu", "", auth.Student, "", "", true); err != nil || st.Remembered || !st.Ignored {
		t.Errorf("tick under Nobody: %+v %v, want ignored", st, err)
	}
}

func TestKeepAnOrdinarySessionIsDeadAt12HoursARememberedOneIsNot(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(auth.KeepPolicy{Students: auth.KeepChoose, Organisers: auth.KeepNobody, Admins: auth.KeepNobody, Owner: auth.KeepNobody, Days: 30})
	kept, _ := f.signIn(auth.Student, true)
	plain, _ := f.signIn(auth.Organiser, false)
	f.c.t = f.c.t.Add(12*time.Hour + time.Minute)
	if !f.alive(kept) {
		t.Error("remembered session dead at 12 hours and a minute")
	}
	if f.alive(plain) {
		t.Error("ordinary session alive at 12 hours and a minute")
	}
}

func TestKeepARememberedSessionRenewsOnUseAndEndsAfterTheIdlePeriod(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepEveryone))
	start := f.c.t
	tok, _ := f.signIn(auth.Student, false)

	f.c.t = start.Add(20 * day)
	_, renewed, err := f.s.Authenticate(context.Background(), tok)
	if err != nil || !renewed.Equal(start.Add(50*day)) {
		t.Fatalf("use on day 20: renewed %v (%v), want day 50", renewed, err)
	}
	f.c.t = start.Add(29 * day)
	if !f.alive(tok) {
		t.Error("dead on day 29 after a use on day 20")
	}
	// The use on day 29 renewed it again, to day 59.
	f.c.t = start.Add(58 * day)
	if !f.alive(tok) {
		t.Error("dead on day 58, 29 days after a use on day 29")
	}
	f.c.t = start.Add(88 * day)
	if f.alive(tok) {
		t.Error("alive 30 days after the last use")
	}

	if f.sessions() != 0 {
		t.Error("the dead session row was not deleted")
	}
}

func TestKeepRenewsAtMostOnceAnHour(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepEveryone))
	start := f.c.t
	tok, _ := f.signIn(auth.Student, false)
	ctx := context.Background()
	f.c.t = start.Add(59 * time.Minute)
	if _, renewed, _ := f.s.Authenticate(ctx, tok); !renewed.IsZero() {
		t.Errorf("renewed after 59 minutes: %v", renewed)
	}
	if _, _, seen := f.row(tok); *seen != start.UnixMilli() {
		t.Error("a use inside the hour wrote last_seen_at")
	}
	f.c.t = start.Add(time.Hour)
	if _, renewed, _ := f.s.Authenticate(ctx, tok); renewed.IsZero() {
		t.Error("not renewed after an hour")
	}
	f.c.t = start.Add(time.Hour + 5*time.Minute)
	if _, renewed, _ := f.s.Authenticate(ctx, tok); !renewed.IsZero() {
		t.Errorf("renewed again 5 minutes after a renewal: %v", renewed)
	}
}

func TestKeepAnOrdinarySessionNeverRenews(t *testing.T) {
	f := newKeepFixture(t)
	tok, _ := f.signIn(auth.Student, false)
	_, before, _ := f.row(tok)
	f.c.t = f.c.t.Add(3 * time.Hour)
	if _, renewed, err := f.s.Authenticate(context.Background(), tok); err != nil || !renewed.IsZero() {
		t.Fatalf("renewed %v (%v)", renewed, err)
	}
	if _, after, seen := f.row(tok); after != before || seen != nil {
		t.Error("use changed an ordinary session's row")
	}
}

func TestKeepOnlyWhenTheySignOutLastsAlmostAYear(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(auth.KeepPolicy{Students: auth.KeepEveryone, Organisers: auth.KeepNobody, Admins: auth.KeepNobody, Owner: auth.KeepNobody, Days: 30, UntilSignOut: true})
	start := f.c.t
	tok, st := f.signIn(auth.Student, false)
	if !st.Expires.Equal(start.Add(400 * day)) {
		t.Errorf("expiry %v, want 400 days", st.Expires)
	}
	f.c.t = start.Add(100 * day)
	if !f.alive(tok) {
		t.Fatal("dead on day 100")
	}
	f.c.t = start.Add(499 * day)
	if !f.alive(tok) {
		t.Error("dead 399 days after a use")
	}
	f.c.t = start.Add(899 * day)
	if f.alive(tok) {
		t.Error("alive 400 days after a use")
	}
}

func TestKeepAShorterPeriodAppliesAtNextUseAndAtCleanup(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepEveryone))
	start := f.c.t
	idle, _ := f.signIn(auth.Student, false)
	f.c.t = start.Add(10 * day)
	recent, _ := f.signIn(auth.Organiser, false)
	p := all(auth.KeepEveryone)
	p.Days = 7
	f.policy(p)
	n, err := f.s.DeleteExpiredSessions(context.Background())
	if err != nil || n != 1 || f.sessions() != 1 {
		t.Fatalf("cleanup removed %d (%v), %d sessions left; want the 10-day idle one removed and one left", n, err, f.sessions())
	}
	if f.alive(idle) {
		t.Error("session idle for 10 days is alive under a 7 day period")
	}
	if !f.alive(recent) {
		t.Error("session used today died under a 7 day period")
	}

	// Next use, without the cleanup in between.
	g := newKeepFixture(t)
	g.policy(all(auth.KeepEveryone))
	late, _ := g.signIn(auth.Student, false)
	g.c.t = g.c.t.Add(10 * day)
	g.policy(p)
	if g.alive(late) {
		t.Error("a session idle for 10 days was alive at its next use under a 7 day period")
	}
}

func TestKeepCleanupKeepsRememberedAt12HoursAndDropsOrdinary(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(auth.KeepPolicy{Students: auth.KeepChoose, Organisers: auth.KeepNobody, Admins: auth.KeepNobody, Owner: auth.KeepNobody, Days: 30})
	f.signIn(auth.Student, true)
	f.signIn(auth.Organiser, false)
	f.c.t = f.c.t.Add(13 * time.Hour)
	if n, err := f.s.DeleteExpiredSessions(context.Background()); err != nil || n != 1 {
		t.Fatalf("cleanup removed %d (%v), want the ordinary one", n, err)
	}
	var remember int
	if err := f.s.DB.QueryRow("SELECT remember FROM sessions").Scan(&remember); err != nil || remember != 1 {
		t.Errorf("the remembered session did not survive: %d %v", remember, err)
	}
}

func TestKeepSignOutAndDisableEndASessionUnderEverySetting(t *testing.T) {
	for _, m := range []auth.KeepMode{auth.KeepNobody, auth.KeepChoose, auth.KeepEveryone} {
		f := newKeepFixture(t)
		f.policy(all(m))
		ctx := context.Background()
		tok, _ := f.signIn(auth.Student, true)
		if err := f.s.SignOut(ctx, tok); err != nil || f.alive(tok) || f.sessions() != 0 {
			t.Errorf("%s: sign-out left the session alive", m)
		}
		tok, _ = f.signIn(auth.Student, true)
		if _, _, err := f.s.SetDisabled(asOwner(ctx), roleEmails[auth.Student], true, 1); err != nil {
			t.Fatal(err)
		}
		if f.alive(tok) || f.sessions() != 0 {
			t.Errorf("%s: a disabled user's session is alive", m)
		}
	}
}

// V203-75: the role is read on every request, and a role change ends a
// remembered session so a long session does not carry into a new role.
func TestKeepADemotedAdminIsRefusedAndTheirRememberedSessionEnds(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(auth.KeepPolicy{Students: auth.KeepNobody, Organisers: auth.KeepNobody, Admins: auth.KeepEveryone, Owner: auth.KeepNobody, Days: 30})
	ctx := context.Background()
	kept, _ := f.signIn(auth.Admin, false)
	f.ended = nil
	if _, err := f.s.GrantRole(asOwner(ctx), roleEmails[auth.Admin], auth.Organiser); err != nil {
		t.Fatal(err)
	}
	if f.alive(kept) {
		t.Error("a remembered session survived a role change")
	}
	if len(f.ended) != 1 || f.ended[0] != hash(kept) {
		t.Errorf("ended sessions %v, want the remembered one", f.ended)
	}

	// An ordinary session stays and is read with the new role every time.
	f.policy(all(auth.KeepNobody))
	plain, _ := f.signIn(auth.Organiser, false)
	if _, err := f.s.GrantRole(asOwner(ctx), roleEmails[auth.Organiser], auth.Admin); err != nil {
		t.Fatal(err)
	}
	if u, _, err := f.s.Authenticate(ctx, plain); err != nil || u.Role != auth.Admin {
		t.Errorf("ordinary session after a role change: %+v %v, want it alive with the new role", u, err)
	}
	if _, err := f.s.GrantRole(asOwner(ctx), roleEmails[auth.Organiser], auth.Organiser); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := f.s.Authenticate(ctx, plain); u.Role != auth.Organiser {
		t.Errorf("role on the next request: %q, want organiser", u.Role)
	}
}

func TestKeepSwitchingARoleToNobodyEndsItsRememberedSessionsOnly(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepEveryone))
	s1, _ := f.signIn(auth.Student, false)
	s2, _ := f.signIn(auth.Student, false)
	org, _ := f.signIn(auth.Organiser, false)
	f.policy(auth.KeepPolicy{Students: auth.KeepChoose, Organisers: auth.KeepEveryone, Admins: auth.KeepEveryone, Owner: auth.KeepEveryone, Days: 30})
	plain, _ := f.signIn(auth.Student, false)
	f.ended = nil

	before := f.audit("keep_signed_in_changed")
	res, err := f.s.SetKeepPolicy(asOwner(context.Background()), auth.KeepPolicy{Students: auth.KeepNobody, Organisers: auth.KeepEveryone, Admins: auth.KeepEveryone, Owner: auth.KeepEveryone, Days: 30})
	if err != nil || res.SignedOut != 2 {
		t.Fatalf("save: %+v %v, want 2 signed out", res, err)
	}
	if f.alive(s1) || f.alive(s2) {
		t.Error("a remembered student survived the switch to Nobody")
	}
	if !f.alive(plain) || !f.alive(org) {
		t.Error("an ordinary session or another role's remembered session ended")
	}
	want := []string{hash(s1), hash(s2)}
	sort.Strings(want)
	sort.Strings(f.ended)
	if !reflect.DeepEqual(f.ended, want) {
		t.Errorf("sessions reported ended %v, want %v", f.ended, want)
	}
	if got := f.audit("keep_signed_in_changed") - before; got != 1 {
		t.Errorf("%d audit rows for one save, want 1", got)
	}
}

func TestKeepSaveAuditsOnceAndANoChangeSaveWritesNothing(t *testing.T) {
	f := newKeepFixture(t)
	ctx := asOwner(context.Background())
	p := all(auth.KeepNobody)
	if res, err := f.s.SetKeepPolicy(ctx, p); err != nil || res.SignedOut != 0 || f.audit("keep_signed_in_changed") != 0 {
		t.Fatalf("saving the defaults: %+v %v, audit %d; want no row", res, err, f.audit("keep_signed_in_changed"))
	}
	p.Admins = auth.KeepChoose
	p.Days = 45
	if _, err := f.s.SetKeepPolicy(ctx, p); err != nil || f.audit("keep_signed_in_changed") != 1 {
		t.Fatalf("a change: %v, audit %d, want 1", err, f.audit("keep_signed_in_changed"))
	}
	var who, details string
	f.s.DB.QueryRow("SELECT actor_email, details FROM site_audit WHERE action = 'keep_signed_in_changed'").Scan(&who, &details)
	if who != ownerEmail || details == "" {
		t.Errorf("audit row actor %q details %q", who, details)
	}
	got, err := f.s.KeepPolicy(context.Background())
	if err != nil || got != p {
		t.Errorf("read back %+v (%v), want %+v", got, err, p)
	}
}

func TestKeepRefusesBadValuesWithTheCopyUnderTheField(t *testing.T) {
	f := newKeepFixture(t)
	ctx := asOwner(context.Background())
	const days, mode = "Enter a whole number of days from 1 to 365.", "Choose Nobody, Each person chooses or Everyone."
	for name, c := range map[string]struct {
		mod   func(*auth.KeepPolicy)
		field string
		copy  string
	}{
		"days zero":    {func(p *auth.KeepPolicy) { p.Days = 0 }, "days", days},
		"days 366":     {func(p *auth.KeepPolicy) { p.Days = 366 }, "days", days},
		"days minus":   {func(p *auth.KeepPolicy) { p.Days = -3 }, "days", days},
		"bad students": {func(p *auth.KeepPolicy) { p.Students = "sometimes" }, "students", mode},
		"empty owner":  {func(p *auth.KeepPolicy) { p.Owner = "" }, "owner", mode},
	} {
		p := all(auth.KeepNobody)
		c.mod(&p)
		_, err := f.s.SetKeepPolicy(ctx, p)
		var fe auth.FieldErrors
		if !errors.As(err, &fe) || fe[c.field] != c.copy {
			t.Errorf("%s: %v, want %s: %q", name, err, c.field, c.copy)
		}
	}
	// With "Only when they sign out" the days are not used and not checked.
	p := all(auth.KeepEveryone)
	p.UntilSignOut, p.Days = true, 0
	if _, err := f.s.SetKeepPolicy(ctx, p); err != nil {
		t.Errorf("until sign-out with no days: %v", err)
	}
	if f.audit("keep_signed_in_changed") != 1 {
		t.Error("refused saves wrote audit rows")
	}
}

func TestKeepCountsPeopleWithARememberedSession(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepEveryone))
	f.signIn(auth.Student, false)
	f.signIn(auth.Student, false)
	f.signIn(auth.Admin, false)
	f.signIn(auth.Organiser, false)
	f.signIn(auth.Owner, false)
	got, err := f.s.KeepCounts(context.Background())
	if err != nil || got != (auth.KeepCounts{Students: 1, Organisers: 1, Admins: 1, Owner: 1}) {
		t.Errorf("counts %+v (%v), want one of each role (two student sessions, one person)", got, err)
	}
}

func TestKeepSetRememberTurnsTheCurrentSessionOnAndOff(t *testing.T) {
	f := newKeepFixture(t)
	f.policy(all(auth.KeepChoose))
	ctx := context.Background()
	tok, _ := f.signIn(auth.Admin, false)
	f.c.t = f.c.t.Add(2 * time.Hour)
	st, err := f.s.SetRemember(ctx, tok, true)
	if err != nil || !st.Remembered || !st.Expires.Equal(f.c.t.Add(30*day)) {
		t.Fatalf("on: %+v %v", st, err)
	}
	if remember, _, seen := f.row(tok); !remember || seen == nil {
		t.Error("row not remembered")
	}
	f.c.t = f.c.t.Add(day)
	if !f.alive(tok) {
		t.Fatal("remembered session dead after a day")
	}
	st, err = f.s.SetRemember(ctx, tok, false)
	if err != nil || st.Remembered || !st.Expires.Equal(f.c.t.Add(12*time.Hour)) {
		t.Fatalf("off: %+v %v, want an ordinary session of 12 hours from now", st, err)
	}
	if remember, expires, seen := f.row(tok); remember || seen != nil || expires != f.c.t.Add(12*time.Hour).UnixMilli() {
		t.Error("row not ordinary after turning off")
	}
	if !f.alive(tok) {
		t.Error("turning off signed the person out at once")
	}
}

func TestKeepSetRememberIsRefusedUnlessTheRoleChooses(t *testing.T) {
	for _, m := range []auth.KeepMode{auth.KeepNobody, auth.KeepEveryone} {
		f := newKeepFixture(t)
		f.policy(all(m))
		tok, _ := f.signIn(auth.Admin, false)
		_, err := f.s.SetRemember(context.Background(), tok, true)
		var ce *auth.ChangeError
		if !errors.As(err, &ce) || ce.Kind != "conflict" || ce.Message != "Podium does not let you choose whether to stay signed in." {
			t.Errorf("%s: %v, want a conflict saying so", m, err)
		}
	}
	f := newKeepFixture(t)
	f.policy(all(auth.KeepChoose))
	if _, err := f.s.SetRemember(context.Background(), "nope", true); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("unknown session: %v", err)
	}
}

// Every way the server ends a session tells OnSessionsEnded, so a provider
// key held for the session is forgotten.
func TestKeepEveryServerSideEndForgetsTheSessionsKeys(t *testing.T) {
	ctx := context.Background()
	owner := asOwner(ctx)
	cases := map[string]func(f *keepFixture, tok string){
		"sign-out":         func(f *keepFixture, tok string) { f.s.SignOut(ctx, tok) },
		"disable":          func(f *keepFixture, _ string) { f.s.SetDisabled(owner, roleEmails[auth.Student], true, 1) },
		"end sessions":     func(f *keepFixture, _ string) { f.s.EndSessions(owner, roleEmails[auth.Student]) },
		"remove":           func(f *keepFixture, _ string) { f.s.Remove(owner, roleEmails[auth.Student]) },
		"reset google":     func(f *keepFixture, _ string) { f.s.ResetGoogleAccount(owner, roleEmails[auth.Student]) },
		"mode change":      func(f *keepFixture, _ string) { f.s.RecordMode(ctx, "testing") },
		"cleanup":          func(f *keepFixture, _ string) { f.c.t = f.c.t.Add(500 * day); f.s.DeleteExpiredSessions(ctx) },
		"expired on use":   func(f *keepFixture, tok string) { f.c.t = f.c.t.Add(500 * day); f.s.Authenticate(ctx, tok) },
		"switch to nobody": func(f *keepFixture, _ string) { f.s.SetKeepPolicy(owner, all(auth.KeepNobody)) },
	}
	for name, end := range cases {
		f := newKeepFixture(t)
		f.policy(all(auth.KeepEveryone))
		tok, _ := f.signIn(auth.Student, false)
		f.ended = nil
		end(f, tok)
		if len(f.ended) != 1 || f.ended[0] != hash(tok) {
			t.Errorf("%s: sessions reported ended %v, want [%s]", name, f.ended, hash(tok))
		}
	}
}

// The tick is inside the signed sign-in state: it round-trips, and a state
// sealed before the field existed opens with no tick.
func TestKeepTheTickTravelsInTheSignedState(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	f := auth.NewFlow("/", now)
	if f.Remember {
		t.Fatal("a new flow starts with the tick on")
	}
	f.Remember = true
	v, err := auth.SealFlow(secret, f)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := auth.OpenFlow(secret, v, now); err != nil || !got.Remember {
		t.Errorf("ticked flow: %+v %v", got, err)
	}
	f.Remember = false
	v, _ = auth.SealFlow(secret, f)
	if got, err := auth.OpenFlow(secret, v, now); err != nil || got.Remember {
		t.Errorf("unticked flow: %+v %v", got, err)
	}
}
