package ai

import "testing"

func TestOneCallPerAdmin(t *testing.T) {
	var s Slots
	release, ok := s.Take(1)
	if !ok {
		t.Fatal("first call refused")
	}
	if _, ok := s.Take(1); ok {
		t.Error("second call for the same admin allowed")
	}
	other, ok := s.Take(2)
	if !ok {
		t.Error("another admin refused")
	}
	other()
	release()
	release() // a second release is harmless
	if r, ok := s.Take(1); !ok {
		t.Error("slot not given back")
	} else {
		r()
	}
}
