package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Flow is what the server must remember between sending the browser to
// Google and Google sending it back. It travels in a short-lived HttpOnly
// cookie, signed with the session secret so the browser cannot alter it.
type Flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
	Expires  int64  `json:"e"`
}

// FlowTTL is how long a sign-in attempt may take.
const FlowTTL = 10 * time.Minute

// ErrFlow means the sign-in state cookie is missing, altered or expired.
var ErrFlow = errors.New("sign-in state invalid or expired")

// NewFlow starts a sign-in attempt that returns to next (made safe).
func NewFlow(next string, now time.Time) Flow {
	return Flow{
		State:    randomToken(24),
		Nonce:    randomToken(24),
		Verifier: oauth2.GenerateVerifier(),
		Next:     SafeNext(next),
		Expires:  now.Add(FlowTTL).Unix(),
	}
}

// SealFlow encodes and signs f for the cookie.
func SealFlow(secret []byte, f Flow) (string, error) {
	body, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + base64.RawURLEncoding.EncodeToString(flowMAC(secret, payload)), nil
}

// OpenFlow verifies and decodes a cookie written by SealFlow.
func OpenFlow(secret []byte, value string, now time.Time) (Flow, error) {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok {
		return Flow{}, ErrFlow
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, flowMAC(secret, payload)) {
		return Flow{}, ErrFlow
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Flow{}, ErrFlow
	}
	var f Flow
	if err := json.Unmarshal(body, &f); err != nil || now.Unix() > f.Expires || f.State == "" {
		return Flow{}, ErrFlow
	}
	return f, nil
}

func flowMAC(secret []byte, payload string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("podium sign-in state v1\x00"))
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// randomToken returns n random bytes, base64url-encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// SafeNext returns next if it is a path on this site, otherwise "/". It
// blocks open redirects such as "//evil.example", "/\evil.example" or
// "https://evil.example", and never returns into the API.
func SafeNext(next string) string {
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") ||
		strings.ContainsAny(next, "\r\n\t\\") || len(next) > 1024 {
		return "/"
	}
	// Clean the path the way the redirect will, then check the result:
	// "/x/../api/..." must not land in the API.
	p, query, hasQuery := strings.Cut(next, "?")
	clean := path.Clean(p)
	if strings.HasSuffix(p, "/") && clean != "/" {
		clean += "/"
	}
	if clean == "/api" || strings.HasPrefix(clean, "/api/") || strings.HasPrefix(clean, "//") {
		return "/"
	}
	if hasQuery {
		return clean + "?" + query
	}
	return clean
}
