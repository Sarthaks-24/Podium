package notifications

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
)

// Every template, in every variant: it says what happened and what to do,
// asking notices carry the reassurance, and no sentence names a person,
// code, evidence or a test, or uses the words the plan rules out.
func TestNoticeTextCarriesNoCodeTestDataOrNames(t *testing.T) {
	forbidden := []string{"flag", "trust", "suspicious", "cheat", "test", "evidence", "source", "code", "output", "input", "rating",
		"score", "@", "Asha", "Ravi", "\u2014", "\u2013", "!"}
	for _, kind := range Kinds {
		for _, outcome := range []string{"", "warning", "zero_problem", "referred"} {
			for _, contest := range []string{"", "Week 6"} {
				for _, label := range []string{"", "B"} {
					for _, upheld := range []bool{false, true} {
						for _, adj := range []bool{false, true} {
							h, todo, re, link := Sentences(kind, contest, label, outcome, upheld, adj)
							if h == "" || todo == "" || link == "" {
								t.Fatalf("%s: empty sentence: %q %q %q", kind, h, todo, link)
							}
							if (kind == CaseShared || kind == Reminder) != (re == Reassurance) {
								t.Errorf("%s: reassurance %q", kind, re)
							}
							for _, s := range []string{h, todo, re} {
								words := " " + strings.ToLower(strings.NewReplacer(".", " ", ",", " ", ":", " ").Replace(s)) + " "
								for _, bad := range forbidden {
									b := strings.ToLower(bad)
									if len(b) > 1 {
										b = " " + b // a word, or the start of one ("tests")
									}
									if strings.Contains(words, b) {
										t.Errorf("%s/%s: %q contains %q", kind, outcome, s, bad)
									}
								}
								if s != "" && !strings.HasSuffix(s, ".") {
									t.Errorf("%s: %q does not end with a full stop", kind, s)
								}
							}
						}
					}
				}
			}
		}
	}
}

// newInbox is a migrated database with two students and a fixed clock.
func newInbox(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'asha@college.edu', 'student', 1), (2, 'ravi@college.edu', 'student', 1)`); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000)
	return &Service{DB: d, Now: func() time.Time { return now }}, d
}

// MarkRead with another person's ids changes nothing, and List pages by
// id with HasMore (C1-022).
func TestMarkReadForeignIdsAndPaging(t *testing.T) {
	s, d := newInbox(t)
	ctx := context.Background()
	at := s.now().UnixMilli() - 1000
	for i := 0; i < PageSize+3; i++ {
		if _, err := d.Exec(`INSERT INTO notifications (user_id, kind, created_at, available_at) VALUES (1, 'results_final', ?, ?)`, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`INSERT INTO notifications (id, user_id, kind, created_at, available_at) VALUES (1000, 2, 'results_final', ?, ?)`, at, at); err != nil {
		t.Fatal(err)
	}
	// Asha names Ravi's notice: it stays unread.
	if err := s.MarkRead(ctx, 1, []int64{1000}, false); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, 2); n != 1 {
		t.Errorf("Ravi's unread after Asha marked his id: %d", n)
	}
	first, err := s.List(ctx, 1, 0)
	if err != nil || len(first.Notices) != PageSize || !first.HasMore || first.Unread != PageSize+3 {
		t.Fatalf("first page: %d notices, more %v, unread %d, %v", len(first.Notices), first.HasMore, first.Unread, err)
	}
	last := first.Notices[len(first.Notices)-1].ID
	second, err := s.List(ctx, 1, last)
	if err != nil || len(second.Notices) != 3 || second.HasMore {
		t.Fatalf("second page: %d notices, more %v, %v", len(second.Notices), second.HasMore, err)
	}
	for _, n := range second.Notices {
		if n.ID >= last {
			t.Errorf("second page repeats id %d", n.ID)
		}
	}
	// All of Asha's go read; Ravi's stays.
	if err := s.MarkRead(ctx, 1, nil, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, 1); n != 0 {
		t.Errorf("Asha unread after all: %d", n)
	}
	if n, _ := s.Unread(ctx, 2); n != 1 {
		t.Errorf("Ravi unread after Asha read all: %d", n)
	}
}
