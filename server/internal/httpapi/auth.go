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

func (d Deps) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: d.sessionCookieName(), Value: token, Path: "/",
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: true, Secure: d.secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (d Deps) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1,
		HttpOnly: true, Secure: d.secure(), SameSite: http.SameSiteLaxMode})
}

func (d Deps) authConfig(w http.ResponseWriter, r *http.Request) {
	domains, err := d.Auth.AllowedDomains(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	names := make([]string, len(domains))
	for i, dm := range domains {
		names[i] = dm.Domain
	}
	WriteJSON(w, http.StatusOK, map[string]any{"allowed_domains": names, "contact_email": d.Auth.OwnerEmail})
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
	id, err := d.IdP.Exchange(r.Context(), q.Get("code"), f.Verifier, f.Nonce, time.Now())
	if err != nil {
		d.Log.Warn("sign-in: provider exchange failed", "err", err)
		fail("google")
		return
	}
	u, token, expires, err := d.Auth.SignIn(r.Context(), id, clientIP(r), r.UserAgent())
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
	}
	d.Log.Info("signed in", "user_id", u.ID, "role", u.Role)
	d.setSessionCookie(w, token, expires)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, auth.SafeNext(f.Next), http.StatusFound)
}

func (d Deps) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(d.sessionCookieName()); err == nil {
		if err := d.Auth.SignOut(r.Context(), c.Value); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	d.clearCookie(w, d.sessionCookieName(), "/")
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) me(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, userFrom(r.Context()))
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
		c, err := r.Cookie(d.sessionCookieName())
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "signed_out", "Sign in to continue.")
			return
		}
		u, renewed, err := d.Auth.Authenticate(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			d.clearCookie(w, d.sessionCookieName(), "/")
			WriteError(w, http.StatusUnauthorized, "signed_out", "Your session has ended. Sign in again.")
			return
		}
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		if !renewed.IsZero() {
			d.setSessionCookie(w, c.Value, renewed)
		}
		if !u.Role.AtLeast(min) {
			WriteError(w, http.StatusForbidden, "forbidden", "You do not have access to this. Ask an owner if you need it.")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
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
