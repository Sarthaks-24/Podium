package config

import (
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
)

// ServerVars lists every variable podium-server reads, in the order it reads
// them. server/.env.example and docs/OPERATIONS.md mirror this list.
var ServerVars = []Var{
	{Name: "PODIUM_BASE_URL", Required: true},
	{Name: "PODIUM_LISTEN", Default: "127.0.0.1:8080"},
	{Name: "PODIUM_DB_PATH", Default: "data/podium.db"},
	{Name: "PODIUM_TESTS_DIR", Default: "data/tests"},
	{Name: "PODIUM_OWNER_EMAIL", Required: true},
	{Name: "PODIUM_ALLOWED_DOMAINS"},
	{Name: "PODIUM_SESSION_SECRET", Required: true, Secret: true},
	{Name: "GOOGLE_CLIENT_ID", Required: true},
	{Name: "GOOGLE_CLIENT_SECRET", Required: true, Secret: true},
	{Name: "PODIUM_LOG_LEVEL", Default: "info"},
}

// Server is the validated configuration of podium-server.
type Server struct {
	// BaseURL is the public URL students open, without a trailing slash.
	BaseURL string
	// Listen is the address the HTTP server binds; Caddy proxies to it.
	Listen string
	// DBPath is the SQLite database file.
	DBPath string
	// TestsDir holds test blobs named by their SHA-256.
	TestsDir string
	// OwnerEmail is always an owner; it is the recovery path if every other
	// owner is removed, and the contact shown on the sign-in page.
	OwnerEmail string
	// AllowedDomains seeds the allowed-domain list on first start only;
	// afterwards owners manage the list in the web app.
	AllowedDomains []string
	// SessionSecret signs the short-lived sign-in state cookie.
	SessionSecret []byte
	// GoogleClientID and GoogleClientSecret identify the OAuth client.
	GoogleClientID     string
	GoogleClientSecret string
	// LogLevel is the minimum level written to the JSON log.
	LogLevel slog.Level
}

// minSecretLen is the shortest accepted PODIUM_SESSION_SECRET.
const minSecretLen = 32

var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// NormaliseDomain lowercases d, strips a leading "@" and reports whether the
// result is a valid DNS domain.
func NormaliseDomain(d string) (string, bool) {
	d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
	return d, len(d) <= 253 && domainPattern.MatchString(d)
}

// LoadServer validates the server configuration. On failure the error lists
// every problem at once and names the file to fix.
func LoadServer(get Getenv) (Server, error) {
	r := newReader(get, ServerService, ServerVars)
	c := Server{
		BaseURL:  r.baseURL("PODIUM_BASE_URL"),
		Listen:   r.str("PODIUM_LISTEN"),
		DBPath:   r.str("PODIUM_DB_PATH"),
		TestsDir: r.str("PODIUM_TESTS_DIR"),
	}
	c.OwnerEmail = r.email("PODIUM_OWNER_EMAIL")
	for _, d := range strings.Split(r.str("PODIUM_ALLOWED_DOMAINS"), ",") {
		if strings.TrimSpace(d) == "" {
			continue
		}
		nd, ok := NormaliseDomain(d)
		if !ok {
			r.p.add("PODIUM_ALLOWED_DOMAINS must be a comma-separated list of domains such as college.edu")
			break
		}
		c.AllowedDomains = append(c.AllowedDomains, nd)
	}
	if s := r.str("PODIUM_SESSION_SECRET"); s != "" {
		if len(s) < minSecretLen || strings.Contains(s, "change-me") {
			r.p.add("PODIUM_SESSION_SECRET must be a random string of at least %d characters (for example from: openssl rand -base64 48)", minSecretLen)
		} else {
			c.SessionSecret = []byte(s)
		}
	}
	c.GoogleClientID = r.placeholderFree("GOOGLE_CLIENT_ID")
	c.GoogleClientSecret = r.placeholderFree("GOOGLE_CLIENT_SECRET")
	c.LogLevel = r.level("PODIUM_LOG_LEVEL")
	return c, r.p.err()
}

func (r *reader) email(name string) string {
	s := r.str(name)
	if s == "" {
		return ""
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s, "@") || strings.Contains(s, "change-me") {
		r.p.add("%s must be a plain email address such as you@college.edu", name)
		return ""
	}
	return strings.ToLower(s)
}

func (r *reader) placeholderFree(name string) string {
	s := r.str(name)
	if strings.Contains(s, "change-me") || strings.HasPrefix(s, "your-") {
		r.p.add("%s still holds the example placeholder", name)
		return ""
	}
	return s
}
