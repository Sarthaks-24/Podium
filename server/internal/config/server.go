package config

import (
	"crypto/rand"
	"log/slog"
	"net"
	"net/mail"
	"regexp"
	"strings"
)

// ServerVars lists every variable podium-server reads, in the order it reads
// them. server/.env.example and docs/OPERATIONS.md mirror this list. Which
// ones are required depends on PODIUM_MODE (see LoadServer), so Required is
// not set here.
var ServerVars = []Var{
	{Name: "PODIUM_MODE", Default: string(ModeProduction)},
	{Name: "PODIUM_BASE_URL"},
	{Name: "PODIUM_LISTEN", Default: "127.0.0.1:8080"},
	{Name: "PODIUM_JUDGE_LISTEN", Default: "127.0.0.1:8081"},
	{Name: "PODIUM_DB_PATH", Default: "data/podium.db"},
	{Name: "PODIUM_TESTS_DIR", Default: "data/tests"},
	{Name: "PODIUM_OWNER_EMAIL"},
	{Name: "PODIUM_ALLOWED_DOMAINS"},
	{Name: "PODIUM_SESSION_SECRET", Secret: true},
	{Name: "GOOGLE_CLIENT_ID"},
	{Name: "GOOGLE_CLIENT_SECRET", Secret: true},
	{Name: "PODIUM_JUDGE_TOKEN", Secret: true},
	{Name: "PODIUM_LOG_LEVEL", Default: "info"},
}

// Server is the validated configuration of podium-server.
type Server struct {
	// Mode is testing, dev or production.
	Mode Mode
	// BaseURL is the public URL students open, without a trailing slash.
	BaseURL string
	// Listen is the address the HTTP server binds; Caddy proxies to it.
	Listen string
	// JudgeListen is the address of the judge worker API. It is a separate
	// listener so the judge routes are never behind the public proxy
	// (decision D44); keep it on loopback unless a remote worker needs it.
	JudgeListen string
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
	// SessionSecret signs the short-lived sign-in state cookie. In testing
	// mode without one, a random secret is made for this run.
	SessionSecret []byte
	// GoogleClientID and GoogleClientSecret identify the OAuth client.
	GoogleClientID     string
	GoogleClientSecret string
	// JudgeToken authenticates judge workers. Testing and dev mode fall
	// back to TestingJudgeToken.
	JudgeToken string
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
// every problem at once and names the file to fix. What is required depends
// on the mode (decision D48):
//
//	                     testing           dev        production
//	PODIUM_BASE_URL      localhost:5173    required   required, https
//	public listener      loopback only     any        any
//	judge listener       loopback only     loopback   loopback
//	PODIUM_OWNER_EMAIL   owner@podium.test required   required
//	session secret       random per run    required   required
//	Google client        not used          required   required
//	PODIUM_JUDGE_TOKEN   testing token     optional   required, not the testing token
func LoadServer(get Getenv) (Server, error) {
	r := newReader(get, ServerService, ServerVars)
	c := Server{Mode: r.mode("PODIUM_MODE")}
	testing := c.Mode == ModeTesting
	need := func(name, value string) {
		if value == "" && r.get(name) == "" {
			r.p.add("%s is required in %s mode but missing", name, c.Mode)
		}
	}

	if testing && strings.TrimSpace(r.get("PODIUM_BASE_URL")) == "" {
		c.BaseURL = "http://localhost:5173"
	} else {
		c.BaseURL = r.baseURL("PODIUM_BASE_URL")
		need("PODIUM_BASE_URL", c.BaseURL)
	}
	c.Listen = r.str("PODIUM_LISTEN")
	c.JudgeListen = r.str("PODIUM_JUDGE_LISTEN")
	c.DBPath = r.str("PODIUM_DB_PATH")
	c.TestsDir = r.str("PODIUM_TESTS_DIR")
	switch {
	case testing && (!loopbackURL(c.BaseURL) || !IsLoopback(c.Listen)):
		r.p.add("testing mode turns sign-in off, so it runs only on this machine: PODIUM_BASE_URL must be a localhost address and PODIUM_LISTEN a loopback address")
	case c.Mode == ModeProduction && c.BaseURL != "" && !strings.HasPrefix(c.BaseURL, "https://"):
		r.p.add("PODIUM_BASE_URL must be https in production mode; use PODIUM_MODE=dev for local work over http")
	}

	c.OwnerEmail = r.email("PODIUM_OWNER_EMAIL")
	if testing && c.OwnerEmail == "" && r.get("PODIUM_OWNER_EMAIL") == "" {
		c.OwnerEmail = testingOwner
	}
	need("PODIUM_OWNER_EMAIL", c.OwnerEmail)
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
	} else if testing {
		c.SessionSecret = randomSecret()
	} else {
		need("PODIUM_SESSION_SECRET", "")
	}
	if !testing {
		c.GoogleClientID = r.placeholderFree("GOOGLE_CLIENT_ID")
		c.GoogleClientSecret = r.placeholderFree("GOOGLE_CLIENT_SECRET")
		need("GOOGLE_CLIENT_ID", c.GoogleClientID)
		need("GOOGLE_CLIENT_SECRET", c.GoogleClientSecret)
	}
	c.JudgeToken = r.token("PODIUM_JUDGE_TOKEN")
	switch {
	case c.JudgeToken == "" && r.get("PODIUM_JUDGE_TOKEN") == "" && c.Mode != ModeProduction:
		c.JudgeToken = TestingJudgeToken
	case c.Mode == ModeProduction && c.JudgeToken == TestingJudgeToken:
		r.p.add("PODIUM_JUDGE_TOKEN is the public testing token; production needs its own (openssl rand -hex 32)")
	default:
		need("PODIUM_JUDGE_TOKEN", c.JudgeToken)
	}
	if c.JudgeListen != "" && c.JudgeListen == c.Listen {
		r.p.add("PODIUM_JUDGE_LISTEN must differ from PODIUM_LISTEN: the judge API has its own listener")
	} else if c.JudgeListen != "" && !IsLoopback(c.JudgeListen) {
		r.p.add("PODIUM_JUDGE_LISTEN must be a loopback address such as 127.0.0.1:8081; reach a remote judge through an SSH or WireGuard tunnel (docs/OPERATIONS.md)")
	}
	c.LogLevel = r.level("PODIUM_LOG_LEVEL")
	return c, r.p.err()
}

func randomSecret() []byte {
	b := make([]byte, 48)
	rand.Read(b)
	return b
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

// IsLoopback reports whether a host:port listen address binds only the
// local machine.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// token reads a shared secret: at least minSecretLen characters and not
// the example placeholder.
func (r *reader) token(name string) string {
	s := r.str(name)
	if s == "" {
		return ""
	}
	if len(s) < minSecretLen || strings.Contains(s, "change-me") {
		r.p.add("%s must be a random string of at least %d characters (for example from: openssl rand -hex 32), the same in server.env and judge.env", name, minSecretLen)
		return ""
	}
	return s
}

func (r *reader) placeholderFree(name string) string {
	s := r.str(name)
	if strings.Contains(s, "change-me") || strings.HasPrefix(s, "your-") {
		r.p.add("%s still holds the example placeholder", name)
		return ""
	}
	return s
}
