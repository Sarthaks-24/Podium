package httpapi

// The site audit (v2 plan B2.7, D62): every people change writes one row
// naming its actor, the People History feed filters them, and no row of
// the site or contest audit ever holds an AI key, a session token, a
// cookie value or an OAuth code, whichever audited path they travel
// through (the gate's leak test).

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
)

// Fake secrets that look like real ones.
const (
	fakeAIKey     = "sk-ant-api03-LEAKTEST-0123456789abcdefghijklmnopqrstuvwxyzABCDEF-leak"
	fakeCookie    = "leakcookie-9f8e7d6c5b4a39281706f5e4d3c2b1a0"
	fakeOAuthCode = "4/0AeaYSHleaktestoauthcode1234567890"
	fakeBearer    = "ya29.a0leaktestbearertoken0987654321"
)

// leakyDo sends a request carrying every fake secret: an extra cookie, an
// Authorization header, the OAuth code and the key in the query and in
// headers a proxy or a curious client might add.
func leakyDo(t *testing.T, b *browser, method, path string, body any) (int, map[string]any) {
	t.Helper()
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	path += sep + "code=" + url.QueryEscape(fakeOAuthCode) + "&key=" + url.QueryEscape(fakeAIKey)
	u, _ := url.Parse(b.s.srv.URL)
	b.client.Jar.SetCookies(u, []*http.Cookie{{Name: "debug", Value: fakeCookie}})
	orig := b.client.Transport
	b.client.Transport = headerTransport{base: http.DefaultTransport, headers: map[string]string{
		"X-Api-Key": fakeAIKey, "X-Debug-Token": fakeBearer, "X-Oauth-Code": fakeOAuthCode}}
	defer func() { b.client.Transport = orig }()
	return b.do(method, path, body, b.s.srv.URL)
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}
	return h.base.RoundTrip(r)
}

func sessionCookie(b *browser) string {
	u, _ := url.Parse(b.s.srv.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if strings.Contains(c.Name, "podium_session") {
			return c.Value
		}
	}
	return ""
}

func TestSiteAuditRecordsPeopleChangesAndNoSecret(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	token := sessionCookie(owner)
	if token == "" {
		t.Fatal("no session cookie")
	}
	// The owner's AI key is held in memory for the session, as the key
	// form leaves it.
	s.ai.Keys.Put(token, ai.KeyEntry{Provider: "anthropic", Key: ai.NewSecret(fakeAIKey)})
	student := s.signedIn("asha@college.edu", "Asha")
	studentToken := sessionCookie(student)
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	cid := s.createContest(owner, "week-1", start, map[string]int64{"A": pid}, nil)

	steps := []struct {
		method, path string
		body         any
		want         int
	}{
		{"POST", "/api/admin/people", map[string]string{"email": "ta@college.edu", "role": "organiser"}, 200},
		{"POST", "/api/admin/people", map[string]string{"email": "ta@college.edu", "role": "owner"}, 200},
		{"POST", "/api/admin/people/import", map[string]any{"text": "new1@school.edu, new2@school.edu"}, 200},
		{"POST", "/api/admin/people/remove", map[string]string{"email": "new1@school.edu"}, 200},
		{"POST", "/api/admin/accounts/end-sessions", map[string]string{"email": "asha@college.edu"}, 200},
		{"POST", "/api/admin/accounts/disable", map[string]string{"email": "asha@college.edu"}, 200},
		{"POST", "/api/admin/accounts/enable", map[string]string{"email": "asha@college.edu"}, 200},
		{"POST", "/api/admin/accounts/reset-google", map[string]string{"email": "asha@college.edu"}, 204},
		{"PUT", fmt.Sprintf("/api/admin/contests/%d", cid), map[string]any{"title": "Week one"}, 200},
	}
	for _, st := range steps {
		if code, out := leakyDo(t, owner, st.method, st.path, st.body); code != st.want {
			t.Fatalf("%s %s: %d %v", st.method, st.path, code, out)
		}
	}
	var taID int64
	s.db.QueryRow("SELECT id FROM users WHERE email = 'ta@college.edu'").Scan(&taID)
	if code, _ := leakyDo(t, owner, "DELETE", fmt.Sprintf("/api/admin/people/%d", taID), nil); code != 204 {
		t.Fatal("revoke")
	}
	// The server's own change: a new run mode.
	if _, err := s.authSvc.RecordMode(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	// A change of mode ends every session.
	owner = s.signedIn("owner@college.edu", "Owner")

	// History, newest first, filterable; actors as they were.
	code, hist := owner.do("GET", "/api/admin/site-audit", nil, "")
	if code != 200 {
		t.Fatalf("site audit %d %v", code, hist)
	}
	var actions []string
	for _, e := range hist["entries"].([]any) {
		actions = append(actions, e.(map[string]any)["action"].(string))
	}
	want := "mode_changed role_revoked google_reset account_enabled account_disabled sessions_ended person_removed list_imported role_granted role_granted"
	if got := strings.Join(actions, " "); !strings.HasPrefix(got, want) {
		t.Errorf("actions %s\nwant start %s", got, want)
	}
	first := hist["entries"].([]any)[len(actions)-1].(map[string]any)
	if first["actor"] == nil && actions[len(actions)-1] != "mode_changed" {
		t.Errorf("oldest row has no actor: %v", first)
	}
	_, f := owner.do("GET", "/api/admin/site-audit?action=role_granted&q=TA@", nil, "")
	if n := len(f["entries"].([]any)); n != 2 {
		t.Errorf("filtered history has %d rows, want 2", n)
	}
	// The leak test: no secret anywhere in either audit table.
	secrets := []string{fakeAIKey, fakeCookie, fakeOAuthCode, fakeBearer, token, studentToken}
	for _, table := range []string{"site_audit", "contest_audit"} {
		rows, err := s.db.Query("SELECT * FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		n := 0
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			line := fmt.Sprint(vals...)
			for _, sec := range secrets {
				if sec != "" && strings.Contains(line, sec) {
					t.Errorf("%s holds a secret: %s", table, line)
				}
			}
			n++
		}
		rows.Close()
		if n == 0 {
			t.Errorf("%s is empty", table)
		}
	}
	// And not in the database file at all (the WAL included).
	for _, f := range []string{s.dbPath, s.dbPath + "-wal"} {
		b, _ := os.ReadFile(f)
		for _, sec := range []string{fakeAIKey, fakeBearer, fakeCookie, fakeOAuthCode, token} {
			if strings.Contains(string(b), sec) {
				t.Errorf("%s holds a secret", f)
			}
		}
	}
}
