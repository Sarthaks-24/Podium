package httpapi

// The server half of the "deploy during practice" drill (P0 B0.17): a
// deploy restarts the server while a student works. Sessions, drafts and
// the submission keys live in the database, so after the restart the
// student is still signed in, the draft is there, the event stream opens
// again, and a submit retried with the same client key (the browser
// retries one that failed during the restart) returns the first
// submission instead of counting twice. The browser half (the reconnect
// notice and its timing) is the drill on the VM.

import (
	"bufio"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

func TestRestartKeepsSessionDraftAndSubmitKey(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")
	code, created := owner.raw("POST", "/api/admin/problems/import", "application/zip", zipFolder(t, "../../../problems/a-plus-b"), s.srv.URL)
	if code != 201 {
		t.Fatalf("import %d %v", code, created)
	}
	pid := strconv.Itoa(int(created["id"].(float64)))
	draft := "print(sum(map(int, input().split())))"
	if code, _ := owner.do("PUT", "/api/workspace/"+pid+"/draft", map[string]string{"language": "python3", "source": draft}, s.srv.URL); code != 204 {
		t.Fatalf("draft %d", code)
	}
	body := map[string]any{"language": "python3", "source": draft, "client_key": "pressed-during-restart"}
	code, first := owner.do("POST", "/api/workspace/"+pid+"/submit", body, s.srv.URL)
	if code != 202 {
		t.Fatalf("submit %d %v", code, first)
	}

	s.restart()

	if code, me := owner.do("GET", "/api/me", nil, ""); code != 200 {
		t.Fatalf("after the restart the session is gone: %d %v", code, me)
	}
	code, ws := owner.do("GET", "/api/workspace/"+pid, nil, "")
	if code != 200 {
		t.Fatalf("workspace after the restart %d %v", code, ws)
	}
	d, _ := ws["draft"].(map[string]any)
	if d == nil || d["source"] != draft {
		t.Errorf("draft after the restart: %v", ws["draft"])
	}
	// The browser's retry of the submit it could not confirm.
	code, again := owner.do("POST", "/api/workspace/"+pid+"/submit", body, s.srv.URL)
	if code != 200 || again["id"] != first["id"] {
		t.Errorf("retried submit after the restart: %d %v, want 200 and id %v", code, again, first["id"])
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM submissions WHERE kind = 'submit'`).Scan(&n)
	if n != 1 {
		t.Errorf("%d submissions after the retry, want 1", n)
	}

	// The event stream reconnects and starts with a clock.
	req, _ := http.NewRequest("GET", s.srv.URL+"/api/events", nil)
	stream, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatalf("event stream after the restart: %d", stream.StatusCode)
	}
	sc := bufio.NewScanner(stream.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "event: ") {
			if ev := strings.TrimPrefix(line, "event: "); ev != "clock" {
				t.Errorf("first event after the restart %q, want clock", ev)
			}
			break
		}
	}
}
