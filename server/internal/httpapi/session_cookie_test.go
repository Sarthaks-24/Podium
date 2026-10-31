package httpapi

import (
	"slices"
	"testing"

	"github.com/Sarthaks-24/podium/internal/webui"
)

// The landing page split (v4 P2) checks the session cookie by name in
// webui, which cannot import httpapi: both names must be the ones set here.
func TestSessionCookieNames(t *testing.T) {
	for _, base := range []string{"https://podium.example", "http://localhost:8080"} {
		if name := (Deps{BaseURL: base}).sessionCookieName(); !slices.Contains(webui.SessionCookies, name) {
			t.Errorf("%s: cookie %q missing from webui.SessionCookies %v", base, name, webui.SessionCookies)
		}
	}
}
