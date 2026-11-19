package submissions

// The contest setting "Locked code" (v2.1.1 stage P) changes what a student
// is shown, never what is judged.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Hidden sends both locked maps empty, folded and shown send the code, and
// the answer names the setting ("shown" when the scope has none).
func TestWorkspaceLockedCodeModes(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	for _, c := range []struct {
		scope, want string
		code        bool
	}{{"", "shown", true}, {"shown", "shown", true}, {"folded", "folded", true}, {"hidden", "hidden", false}} {
		w, err := r.svc.Workspace(ctx, p.ID, 1, Scope{LockedCode: c.scope})
		if err != nil {
			t.Fatal(err)
		}
		if w.LockedCode != c.want || (w.LockedTop["cpp17"] != "") != c.code || (w.LockedBottom["python3"] != "") != c.code {
			t.Errorf("%q: locked_code %q, top %v, bottom %v", c.scope, w.LockedCode, w.LockedTop, w.LockedBottom)
		}
		if w.Mode != "function" || len(w.Languages) != 2 {
			t.Errorf("%q: mode %q, %d languages", c.scope, w.Mode, len(w.Languages))
		}
		b, _ := json.Marshal(w)
		var raw map[string]json.RawMessage
		json.Unmarshal(b, &raw)
		if !c.code && (string(raw["locked_top"]) != "{}" || string(raw["locked_bottom"]) != "{}") {
			t.Errorf("hidden payload: top %s, bottom %s", raw["locked_top"], raw["locked_bottom"])
		}
	}
}

// A submission in a hidden contest is judged exactly as in a shown one: the
// same composed file, the same top line count, the same source kept.
func TestHiddenContestJudgesTheSameAsShown(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	type judged struct {
		source, composed string
		top              int
	}
	var got []judged
	for _, mode := range []string{"shown", "hidden"} {
		sc := Scope{LockedCode: mode, View: ViewOptions{Coarse: true, RoundMS: 100, HideLocked: mode == "hidden"}}
		v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: fnCode, Scope: sc})
		if err != nil {
			t.Fatal(err)
		}
		source, _, top, payload := r.row(t, v.ID)
		var pl judgeapi.SubmitPayload
		json.Unmarshal([]byte(payload), &pl)
		got = append(got, judged{source, r.blob(t, pl.Program.SourceSHA), top})
		r.judgeNext(t, judgeapi.KindSubmit, nil)
		r.clock.advance(time.Minute)
	}
	a, b := got[0], got[1]
	if a.source != b.source || a.composed != b.composed || a.top != b.top || a.composed != fnTopCpp+fnCode+fnBottomCpp {
		t.Errorf("shown judged %q (top %d), hidden judged %q (top %d)", a.composed, a.top, b.composed, b.top)
	}
}
