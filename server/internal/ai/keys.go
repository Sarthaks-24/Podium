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

// Provider names.
const (
	ProviderAnthropic = "anthropic"
	// ProviderFake exists only in testing mode (decision D50).
	ProviderFake = "fake"
)

// Models offered for the Anthropic provider; the first is the default.
var AnthropicModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}

// ModelNames are the names the key form shows for AnthropicModels.
var ModelNames = map[string]string{
	"claude-opus-5-5":   "Claude Opus 5.5 (default)",
	"claude-sonnet-5-5": "Claude Sonnet 5.5",
	"claude-haiku-5-5":  "Claude Haiku 5.5",
}

// DefaultModel is used when an admin does not choose one.
const DefaultModel = "claude-opus-5-5"

// KeyEntry is one admin's provider settings. Key is never shown back:
// views use Status.
type KeyEntry struct {
	Provider string
	Model    string
	Key      Secret
	Expires  time.Time
}

// KeyStatus is what the key form may show: never the key.
type KeyStatus struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	ActiveUntil time.Time `json:"active_until"`
}

// Keys holds pasted keys in memory only, keyed by a hash of the admin's
// session token, for KeyTTL. A restart forgets them all; signing out
// forgets that session's key. Nothing here is ever written anywhere.
type Keys struct {
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu sync.Mutex
	m  map[string]KeyEntry
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

// Put stores e for the session for KeyTTL from now and returns its status.
func (k *Keys) Put(session string, e KeyEntry) KeyStatus {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]KeyEntry{}
	}
	now := k.now()
	k.sweep(now)
	e.Expires = now.Add(KeyTTL)
	k.m[sessionKey(session)] = e
	return KeyStatus{Provider: e.Provider, Model: e.Model, ActiveUntil: e.Expires}
}

// Get returns the session's entry if it has not expired.
func (k *Keys) Get(session string) (KeyEntry, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	h := sessionKey(session)
	e, ok := k.m[h]
	if !ok {
		return KeyEntry{}, false
	}
	if !k.now().Before(e.Expires) {
		delete(k.m, h)
		return KeyEntry{}, false
	}
	return e, true
}

// Status returns what the key form may show for the session.
func (k *Keys) Status(session string) (KeyStatus, bool) {
	e, ok := k.Get(session)
	if !ok {
		return KeyStatus{}, false
	}
	return KeyStatus{Provider: e.Provider, Model: e.Model, ActiveUntil: e.Expires}, true
}

// Forget drops the session's key (DELETE on the key route, and sign-out).
func (k *Keys) Forget(session string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, sessionKey(session))
}

// Sweep drops expired keys. Get also drops them; Sweep bounds memory for
// sessions that never come back.
func (k *Keys) Sweep() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.sweep(k.now())
}

func (k *Keys) sweep(now time.Time) {
	for h, e := range k.m {
		if !now.Before(e.Expires) {
			delete(k.m, h)
		}
	}
}
