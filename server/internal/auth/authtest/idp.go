// Package authtest provides a fake OpenID Connect provider for tests. It
// implements the parts of Google's flow podium uses: an authorization
// endpoint that redirects straight back with a code, a token endpoint that
// checks the client credentials and the PKCE verifier, and a key set for
// verifying the RS256 ID tokens it signs.
package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// Person is the identity the fake provider asserts for the next sign-in.
type Person struct {
	Email    string
	Verified bool
	Name     string
}

// IdP is a running fake provider.
type IdP struct {
	Server       *httptest.Server
	ClientID     string
	ClientSecret string

	key *rsa.PrivateKey
	mu  sync.Mutex
	// next is asserted for the next authorization request.
	next Person
	// codes maps issued codes to their request.
	codes map[string]grant
	// Mutate, when set, edits the claims of the next ID token (for
	// testing rejections such as a wrong audience or nonce).
	Mutate func(claims map[string]any)
	// LastAuthQuery is the query of the most recent authorization request.
	LastAuthQuery url.Values
}

type grant struct {
	person      Person
	nonce       string
	challenge   string
	redirectURI string
	// maxAge is set when the request carried max_age: like Google, the
	// provider then states auth_time (the person "signs in" just now).
	maxAge bool
}

// New starts a fake provider; it stops when the test ends.
func New(t *testing.T) *IdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &IdP{ClientID: "test-client.apps.example", ClientSecret: "test-secret", key: key, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", p.authorize)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/certs", p.certs)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Server.Close)
	return p
}

// Endpoints returns endpoints for auth.NewProvider.
func (p *IdP) Endpoints() auth.Endpoints {
	return auth.Endpoints{
		AuthURL: p.Server.URL + "/auth", TokenURL: p.Server.URL + "/token", KeysURL: p.Server.URL + "/certs",
		Issuers: []string{p.Server.URL},
	}
}

// SignInAs sets the identity asserted on the next authorization request.
func (p *IdP) SignInAs(person Person) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = person
}

func (p *IdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.mu.Lock()
	p.LastAuthQuery = q
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		p.mu.Unlock()
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := fmt.Sprintf("code-%d", len(p.codes)+1)
	p.codes[code] = grant{person: p.next, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"),
		maxAge: q.Get("max_age") != ""}
	p.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	bq := back.Query()
	bq.Set("code", code)
	bq.Set("state", q.Get("state"))
	back.RawQuery = bq.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *IdP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	mutate := p.Mutate
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case !ok:
		tokenError(w, "invalid_grant")
		return
	case r.PostForm.Get("client_id") != p.ClientID || r.PostForm.Get("client_secret") != p.ClientSecret:
		tokenError(w, "invalid_client")
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
		tokenError(w, "invalid_grant") // PKCE verifier does not match
		return
	case r.PostForm.Get("redirect_uri") != g.redirectURI:
		tokenError(w, "redirect_uri_mismatch")
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.Server.URL, "aud": p.ClientID, "sub": "sub-" + g.person.Email,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": g.nonce,
		"email": g.person.Email, "email_verified": g.person.Verified, "name": g.person.Name,
		"picture": "https://lh3.googleusercontent.com/a/test",
	}
	if g.maxAge {
		claims["auth_time"] = now.Unix()
	}
	if mutate != nil {
		mutate(claims)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": p.Sign(claims),
	})
}

// Sign returns an RS256 ID token for claims, signed with the provider key.
func (p *IdP) Sign(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (p *IdP) certs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(p.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

func tokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}
