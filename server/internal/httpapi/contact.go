package httpapi

// The contact form (contract C6, D71 as amended): signed-in only, stored
// in contact_messages, read by owners and admins in an inbox. Day-one
// stubs: the shapes are final, storage lands next.

import (
	"net/http"
	"time"
)

func (d Deps) sendContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Topic   string `json:"topic"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": 1, "created_at": time.Now().UnixMilli()})
}

func (d Deps) listContact(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "more": false})
}

func (d Deps) deleteContact(w http.ResponseWriter, r *http.Request) {
	WriteError(w, http.StatusNotFound, "not_found", "That message does not exist or was already deleted.")
}

func (d Deps) setSiteSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		ContactEmail   string `json:"contact_email"`
		Phone          string `json:"phone"`
		PrivacyContact string `json:"privacy_contact"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	WriteJSON(w, http.StatusOK, req)
}
