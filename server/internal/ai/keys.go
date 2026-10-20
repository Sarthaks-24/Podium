package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// KeyTTL is how long a pasted key stays usable (m5-plan section 1).
const KeyTTL = 2 * time.Hour

const redacted = "[redacted]"

// Secret holds a provider key. Every way of printing, logging or encoding
// it yields "[redacted]"; only this package can read the value, to hand it
// to a provider client. The value sits behind a pointer, so even reflection
// (a Secret in an unexported struct field printed with %v) shows an
// address, never the key.
type Secret struct {
	p *string
}

// NewSecret wraps a key.
func NewSecret(key string) Secret { return Secret{&key} }

// IsZero reports whether the secret holds nothing.
func (s Secret) IsZero() bool { return s.p == nil || *s.p == "" }

func (s Secret) reveal() string {
	if s.p == nil {
		return ""
	}
	return *s.p
}

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer (%#v).
func (Secret) GoString() string { return redacted }

// Format implements fmt.Formatter for every verb, so %x, %q and %+v
// redact too.
func (Secret) Format(f fmt.State, _ rune) { io.WriteString(f, redacted) }

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// MarshalText implements encoding.TextMarshaler (YAML, slog text keys).
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// KeyEntry is one admin's settings for one provider. Key is never shown
// back: views use KeyStatus.
type KeyEntry struct {
	Provider string
	// Model is "" until the admin chooses one.
	Model   string
	Key     Secret
	Expires time.Time
	// Models is the provider's filtered list, kept for the session once
	// listed; ListedAt is zero until then.
	Models   []Model
	ListedAt time.Time

	saved time.Time
}

// KeyStatus is what the key form may show: never the key.
type KeyStatus struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	ActiveUntil time.Time `json:"active_until"`
}

func (e KeyEntry) status() KeyStatus {
	return KeyStatus{Provider: e.Provider, Model: e.Model, ActiveUntil: e.Expires}
}

// Keys holds pasted keys in memory only, keyed by a hash of the admin's
// session token and the provider, each for KeyTTL. A restart forgets them
// all; signing out forgets that session's keys. Nothing here is ever
// written anywhere.
type Keys struct {
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu sync.Mutex
	m  map[string]map[string]*KeyEntry // session hash, then provider
}

func (k *Keys) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func sessionKey(session string) string {
	sum := sha256.Sum256([]byte(session))
	return hex.EncodeToString(sum[:])
}

// entries returns the session's live entries, dropping expired ones.
// k.mu must be held.
func (k *Keys) entries(session string, now time.Time) map[string]*KeyEntry {
	h := sessionKey(session)
	m := k.m[h]
	for p, e := range m {
		if !now.Before(e.Expires) {
			delete(m, p)
		}
	}
	if len(m) == 0 {
		delete(k.m, h)
		return nil
	}
	return m
}

// Put stores e for the session and its provider for KeyTTL from now,
// replacing that provider's earlier key, and makes it the active one.
func (k *Keys) Put(session string, e KeyEntry) KeyStatus {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]map[string]*KeyEntry{}
	}
	now := k.now()
	k.sweep(now)
	e.Expires = now.Add(KeyTTL)
	e.saved = now
	h := sessionKey(session)
	if k.m[h] == nil {
		k.m[h] = map[string]*KeyEntry{}
	}
	k.m[h][e.Provider] = &e
	return e.status()
}

// SetModel changes the model of the session's key for provider, keeping
// its expiry, and makes it the active one. It reports false when the
// session holds no key for provider.
func (k *Keys) SetModel(session, provider, model string) (KeyStatus, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	e, ok := k.entries(session, now)[provider]
	if !ok {
		return KeyStatus{}, false
	}
	e.Model, e.saved = model, now
	return e.status(), true
}

// SetModels keeps a provider's filtered model list for the session.
func (k *Keys) SetModels(session, provider string, models []Model) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if e, ok := k.entries(session, now)[provider]; ok {
		e.Models, e.ListedAt = append([]Model(nil), models...), now
	}
}

// Get returns the session's active entry: the live one saved last.
func (k *Keys) Get(session string) (KeyEntry, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var best *KeyEntry
	for _, e := range k.entries(session, k.now()) {
		if best == nil || e.saved.After(best.saved) || (e.saved.Equal(best.saved) && e.Provider < best.Provider) {
			best = e
		}
	}
	if best == nil {
		return KeyEntry{}, false
	}
	return *best, true
}

// GetProvider returns the session's entry for provider if it has not
// expired.
func (k *Keys) GetProvider(session, provider string) (KeyEntry, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.entries(session, k.now())[provider]
	if !ok {
		return KeyEntry{}, false
	}
	return *e, true
}

// Status returns what the key form may show for the session's active key.
func (k *Keys) Status(session string) (KeyStatus, bool) {
	e, ok := k.Get(session)
	if !ok {
		return KeyStatus{}, false
	}
	return e.status(), true
}

// Forget drops every key of the session (sign-out, and DELETE on the key
// route without a provider).
func (k *Keys) Forget(session string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, sessionKey(session))
}

// ForgetProvider drops the session's key for one provider.
func (k *Keys) ForgetProvider(session, provider string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	h := sessionKey(session)
	delete(k.m[h], provider)
	if len(k.m[h]) == 0 {
		delete(k.m, h)
	}
}

// Sweep drops expired keys. Get also drops them; Sweep bounds memory for
// sessions that never come back.
func (k *Keys) Sweep() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.sweep(k.now())
}

func (k *Keys) sweep(now time.Time) {
	for h, m := range k.m {
		for p, e := range m {
			if !now.Before(e.Expires) {
				delete(m, p)
			}
		}
		if len(m) == 0 {
			delete(k.m, h)
		}
	}
}
