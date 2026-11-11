package submissions

import (
	"context"
	"errors"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
)

// V203-64: a draft for a user that does not exist breaks a foreign key; it
// is refused from the driver's error code, with the real SQLite driver.
func TestDraftForAMissingUserIsNotFound(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, nil)
	err := r.svc.SaveDraft(context.Background(), p.ID, 9999, Scope{}, "python3", "print(1)")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("a draft for user 9999: %v, want ErrNotFound", err)
	}
	if db.IsForeignKey(err) {
		t.Error("the foreign key error leaked out of SaveDraft")
	}
}
