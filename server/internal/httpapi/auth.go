package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/retention"
)

// IdentityProvider is the sign-in provider; *auth.Provider implements it.
type IdentityProvider interface {
	AuthCodeURL(state, nonce, verifier, hd string) string
	Exchange(ctx context.Context, code, verifier, nonce string, now time.Time) (auth.Identity, error)
}

// sessionCookieName is the session cookie's name. With https the __Host-
// prefix makes the browser refuse it unless it is Secure, host-only and
// scoped to the whole site.
func (d Deps) sessionCookieName() string {
	if d.secure() {
		return "__Host-podium_session"
	}
	return "podium_session"
}

// flowCookie names the sign-in state cookie and its path. On https it is
// __Host- prefixed (so Path must be "/"): a sibling subdomain or a plain
// http response cannot plant one, which would let an attacker finish their
// own sign-in in a victim's browser (login CSRF).
func (d Deps) flowCookie() (name, path string) {
	if d.secure() {
		return "__Host-podium_signin", "/"
	}
	return "podium_signin", "/api/auth/"
}

func (d Deps) secure() bool { return strings.HasPrefix(d.BaseURL, "https://") }

// setSessionCookie sets a browser-session cookie: no Expires or Max-Age,
// so closing the browser forgets it (a shared lab PC, security council
// S5). The server ends the session itself auth.SessionTTL after sign-in.
func (d Deps) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: d.sessionCookieName(), Value: token, Path: "/",
		HttpOnly: true, Secure: d.secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (d Deps) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1,
		HttpOnly: true, Secure: d.secure(), SameSite: http.SameSiteLaxMode})
}

// forwardedHeaders are set by reverse proxies. In testing mode any of them
// means the request did not come from this machine's own browser: Caddy
// (or any other proxy) in front of a testing-mode server must never work.
var forwardedHeaders = []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip", "X-Forwarded-Host"}

// isLocalHost reports whether a Host header names this machine:
// localhost, 127.0.0.1 or [::1], with any port.
func isLocalHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

// localOnly refuses, in testing mode, a request whose Host is not this
// machine or that carries a proxy's forwarding header (security council
// S2). The configuration check already keeps testing mode on loopback;
// this stops a proxy on the same host (Caddy on the real VM) from
// publishing it. Outside testing mode it lets everything through. It
// answers the refusal itself and returns false.
func (d Deps) localOnly(w http.ResponseWriter, r *http.Request) bool {
	if d.Mode != "testing" {
		return true
	}
	ok := isLocalHost(r.Host)
	for _, h := range forwardedHeaders {
		if r.Header.Get(h) != "" {
			ok = false
		}
	}
	if !ok {
		WriteError(w, http.StatusForbidden, "testing_mode_local", "This server is in testing mode, which works only from a browser on the same machine.")
	}
	return ok
}

// localOnlyAll is localOnly as middleware, so New can apply it to every
// route, the web app included.
func (d Deps) localOnlyAll(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.localOnly(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func (d Deps) authConfig(w http.ResponseWriter, r *http.Request) {
	if !d.localOnly(w, r) {
		return
	}
	domains, err := d.Auth.AllowedDomains(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	names := make([]string, len(domains))
	for i, dm := range domains {
		names[i] = dm.Domain
	}
	mode := d.Mode
	if mode == "" {
		mode = "production"
	}
	org, err := d.Auth.Organisation(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	// The organisation's own address replaces the owner's personal one as
	// soon as an owner enters it (pre-auth review, observation 1).
	contactEmail := org.ContactEmail
	if contactEmail == "" {
		contactEmail = d.Auth.OwnerEmail
	}
	WriteJSON(w, http.StatusOK, map[string]any{"allowed_domains": names, "contact_email": contactEmail, "mode": mode,
		"organisation": org, "retention": retention.Periods()})
}

// testSignIn signs in as anyone with any role, for testing mode only (the
// route is not registered otherwise). Beyond the local-only check every
// testing-mode route gets, it needs an Origin header equal to the base URL:
// the page's own form sends one, a script on another machine has no
// reason to.
func (d Deps) testSignIn(w http.ResponseWriter, r *http.Request) {
	if !d.localOnly(w, r) {
		return
	}
	if r.Header.Get("Origin") != d.BaseURL {
		WriteError(w, http.StatusForbidden, "cross_site", "Test sign-in works only from the sign-in page at "+d.BaseURL+".")
		return
	}
	var req struct {
		Email string    `json:"email"`
		Name  string    `json:"name"`
		Role  auth.Role `json:"role"`
		Next  string    `json:"next"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u, token, _, err := d.Auth.TestSignIn(r.Context(), req.Email, req.Name, req.Role, clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrDisabled) {
		WriteError(w, http.StatusForbidden, "disabled", "This account is disabled. An owner can enable it on the People page.")
		return
	}
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	// As with Google, a new sign-in ends this browser's previous session.
	if old, err := r.Cookie(d.sessionCookieName()); err == nil {
		d.Auth.SignOut(r.Context(), old.Value)
		d.forgetKey(old.Value)
	}
	d.setSessionCookie(w, token)
	d.Log.Info("test sign-in", "user_id", u.ID, "role", u.Role)
	WriteJSON(w, http.StatusOK, map[string]string{"next": auth.SafeNext(req.Next)})
}

func (d Deps) googleStart(w http.ResponseWriter, r *http.Request) {
	f := auth.NewFlow(r.URL.Query().Get("next"), time.Now())
	sealed, err := auth.SealFlow(d.SessionSecret, f)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	hd := ""
	if domains, err := d.Auth.AllowedDomains(r.Context()); err == nil && len(domains) == 1 {
		hd = domains[0].Domain
	}
	// SameSite=Lax: the cookie must come back on Google's top-level
	// redirect to the callback.
	name, path := d.flowCookie()
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: sealed, Path: path, MaxAge: int(auth.FlowTTL.Seconds()),
		HttpOnly: true, Secure: d.secure(), SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, d.IdP.AuthCodeURL(f.State, f.Nonce, f.Verifier, hd), http.StatusFound)
}

func (d Deps) googleCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(code string) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/signin?error="+code, http.StatusFound)
	}
	name, path := d.flowCookie()
	c, err := r.Cookie(name)
	d.clearCookie(w, name, path)
	if err != nil {
		fail("expired")
		return
	}
	f, err := auth.OpenFlow(d.SessionSecret, c.Value, time.Now())
	q := r.URL.Query()
	if err != nil || q.Get("state") != f.State {
		fail("expired")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		// The person cancelled at Google, or Google refused.
		fail("google")
		return
	}
	// Each attempt finishes once: a replayed callback costs no call to
	// Google (P0 security review, finding 4).
	if fresh, recorded := d.flows.take(f.State, f.Expires, time.Now().Unix()); !fresh {
		fail("expired")
		return
	} else if !recorded {
		d.Log.Warn("sign-in: too many attempts in flight to remember this one")
	}
	id, err := d.IdP.Exchange(r.Context(), q.Get("code"), f.Verifier, f.Nonce, time.Now())
	if err != nil {
		d.Log.Warn("sign-in: provider exchange failed", "err", err)
		fail("google")
		return
	}
	u, token, _, err := d.Auth.SignIn(r.Context(), id, clientIP(r), r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrDomain):
		d.Log.Info("sign-in refused: domain not allowed", "domain", domainOf(id.Email))
		fail("domain")
		return
	case errors.Is(err, auth.ErrUnverified):
		fail("unverified")
		return
	case errors.Is(err, auth.ErrDisabled):
		d.Log.Info("sign-in refused: account disabled", "domain", domainOf(id.Email))
		fail("disabled")
		return
	case errors.Is(err, auth.ErrAccount):
		d.Log.Warn("sign-in refused: email bound to a different Google account", "domain", domainOf(id.Email))
		fail("account")
		return
	case err != nil:
		// A browser navigation: send the person somewhere readable.
		d.Log.Error("sign-in failed", "err", err)
		fail("server")
		return
	}
	// Signing in replaces any session this browser already had.
	if old, err := r.Cookie(d.sessionCookieName()); err == nil {
		d.Auth.SignOut(r.Context(), old.Value)
		d.forgetKey(old.Value)
	}
	d.Log.Info("signed in", "user_id", u.ID, "role", u.Role)
	d.setSessionCookie(w, token)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, auth.SafeNext(f.Next), http.StatusFound)
}

func (d Deps) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(d.sessionCookieName()); err == nil {
		// The AI key goes first: it must not outlive the session even if
		// the database write fails.
		d.forgetKey(c.Value)
		if err := d.Auth.SignOut(r.Context(), c.Value); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	d.clearCookie(w, d.sessionCookieName(), "/")
	w.WriteHeader(http.StatusNoContent)
}

// meJSON is GET /api/me: the user, and when the site was last updated by
// an emergency deploy (D112), for the student banner.
type meJSON struct {
	auth.User
	SiteUpdatedAt *int64 `json:"site_updated_at"`
	// NotificationsUnread is 0 while the person is in a live contest
	// (C9: notices are held).
	NotificationsUnread int `json:"notifications_unread"`
}

func (d Deps) me(w http.ResponseWriter, r *http.Request) {
	out := meJSON{User: userFrom(r.Context())}
	at, ok, err := d.Auth.LastForcedDeploy(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if ok {
		out.SiteUpdatedAt = millis(at)
	}
	if n := d.notices(); n != nil {
		if out.NotificationsUnread, err = n.Unread(r.Context(), out.ID); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d Deps) setPreferences(w http.ResponseWriter, r *http.Request) {
	var p auth.Preferences
	if !decodeJSON(w, r, &p) {
		return
	}
	if err := d.Auth.SetPreferences(r.Context(), userFrom(r.Context()).ID, p); err != nil {
		d.changeError(w, r, err)
		return
	}
	if p.HideProgress == nil {
		hide := userFrom(r.Context()).HideProgress
		p.HideProgress = &hide
	}
	WriteJSON(w, http.StatusOK, p)
}

type userKey struct{}

func userFrom(ctx context.Context) auth.User {
	u, _ := ctx.Value(userKey{}).(auth.User)
	return u
}

// requireRole wraps h so it runs only for a signed-in user holding at least
// min. The role comes from the database on every request.
func (d Deps) requireRole(min auth.Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.localOnly(w, r) {
			return
		}
		c, err := r.Cookie(d.sessionCookieName())
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "signed_out", "Sign in to continue.")
			return
		}
		// Unknown cookies cost a database read each, so an address that
		// sent too many waits before the next read (security audit L3).
		addr := addressKey(r)
		if !loopbackRequest(r) && d.authFailLimit.exhausted(addr) {
			w.Header().Set("Retry-After", "5")
			WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests from your network. Wait a few seconds and try again.")
			return
		}
		u, _, err := d.Auth.Authenticate(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			if !loopbackRequest(r) {
				d.authFailLimit.allow(addr)
			}
			d.clearCookie(w, d.sessionCookieName(), "/")
			WriteError(w, http.StatusUnauthorized, "signed_out", "Your session has ended. Sign in again.")
			return
		}
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		if !u.Role.AtLeast(min) {
			WriteError(w, http.StatusForbidden, "forbidden", "You do not have access to this. Ask an owner if you need it.")
			return
		}
		// The actor rides on the context so every change the request makes
		// names them in the site audit (B2.7).
		ctx := auth.WithActor(context.WithValue(r.Context(), userKey{}, u), auth.Actor{ID: u.ID, Email: u.Email, Role: u.Role})
		h(w, r.WithContext(ctx))
	}
}

// sameOrigin refuses state-changing API requests from other sites. SameSite
// cookies already stop most of them; this covers older browsers and
// same-site subdomains.
func (d Deps) sameOrigin(next http.Handler) http.Handler {
	want := d.BaseURL
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if strings.HasPrefix(r.URL.Path, "/api/") {
				origin := r.Header.Get("Origin")
				site := r.Header.Get("Sec-Fetch-Site")
				if (origin != "" && origin != want) || (origin == "" && site != "" && site != "same-origin") {
					WriteError(w, http.StatusForbidden, "cross_site", "This request came from another site and was refused.")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the address recorded with a session. Behind Caddy on the
// same host the peer is loopback, so the forwarded address is used then.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			if p := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); p != nil {
				return p.String()
			}
		}
	}
	return host
}

func domainOf(email string) string {
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		return email[i+1:]
	}
	return ""
}

// redirectURL is the callback registered with Google.
func RedirectURL(baseURL string) string {
	u, _ := url.Parse(baseURL)
	u.Path = strings.TrimRight(u.Path, "/") + "/api/auth/google/callback"
	return u.String()
}

func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}
