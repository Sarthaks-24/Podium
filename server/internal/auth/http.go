package auth

// The HTTP side of identity, apart from sign-in and sessions, which stay
// with the session middleware in httpapi: the signed-in user on the
// request context, the user's own preferences, and the owner's People
// page (organisers, allowed domains, account actions, the organisation's
// details and the site audit). Shapes are in docs/API.md, "Settings:
// people, sign-in and organisation".

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/httpx"
)

type userKey struct{}

// WithUser returns ctx carrying the signed-in user. httpapi's role check
// sets it before any handler runs.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFrom returns the signed-in user WithUser stored, or the zero User.
func UserFrom(ctx context.Context) User {
	u, _ := ctx.Value(userKey{}).(User)
	return u
}

// DomainOf is the part of an email after the @, which logs name instead of
// the address.
func DomainOf(email string) string {
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		return email[i+1:]
	}
	return ""
}

// Handlers serve the identity routes httpapi mounts.
type Handlers struct {
	Auth *Service
	Log  *slog.Logger
	// Now is the server's clock, for server_now.
	Now func() time.Time
}

// WriteChangeError maps a refused change to its status code; anything
// else is a logged 500.
func WriteChangeError(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	var ce *ChangeError
	if !errors.As(err, &ce) {
		httpx.InternalError(log, w, r, err)
		return
	}
	switch ce.Kind {
	case "invalid":
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", ce.Message)
	case "not_found":
		httpx.WriteError(w, http.StatusNotFound, "not_found", ce.Message)
	case "forbidden":
		httpx.WriteError(w, http.StatusForbidden, "not_allowed", ce.Message)
	default:
		httpx.WriteError(w, http.StatusConflict, "conflict", ce.Message)
	}
}

func (d *Handlers) changeError(w http.ResponseWriter, r *http.Request, err error) {
	WriteChangeError(d.Log, w, r, err)
}

func (d *Handlers) internalError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.InternalError(d.Log, w, r, err)
}

// Me is GET /api/me: the signed-in user.
func (d *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, UserFrom(r.Context()))
}

// MyProfile answers GET /api/me/profile: the signed-in person's own dates (v4 M1).
func (d *Handlers) MyProfile(w http.ResponseWriter, r *http.Request) {
	p, err := d.Auth.Profile(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (d *Handlers) SetPreferences(w http.ResponseWriter, r *http.Request) {
	var p Preferences
	if !httpx.DecodeJSON(w, r, &p) {
		return
	}
	if err := d.Auth.SetPreferences(r.Context(), UserFrom(r.Context()).ID, p); err != nil {
		d.changeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (d *Handlers) ListPeople(w http.ResponseWriter, r *http.Request) {
	people, err := d.Auth.Organisers(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	list, counts, refused, err := d.Auth.List(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	disabled, err := d.Auth.DisabledAccounts(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	org, err := d.Auth.Organisation(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	open, err := d.Auth.SignInOpen(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"people": people, "list": list, "signin_open": open, "counts": counts, "refused": refused,
		"disabled": disabled, "organisation": org})
}

// LookupAccount finds one account by its email, students included, so
// People's Account help can say whose account an action is about before
// it runs (audit 2026-10-20 O10). Owners and admins only: anyone else gets
// the same 403 whether or not the address exists, so the route tells
// nobody else which addresses have accounts. The address is matched as
// sign-in stores it (trimmed, lower case) and is never logged.
func (d *Handlers) LookupAccount(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" || len(email) > 320 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Give an email address as ?email=.")
		return
	}
	a, err := d.Auth.Account(r.Context(), email)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"account": a, "server_now": d.Now().UnixMilli()})
}

// EndSessions signs a person out everywhere, for a lost laptop or a shared
// session. Their open streams close at the next clock tick.
func (d *Handlers) EndSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	a, n, err := d.Auth.EndSessions(r.Context(), req.Email)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("sessions ended", "by", UserFrom(r.Context()).ID, "user_id", a.ID, "count", n)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"account": a, "ended": n})
}

// SetAccountDisabled disables or re-enables an account. A disabled account
// is refused at sign-in and at every signed-in route.
func (d *Handlers) SetAccountDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		a, n, err := d.Auth.SetDisabled(r.Context(), req.Email, disabled, UserFrom(r.Context()).ID)
		if err != nil {
			d.changeError(w, r, err)
			return
		}
		d.Log.Info("account disabled or enabled", "by", UserFrom(r.Context()).ID, "user_id", a.ID, "disabled", disabled, "sessions_ended", n)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"account": a, "ended": n})
	}
}

func (d *Handlers) GrantRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Role  Role   `json:"role"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	p, err := d.Auth.GrantRole(r.Context(), req.Email, req.Role)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("role granted", "by", UserFrom(r.Context()).ID, "user_id", p.ID, "role", p.Role)
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (d *Handlers) RevokeRole(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ParseID(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "That person is not an organiser.")
		return
	}
	if err := d.Auth.RevokeRole(r.Context(), id); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("role revoked", "by", UserFrom(r.Context()).ID, "user_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Handlers) ResetGoogleAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := d.Auth.ResetGoogleAccount(r.Context(), req.Email); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("google account link reset", "by", UserFrom(r.Context()).ID, "domain", DomainOf(req.Email))
	w.WriteHeader(http.StatusNoContent)
}

// ImportPeople adds people to the list from pasted text. With dry_run it
// only says what would happen, so the page can show the counts first.
func (d *Handlers) ImportPeople(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text   string `json:"text"`
		DryRun bool   `json:"dry_run"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := d.Auth.Import(r.Context(), req.Text, req.DryRun)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	if !req.DryRun && len(res.Added) > 0 {
		d.Log.Info("people added to the list", "by", UserFrom(r.Context()).ID, "count", len(res.Added))
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// RemovePerson takes someone off the list: deleted when they never signed
// in, otherwise disabled and signed out.
func (d *Handlers) RemovePerson(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	deleted, err := d.Auth.Remove(r.Context(), req.Email)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("person removed from the list", "by", UserFrom(r.Context()).ID, "deleted", deleted, "domain", DomainOf(req.Email))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// SetSignIn opens or closes sign-in for everyone below admin.
func (d *Handlers) SetSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Open *bool `json:"open"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Open == nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Send open as true or false.")
		return
	}
	if err := d.Auth.SetSignInOpen(r.Context(), *req.Open); err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("sign-in switch set", "by", UserFrom(r.Context()).ID, "open", *req.Open)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"open": *req.Open})
}

// LetIn adds a refused address to the list.
func (d *Handlers) LetIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := d.Auth.LetIn(r.Context(), req.Email); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("refused address let in", "by", UserFrom(r.Context()).ID, "domain", DomainOf(req.Email))
	w.WriteHeader(http.StatusNoContent)
}

// SetSiteSettings saves the organisation's four fields at once: name,
// contact email, phone and privacy contact, shown in the footer and on the
// sign-in page. A refused field says what to fix under itself.
func (d *Handlers) SetSiteSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           *string `json:"name"`
		ContactEmail   *string `json:"contact_email"`
		Phone          *string `json:"phone"`
		PrivacyContact *string `json:"privacy_contact"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil || req.ContactEmail == nil || req.Phone == nil || req.PrivacyContact == nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Send all four fields: name, contact_email, phone and privacy_contact.")
		return
	}
	o, err := d.Auth.SetOrganisation(r.Context(), Organisation{Name: *req.Name, ContactEmail: *req.ContactEmail,
		Phone: *req.Phone, PrivacyContact: *req.PrivacyContact})
	var fields FieldErrors
	switch {
	case errors.As(err, &fields):
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]httpx.APIError{"error": {Code: "bad_request", Message: fields.Error(), Fields: fields}})
	case err != nil:
		d.internalError(w, r, err)
	default:
		d.Log.Info("site settings changed", "by", UserFrom(r.Context()).ID)
		httpx.WriteJSON(w, http.StatusOK, o)
	}
}

// SiteAudit is the People page's History, owners only.
func (d *Handlers) SiteAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	until, _ := strconv.ParseInt(q.Get("until"), 10, 64)
	list, more, err := d.Auth.SiteAudit(r.Context(), SiteAuditFilter{Action: q.Get("action"), Q: q.Get("q"), Since: since, Until: until, Page: page})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": list, "page": max(page, 1), "has_more": more, "server_now": time.Now().UnixMilli()})
}
