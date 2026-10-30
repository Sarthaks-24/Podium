package httpapi

// The owner's organisation details: name, contact email, phone and
// privacy contact, shown in the footer and on the sign-in page. Shapes are
// in docs/API.md, "Site settings".

import (
	"errors"
	"net/http"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// setSiteSettings saves all four fields at once; a refused field says
// what to fix under itself.
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
