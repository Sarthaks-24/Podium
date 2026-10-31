package auth

// Site settings: the organisation details the public pages
// show, entered by an owner on the Dashboard's Organisation tab and stored in the settings table
// under one key. They are public by design (GET /api/auth/config), so the
// rules below are about shape, not secrecy: one line each, bounded, and
// never a value the server made up.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// AuditSettingsChanged is the site audit action for a change to the site
// settings.
const AuditSettingsChanged = "settings_changed"

// settingsKey is the settings row that holds the organisation.
const settingsKey = "organisation"

// Organisation is what the public pages say about who runs the site. An
// empty field is shown as nothing.
type Organisation struct {
	Name           string `json:"name"`
	ContactEmail   string `json:"contact_email"`
	Phone          string `json:"phone"`
	PrivacyContact string `json:"privacy_contact"`
}

// FieldErrors maps a form field to what is wrong with it.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	parts := make([]string, 0, len(f))
	for _, m := range f {
		parts = append(parts, m)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// orgCache holds the organisation once read: every sign-in page and every
// config request shows it, and only SetOrganisation changes it.
type orgCache struct {
	mu     sync.Mutex
	loaded bool
	org    Organisation
}

func (s *Service) orgCache() *orgCache { return &s.org }

// Organisation returns the site settings, empty fields until an owner
// enters them.
func (s *Service) Organisation(ctx context.Context) (Organisation, error) {
	c := s.orgCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.org, nil
	}
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		c.loaded = true
		return c.org, nil
	}
	if err != nil {
		return Organisation{}, err
	}
	var o Organisation
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return Organisation{}, err
	}
	c.org, c.loaded = o, true
	return o, nil
}

// singleLine reports whether v is printable text without line breaks or
// other control characters.
func singleLine(v string) bool {
	if !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) || Hidden(r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

// Hidden reports a format character (Unicode Cf) other than the zero-width
// joiners some scripts need: bidirectional overrides and the like, which
// could make stored text read differently from what it says.
func Hidden(r rune) bool {
	return unicode.Is(unicode.Cf, r) && r != 0x200c && r != 0x200d
}

// checkOrganisation trims o and returns what is wrong with it.
func checkOrganisation(o *Organisation) FieldErrors {
	o.Name = strings.TrimSpace(o.Name)
	o.ContactEmail = strings.ToLower(strings.TrimSpace(o.ContactEmail))
	o.Phone = strings.TrimSpace(o.Phone)
	o.PrivacyContact = strings.TrimSpace(o.PrivacyContact)
	f := FieldErrors{}
	if utf8.RuneCountInString(o.Name) > 120 || !singleLine(o.Name) {
		f["name"] = "Use one line of at most 120 characters for the name."
	}
	if o.ContactEmail != "" {
		if a, err := mail.ParseAddress(o.ContactEmail); err != nil || a.Address != o.ContactEmail || len(o.ContactEmail) > 254 {
			f["contact_email"] = "Enter a single email address, such as podium@college.edu."
		}
	}
	if o.Phone != "" {
		digits, ok := 0, len(o.Phone) <= 32
		for _, r := range o.Phone {
			switch {
			case r >= '0' && r <= '9':
				digits++
			case strings.ContainsRune(" +-().", r):
			default:
				ok = false
			}
		}
		if !ok || digits < 6 {
			f["phone"] = "Use digits, spaces and + - ( ) . only, such as +91 80 1234 5678."
		}
	}
	if utf8.RuneCountInString(o.PrivacyContact) > 200 || !singleLine(o.PrivacyContact) {
		f["privacy_contact"] = "Use one line of at most 200 characters for the privacy contact."
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

// SetOrganisation saves the site settings for the actor on ctx and
// records which fields changed in the site audit. It returns the saved
// values, trimmed.
func (s *Service) SetOrganisation(ctx context.Context, o Organisation) (Organisation, error) {
	if f := checkOrganisation(&o); f != nil {
		return Organisation{}, f
	}
	old, err := s.Organisation(ctx)
	if err != nil {
		return Organisation{}, err
	}
	var changed []string
	for _, c := range []struct {
		name     string
		was, now string
	}{{"name", old.Name, o.Name}, {"contact_email", old.ContactEmail, o.ContactEmail},
		{"phone", old.Phone, o.Phone}, {"privacy_contact", old.PrivacyContact, o.PrivacyContact}} {
		if c.was != c.now {
			changed = append(changed, c.name)
		}
	}
	if len(changed) == 0 {
		return o, nil
	}
	body, _ := json.Marshal(o)
	c := s.orgCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Organisation{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingsKey, string(body), s.now().UnixMilli()); err != nil {
		return Organisation{}, err
	}
	if err := s.recordSite(ctx, tx, AuditSettingsChanged, "", map[string]any{"fields": changed}); err != nil {
		return Organisation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Organisation{}, err
	}
	c.org, c.loaded = o, true
	return o, nil
}
