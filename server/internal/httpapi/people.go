package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// maxBody bounds every JSON request body.
const maxBody = 64 << 10

// decodeJSON reads a JSON body into v and writes a 400 on failure. It
// requires Content-Type application/json, which a plain cross-site HTML
// form cannot send.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, "bad_request", "Send the request body as application/json.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		WriteError(w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON for this action.")
		return false
	}
	return true
}

// changeError maps a refused change to its status code.
func (d Deps) changeError(w http.ResponseWriter, r *http.Request, err error) {
	var ce *auth.ChangeError
	if !errors.As(err, &ce) {
		d.internalError(w, r, err)
		return
	}
	switch ce.Kind {
	case "invalid":
		WriteError(w, http.StatusBadRequest, "bad_request", ce.Message)
	case "not_found":
		WriteError(w, http.StatusNotFound, "not_found", ce.Message)
	default:
		WriteError(w, http.StatusConflict, "conflict", ce.Message)
	}
}

func (d Deps) listPeople(w http.ResponseWriter, r *http.Request) {
	people, err := d.Auth.Organisers(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	domains, err := d.Auth.AllowedDomains(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"people": people, "domains": domains})
}

func (d Deps) grantRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string    `json:"email"`
		Role  auth.Role `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := d.Auth.GrantRole(r.Context(), req.Email, req.Role)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("role granted", "by", userFrom(r.Context()).ID, "user_id", p.ID, "role", p.Role)
	WriteJSON(w, http.StatusOK, p)
}

func (d Deps) revokeRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "That person is not an organiser.")
		return
	}
	if err := d.Auth.RevokeRole(r.Context(), id); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("role revoked", "by", userFrom(r.Context()).ID, "user_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) resetGoogleAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := d.Auth.ResetGoogleAccount(r.Context(), req.Email); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("google account link reset", "by", userFrom(r.Context()).ID, "domain", domainOf(req.Email))
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) addDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dm, created, err := d.Auth.AddDomain(r.Context(), req.Domain)
	if err != nil {
		d.changeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		d.Log.Info("domain allowed", "by", userFrom(r.Context()).ID, "domain", dm.Domain)
	}
	WriteJSON(w, status, dm)
}

func (d Deps) removeDomain(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	if err := d.Auth.RemoveDomain(r.Context(), domain); err != nil {
		d.changeError(w, r, err)
		return
	}
	d.Log.Info("domain removed", "by", userFrom(r.Context()).ID, "domain", domain)
	w.WriteHeader(http.StatusNoContent)
}
