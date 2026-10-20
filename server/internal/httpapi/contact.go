package httpapi

// The contact form and its inbox (contract C6, D71 as amended) and the
// site settings write (B3.7). Shapes are in docs/API.md, "Contact" and
// "PUT /api/admin/site-settings".

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contact"
)

// sendContact stores a message from the signed-in person. Beyond the
// general cross-site rule it needs an Origin equal to the base URL: the
// web app's fetch always sends one.
func (d Deps) sendContact(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != d.BaseURL {
		WriteError(w, http.StatusForbidden, "cross_site", "Send the message from the contact page at "+d.BaseURL+".")
		return
	}
	var req struct {
		Topic   string `json:"topic"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id, at, err := d.Contact.Send(r.Context(), userFrom(r.Context()).ID, req.Topic, req.Message)
	var fields contact.FieldErrors
	switch {
	case errors.As(err, &fields):
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_request", Message: fields.Error(), Fields: fields}})
	case errors.Is(err, contact.ErrUserLimit):
		contactLimited(w, "You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers.")
	case errors.Is(err, contact.ErrSiteLimit):
		contactLimited(w, "The organisers have received too many messages today. Try again tomorrow, or email the organisers.")
	case err != nil:
		d.internalError(w, r, err)
	default:
		WriteJSON(w, http.StatusCreated, map[string]int64{"id": id, "created_at": at})
	}
}

// contactRetry is the Retry-After of a refused message: the window is a
// day, so an hour is a sensible next look.
const contactRetry = 3600

func contactLimited(w http.ResponseWriter, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(contactRetry))
	WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
		"code": "rate_limited", "message": msg, "retry_after_s": contactRetry}})
}

func (d Deps) listContact(w http.ResponseWriter, r *http.Request) {
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		id, ok := parseID(v)
		if !ok {
			WriteError(w, http.StatusBadRequest, "bad_request", "before must be a message id.")
			return
		}
		before = id
	}
	list, more, err := d.Contact.List(r.Context(), before)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"messages": list, "more": more, "server_now": d.now().UnixMilli()})
}

func (d Deps) deleteContact(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	err := contact.ErrNotFound
	if ok {
		err = d.Contact.Delete(r.Context(), id)
	}
	switch {
	case errors.Is(err, contact.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "That message does not exist or was already deleted.")
	case err != nil:
		d.internalError(w, r, err)
	default:
		d.Log.Info("contact message deleted", "by", userFrom(r.Context()).ID, "message_id", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d Deps) setSiteSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           *string `json:"name"`
		ContactEmail   *string `json:"contact_email"`
		Phone          *string `json:"phone"`
		PrivacyContact *string `json:"privacy_contact"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil || req.ContactEmail == nil || req.Phone == nil || req.PrivacyContact == nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "Send all four fields: name, contact_email, phone and privacy_contact.")
		return
	}
	o, err := d.Auth.SetOrganisation(r.Context(), auth.Organisation{Name: *req.Name, ContactEmail: *req.ContactEmail,
		Phone: *req.Phone, PrivacyContact: *req.PrivacyContact})
	var fields auth.FieldErrors
	switch {
	case errors.As(err, &fields):
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_request", Message: fields.Error(), Fields: fields}})
	case err != nil:
		d.internalError(w, r, err)
	default:
		d.Log.Info("site settings changed", "by", userFrom(r.Context()).ID)
		WriteJSON(w, http.StatusOK, o)
	}
}
