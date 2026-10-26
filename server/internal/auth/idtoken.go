package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Identity is what a verified ID token says about the person signing in.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	// HostedDomain is Google's hd claim: the Workspace domain of the
	// account, empty for a consumer account.
	HostedDomain string
}

// errToken covers every reason an ID token is rejected. The detail is for
// logs only; people see a generic message.
var errToken = errors.New("id token rejected")

const (
	maxTokenBytes = 16 << 10
	clockSkew     = time.Minute
	// keyRefetchGap stops a stream of tokens with unknown key ids from
	// hammering the key endpoint.
	keyRefetchGap = time.Minute
	keyMaxAge     = 24 * time.Hour
)

// keySet fetches and caches the provider's RSA signing keys (a JWKS).
// At most one fetch is attempted per keyRefetchGap, successful or not, and
// cached keys keep working when a refetch fails: a slow or failing key
// endpoint at the start of a contest must not queue every sign-in behind
// repeated timeouts.
type keySet struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetched   time.Time // last successful fetch
	attempted time.Time // last fetch attempt
	lastErr   error
}

func (k *keySet) key(ctx context.Context, kid string, now time.Time) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	pub, have := k.keys[kid]
	if have && now.Sub(k.fetched) < keyMaxAge {
		return pub, nil
	}
	if !k.attempted.IsZero() && now.Sub(k.attempted) < keyRefetchGap {
		if have {
			return pub, nil // stale but still the provider's key
		}
		if k.lastErr != nil {
			return nil, k.lastErr
		}
		return nil, fmt.Errorf("%w: unknown key id", errToken)
	}
	k.attempted = now
	keys, err := fetchKeys(ctx, k.client, k.url)
	if err != nil {
		k.lastErr = err
		if have {
			return pub, nil
		}
		return nil, err
	}
	k.keys, k.fetched, k.lastErr = keys, now, nil
	if pub, ok := keys[kid]; ok {
		return pub, nil
	}
	return nil, fmt.Errorf("%w: unknown key id", errToken)
}

func fetchKeys(ctx context.Context, client *http.Client, url string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch signing keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch signing keys: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kty, Kid, N, E, Alg, Use string
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode signing keys: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, jk := range doc.Keys {
		if jk.Kty != "RSA" || (jk.Use != "" && jk.Use != "sig") || (jk.Alg != "" && jk.Alg != "RS256") {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(jk.N)
		e, err2 := base64.RawURLEncoding.DecodeString(jk.E)
		if err1 != nil || err2 != nil || len(e) > 4 || len(n) < 256 {
			continue
		}
		keys[jk.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return nil, errors.New("signing key set has no usable RSA keys")
	}
	return keys, nil
}

// tokenRules are the checks an ID token must pass.
type tokenRules struct {
	issuers  []string
	audience string
	nonce    string
	now      time.Time
	// maxAge, when set, refuses a token whose auth_time (when the person
	// last signed in at the provider) is older than this. A token without
	// auth_time passes: the max_age request parameter still applies.
	maxAge time.Duration
}

// verifyIDToken checks an RS256-signed ID token and returns its identity.
func verifyIDToken(ctx context.Context, keys *keySet, raw string, rules tokenRules) (Identity, error) {
	if len(raw) > maxTokenBytes {
		return Identity{}, fmt.Errorf("%w: too large", errToken)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Identity{}, fmt.Errorf("%w: malformed", errToken)
	}
	var header struct{ Alg, Kid string }
	if err := decodeSegment(parts[0], &header); err != nil {
		return Identity{}, err
	}
	if header.Alg != "RS256" {
		return Identity{}, fmt.Errorf("%w: algorithm %q", errToken, header.Alg)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, fmt.Errorf("%w: signature encoding", errToken)
	}
	pub, err := keys.key(ctx, header.Kid, rules.now)
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return Identity{}, fmt.Errorf("%w: bad signature", errToken)
	}

	var c struct {
		Iss           string          `json:"iss"`
		Aud           json.RawMessage `json:"aud"`
		Azp           string          `json:"azp"`
		Exp           int64           `json:"exp"`
		Iat           int64           `json:"iat"`
		AuthTime      int64           `json:"auth_time"`
		Nonce         string          `json:"nonce"`
		Sub           string          `json:"sub"`
		Email         string          `json:"email"`
		EmailVerified any             `json:"email_verified"`
		Name          string          `json:"name"`
		Picture       string          `json:"picture"`
		Hd            string          `json:"hd"`
	}
	if err := decodeSegment(parts[1], &c); err != nil {
		return Identity{}, err
	}
	switch {
	case !slices.Contains(rules.issuers, c.Iss):
		return Identity{}, fmt.Errorf("%w: issuer %q", errToken, c.Iss)
	case !audienceOK(c.Aud, c.Azp, rules.audience):
		return Identity{}, fmt.Errorf("%w: audience", errToken)
	case rules.now.After(time.Unix(c.Exp, 0).Add(clockSkew)):
		return Identity{}, fmt.Errorf("%w: expired", errToken)
	case time.Unix(c.Iat, 0).After(rules.now.Add(clockSkew)):
		return Identity{}, fmt.Errorf("%w: issued in the future", errToken)
	case rules.nonce == "" || c.Nonce != rules.nonce:
		return Identity{}, fmt.Errorf("%w: nonce", errToken)
	case rules.maxAge > 0 && c.AuthTime != 0 && rules.now.Sub(time.Unix(c.AuthTime, 0)) > rules.maxAge+clockSkew:
		return Identity{}, fmt.Errorf("%w: provider sign-in older than %s", errToken, rules.maxAge)
	case c.Sub == "" || c.Email == "":
		return Identity{}, fmt.Errorf("%w: missing subject or email", errToken)
	}
	verified := c.EmailVerified == true || c.EmailVerified == "true"
	return Identity{Subject: c.Sub, Email: strings.ToLower(c.Email), EmailVerified: verified, Name: c.Name, Picture: c.Picture, HostedDomain: strings.ToLower(c.Hd)}, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return fmt.Errorf("%w: segment encoding", errToken)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: segment json", errToken)
	}
	return nil
}

// audienceOK accepts "aud" as a string or an array of strings. With more
// than one audience the token must also name this client as its
// authorised party (azp), as OpenID Connect Core 3.1.3.7 requires.
func audienceOK(raw json.RawMessage, azp, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want && (azp == "" || azp == want)
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil || !slices.Contains(many, want) {
		return false
	}
	return len(many) == 1 || azp == want
}
