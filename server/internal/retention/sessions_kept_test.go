package retention_test

import (
	"testing"

	"github.com/Sarthaks-24/podium/internal/retention"
)

func TestSessionsKeptSaysWhatTheSettingDoes(t *testing.T) {
	for _, c := range []struct {
		days  int
		until bool
		want  string
	}{
		{30, false, "12 hours after sign-in, or 30 days after last use for people who stay signed in"},
		{1, false, "12 hours after sign-in, or 1 day after last use for people who stay signed in"},
		{30, true, "12 hours after sign-in, or until they sign out for people who stay signed in (a browser forgets the sign-in after about 13 months without a visit)"},
	} {
		p := retention.SessionsKept(c.days, c.until)
		if p.Kept != c.want || p.ID != "sessions" || p.Amount != 12 || p.Unit != "hours" || p.After != "sign_in" {
			t.Errorf("SessionsKept(%d, %v) = %+v, want kept %q", c.days, c.until, p, c.want)
		}

	}
}

func TestPeriodsKeptReplacesOnlyTheSessionsRowAndKeepsTheOrder(t *testing.T) {
	plain, kept := retention.Periods(), retention.PeriodsKept(7, false)
	if len(plain) != len(kept) {
		t.Fatalf("%d rows became %d", len(plain), len(kept))
	}
	for i := range plain {
		if plain[i].ID != kept[i].ID {
			t.Errorf("row %d: %s became %s", i, plain[i].ID, kept[i].ID)
		}
		if (plain[i].ID == "sessions") == (plain[i].Kept == kept[i].Kept) {
			t.Errorf("row %s: %q then %q", plain[i].ID, plain[i].Kept, kept[i].Kept)
		}
	}
}
