package contests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// today is the body GetStandings sends now: httpx.WriteJSON's encoder.
func today(t testing.TB, o *standingsJSON) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(o); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func addStudent(t testing.TB, r *svcRig, id int, name string) {
	t.Helper()
	if _, err := r.svc.DB.Exec("INSERT INTO users (id, email, name, role, created_at) VALUES (?, ?, ?, 'student', 0)", id, fmt.Sprintf("u%d@college.edu", id), name); err != nil {
		t.Fatal(err)
	}
}

// The spliced body equals today's, byte for byte, for every board variant
// and viewer the handler has: full and frozen boards, a student seeing
// their own row, an organiser, an organiser's student view, an empty board,
// a board with ties, an extension, names that need escaping, and the
// server clock.
func TestSplicedStandingsEqualTodays(t *testing.T) {
	ctx := context.Background()
	r := newSvc(t)
	h := &Handlers{Contests: r.svc}
	r.svc.StandingsEncodeOnce = true

	empty := r.contest(t, func(e *Edit) { e.FreezeMinutes = 0 })
	empty.Problems = nil

	c := r.contest(t, nil) // freezes the last 30 minutes
	seedBoard(t, r, c, 6, 2)
	addStudent(t, r, 2000, `<b>Zoë "Z" & co</b> é 日本`)
	if _, err := r.svc.DB.Exec("INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, 2000, ?, ?)", c.ID, t0.UnixMilli(), t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec("INSERT INTO contest_extensions (contest_id, user_id, minutes) VALUES (?, 1001, 15)", c.ID); err != nil {
		t.Fatal(err)
	}
	// A contest whose problems are hidden from the labels it shows.
	few := r.contest(t, func(e *Edit) { e.Title = "Week 2" })
	seedBoard(t, r, few, 3, 1)
	few.Problems = few.Problems[:2]

	cases := 0
	for name, contest := range map[string]*Contest{"empty": empty, "ties and extension": c, "two problems": few} {
		for _, now := range []time.Duration{-10, 30, 95, 130} { // before, during, frozen, after
			*r.now = at(int(now))
			for _, variant := range []string{VariantFull, VariantFrozen} {
				for _, student := range []bool{false, true} {
					for _, me := range []int64{0, 1, 2, 1001, 2000} {
						o, err := h.standingsOut(ctx, contest, variant, me, student)
						if err != nil {
							t.Fatal(err)
						}
						got, ok := o.splice()
						if !ok {
							t.Fatalf("%s: no spliced body", name)
						}
						if want := today(t, o); !bytes.Equal(got, want) {
							t.Fatalf("%s at %d min, %s, student view %v, me %d:\n got %s\nwant %s", name, now, variant, student, me, got, want)
						}
						cases++
					}
				}
			}
		}
	}
	t.Logf("%d variants compared", cases)
}

// A new board version gets new bytes; two viewers of one version share the
// rows but never each other's own fields.
func TestSplicedStandingsFollowTheVersionAndTheViewer(t *testing.T) {
	ctx := context.Background()
	r := newSvc(t)
	h := &Handlers{Contests: r.svc}
	r.svc.StandingsEncodeOnce = true
	c := r.contest(t, nil)
	seedBoard(t, r, c, 5, 1)
	*r.now = at(30)

	a1, _ := h.standingsOut(ctx, c, VariantFull, 1000, true)
	a2, _ := h.standingsOut(ctx, c, VariantFull, 1001, true)
	b1, _ := a1.splice()
	b2, _ := a2.splice()
	var p1, p2 struct {
		Me int64 `json:"me"`
	}
	json.Unmarshal(b1, &p1)
	json.Unmarshal(b2, &p2)
	if p1.Me != 1000 || p2.Me != 1001 || bytes.Equal(b1, b2) {
		t.Fatalf("viewers got me=%d and me=%d", p1.Me, p2.Me)
	}

	// A new student submits; once the board is recomputed the bytes change.
	addStudent(t, r, 3000, "New")
	if _, err := r.svc.DB.Exec("INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, 3000, ?, ?)", c.ID, t0.UnixMilli(), t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	r.svc.board.bump(c.ID)
	r.svc.board.mu.Lock()
	r.svc.board.entries[boardKey{c.ID, VariantFull}].lastStart = r.svc.now().Add(-time.Hour)
	r.svc.board.mu.Unlock()
	n1, _ := h.standingsOut(ctx, c, VariantFull, 1000, true)
	nb, _ := n1.splice()
	if n1.Version == a1.Version || len(n1.Rows) != len(a1.Rows)+1 || !bytes.Equal(nb, today(t, n1)) || bytes.Contains(nb, []byte(`"New"`)) == false {
		t.Fatalf("version %d -> %d, rows %d -> %d, new student in body: %v", a1.Version, n1.Version, len(a1.Rows), len(n1.Rows), bytes.Contains(nb, []byte(`"New"`)))
	}
	if bytes.Contains(b1, []byte(`"New"`)) {
		t.Error("the old version's bytes changed")
	}
}

// Many requests at once, each as a different viewer, get their own bytes
// and the shared rows are never written to.
func TestSplicedStandingsUnderConcurrentRequests(t *testing.T) {
	ctx := context.Background()
	r := newSvc(t)
	h := &Handlers{Contests: r.svc}
	r.svc.StandingsEncodeOnce = true
	c := r.contest(t, nil)
	seedBoard(t, r, c, 40, 2)
	*r.now = at(30)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			me := int64(1000 + i%40)
			o, err := h.standingsOut(ctx, c, VariantFull, me, true)
			if err != nil {
				t.Error(err)
				return
			}
			got, ok := o.splice()
			if !ok || !bytes.Equal(got, today(t, o)) {
				t.Errorf("viewer %d: body differs from today's", me)
			}
		}()
	}
	wg.Wait()
}

// BenchmarkStandingsBody compares building a 700-student body for each
// request with splicing the encoded rows.
func BenchmarkStandingsBody(b *testing.B) {
	ctx := context.Background()
	r := newSvc(b)
	h := &Handlers{Contests: r.svc}
	c := r.contest(b, nil)
	seedBoard(b, r, c, 700, 1)
	*r.now = at(30)
	o, err := h.standingsOut(ctx, c, VariantFull, 1000, true)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode each request", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			json.NewEncoder(&buf).Encode(o)
		}
	})
	b.Run("splice", func(b *testing.B) {
		o.splice() // the first request encodes; the rest reuse it
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			o.splice()
		}
	})
	body, _ := o.splice()
	b.Logf("body is %d bytes for 700 students", len(body))
}

// The handler's response, headers and body, is the same with the setting on
// and off.
func TestWriteStandingsSameWithSettingOnAndOff(t *testing.T) {
	ctx := context.Background()
	r := newSvc(t)
	h := &Handlers{Contests: r.svc}
	c := r.contest(t, nil)
	seedBoard(t, r, c, 8, 2)
	*r.now = at(95)
	o, err := h.standingsOut(ctx, c, VariantFrozen, 1003, true)
	if err != nil {
		t.Fatal(err)
	}
	send := func(on bool) *httptest.ResponseRecorder {
		r.svc.StandingsEncodeOnce = on
		w := httptest.NewRecorder()
		h.writeStandings(w, o)
		return w
	}
	off, on := send(false), send(true)
	if off.Code != on.Code || !bytes.Equal(off.Body.Bytes(), on.Body.Bytes()) || !reflect.DeepEqual(off.Header(), on.Header()) {
		t.Errorf("setting off: %d %v\nsetting on: %d %v\nbodies equal: %v", off.Code, off.Header(), on.Code, on.Header(), bytes.Equal(off.Body.Bytes(), on.Body.Bytes()))
	}
	if off.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", off.Header().Get("Cache-Control"))
	}
}
