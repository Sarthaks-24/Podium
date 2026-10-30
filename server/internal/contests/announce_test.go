package contests

import (
	"strings"
	"testing"
)

// The prefilled announcement after "Update students now" says what changed:
// judging, the statement, or only the starter code (v4 E5).
func TestAnnouncementSaysWhatChanged(t *testing.T) {
	cases := []struct {
		u    PinUpdate
		want string
	}{
		{PinUpdate{JudgingChanged: true, StatementChanged: true}, "judged again"},
		{PinUpdate{StatementChanged: true, StartersChanged: true}, "The statement of problem B was updated"},
		{PinUpdate{StartersChanged: true}, "Problem B now opens with new starter code"},
	}
	for _, c := range cases {
		if got := announcementFor("B", &c.u); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q, want it to contain %q", c.u, got, c.want)
		}
	}
}
