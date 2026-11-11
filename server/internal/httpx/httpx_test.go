package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]int{"n": 7})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["n"] != 7 {
		t.Errorf("body = %q, err %v", rec.Body.String(), err)
	}
}

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusForbidden, "forbidden", "Ask an admin.")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	want := `{"error":{"code":"forbidden","message":"Ask an admin."}}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct peer", "203.0.113.9:5555", "", "203.0.113.9"},
		{"direct peer ignores a forged header", "203.0.113.9:5555", "198.51.100.1", "203.0.113.9"},
		{"loopback without header", "127.0.0.1:5555", "", "127.0.0.1"},
		{"loopback uses the last forwarded address", "127.0.0.1:5555", "10.9.9.9, 198.51.100.2", "198.51.100.2"},
		{"loopback with a bad forwarded value", "127.0.0.1:5555", "not-an-ip", "127.0.0.1"},
		{"ipv6 loopback", "[::1]:5555", "198.51.100.3", "198.51.100.3"},
		{"no port", "203.0.113.9", "", "203.0.113.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r); got != c.want {
			t.Errorf("%s: ClientIP = %q, want %q", c.name, got, c.want)
		}
	}
}
