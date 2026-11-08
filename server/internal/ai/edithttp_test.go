package ai

import (
	"strings"
	"testing"
	"time"
)

func draftOf(user, problem int64, size int) *editDraft {
	return &editDraft{id: strings.Repeat("a", 5) + time.Now().Format("150405.000000000"), userID: user, problemID: problem,
		files: map[string][]byte{"statement.md": make([]byte, size)}, created: time.Now()}
}

func TestDraftsPerUserCapEvictsTheOldest(t *testing.T) {
	var e editDrafts
	var first *editDraft
	for i := 0; i < maxDraftsPerUser+2; i++ {
		d := draftOf(1, int64(i+1), 10)
		d.id = string(rune('a' + i))
		d.created = time.Now().Add(time.Duration(i) * time.Second)
		if i == 0 {
			first = d
		}
		if err := e.put(d); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := e.get(first.id, 1, first.problemID); ok {
		t.Error("the oldest draft was kept")
	}
	if n := len(e.m); n != maxDraftsPerUser {
		t.Errorf("%d drafts kept", n)
	}
}

func TestDraftsTotalBytesCapRefuses(t *testing.T) {
	var e editDrafts
	per := maxDraftBytes/3 + 1
	for i := 0; i < 2; i++ {
		d := draftOf(int64(i+1), 1, per)
		d.id = string(rune('a' + i))
		if err := e.put(d); err != nil {
			t.Fatal(err)
		}
	}
	d := draftOf(3, 1, per)
	d.id = "z"
	if err := e.put(d); err != errDraftsFull {
		t.Errorf("third draft: %v", err)
	}
}

func TestDraftsExpireOnGet(t *testing.T) {
	var e editDrafts
	d := draftOf(1, 1, 10)
	d.id = "old"
	d.created = time.Now().Add(-2 * editKeep)
	e.m = map[string]*editDraft{d.id: d}
	if _, ok := e.get("old", 1, 1); ok {
		t.Error("an expired draft was returned")
	}
	if len(e.m) != 0 {
		t.Error("an expired draft was not swept")
	}
}

func TestDraftIsSingleUse(t *testing.T) {
	var e editDrafts
	d := draftOf(1, 1, 10)
	d.id = "x"
	e.put(d)
	if _, ok := e.begin("x", 1, 1); !ok {
		t.Fatal("first begin refused")
	}
	if _, ok := e.begin("x", 1, 1); ok {
		t.Error("a draft being accepted was handed out twice")
	}
	e.release("x")
	if _, ok := e.begin("x", 1, 1); !ok {
		t.Error("a released draft was refused")
	}
}
