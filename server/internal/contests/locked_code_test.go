package contests

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

// The default is shown, a save without the field keeps the stored value, a
// bad value is a field error, a change is audited and a clone copies it.
func TestLockedCodeSetting(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	if c.LockedCode != LockedShown {
		t.Fatalf("new contest has %q, want shown", c.LockedCode)
	}
	edit := func(c *Contest, mode string) (*Contest, error) {
		e := Edit{Title: c.Title, StartsAt: c.Schedule.StartsAt, EndsAt: c.Schedule.EndsAt, PracticeAfterEnd: true, MinGapS: 30,
			MaxPerProblem: 15, Languages: c.Languages, FreezeMinutes: 30, LockedCode: mode}
		return r.svc.Update(ctx, c.ID, e, owner)
	}
	c, err := edit(c, LockedHidden)
	if err != nil || c.LockedCode != LockedHidden {
		t.Fatalf("set hidden: %v, %v", err, c)
	}
	// An old tab that does not send the field cannot reset it.
	c, err = edit(c, "")
	if err != nil || c.LockedCode != LockedHidden {
		t.Fatalf("save without the field: %v, %v, want hidden kept", err, c)
	}
	var fe FieldErrors
	if _, err := edit(c, "x"); !errors.As(err, &fe) || fe["locked_code"] != "Choose shown, folded or hidden." {
		t.Errorf("bad value: %v", err)
	}
	// It may change at any time, also after students started.
	execNoFK(t, r, `INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (`+strconv.FormatInt(c.ID, 10)+`, 2, 1, 1)`)
	if c, err = edit(c, LockedFolded); err != nil || c.LockedCode != LockedFolded {
		t.Fatalf("change after a start: %v, %v", err, c)
	}
	if n := count(t, r, `SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = ? AND details LIKE '%locked_code%'`, c.ID, AuditSettings); n != 2 {
		t.Errorf("%d audit rows name locked_code, want 2 (the save without the field changed nothing)", n)
	}
	clone, err := r.svc.Clone(ctx, c.ID, "week-1-copy", "Week 1 copy", c.Schedule.StartsAt, c.Schedule.EndsAt, owner)
	if err != nil || clone.LockedCode != LockedFolded {
		t.Errorf("clone: %v, %v", err, clone)
	}
}

// Only a hidden contest asks for the compiler and run output to be cleaned.
func TestLockedCodeViewOptions(t *testing.T) {
	for mode, want := range map[string]bool{"": false, LockedShown: false, LockedFolded: false, LockedHidden: true} {
		c := &Contest{LockedCode: mode}
		got := c.ViewOptions(StudentState{})
		if got.HideLocked != want || !got.Coarse {
			t.Errorf("%q: HideLocked %v, want %v", mode, got.HideLocked, want)
		}
	}
}
