package config

import (
	"log/slog"
	"net/url"
	"strings"
)

// JudgeVars lists every variable podium-judge reads, in the order it reads
// them. judge/.env.example and docs/OPERATIONS.md mirror this list. The
// server URL and token are required only by "podium-judge work".
var JudgeVars = []Var{
	{Name: "PODIUM_MODE", Default: string(ModeProduction)},
	{Name: "PODIUM_SERVER_URL", Default: "http://127.0.0.1:8081"},
	{Name: "PODIUM_JUDGE_TOKEN", Secret: true},
	{Name: "PODIUM_ISOLATE_PATH", Default: "/usr/bin/isolate"},
	{Name: "PODIUM_JUDGE_LANES", Default: "2"},
	{Name: "PODIUM_JUDGE_CACHE_DIR", Default: "/var/cache/podium-judge"},
	{Name: "PODIUM_JUDGE_BLOB_CACHE_MB", Default: "2048"},
	{Name: "PODIUM_LOG_LEVEL", Default: "info"},
}

// Judge is the validated configuration of podium-judge.
type Judge struct {
	// Mode is testing, dev or production; outside production the token
	// may be left out and the testing token is used.
	Mode Mode
	// ServerURL is the server's judge API (its PODIUM_JUDGE_LISTEN).
	ServerURL string
	// Token authenticates this worker; it equals the server's.
	Token string
	// IsolatePath is the setuid isolate binary.
	IsolatePath string
	// Lanes is the number of sandboxes run in parallel; each is one box id.
	Lanes int
	// CacheDir holds test blobs and compiled binaries.
	CacheDir string
	// BlobCacheMB caps the downloaded files in CacheDir/blobs; the least
	// recently used ones no running job holds are removed above it.
	BlobCacheMB int
	// LogLevel is the minimum level written to the JSON log.
	LogLevel slog.Level
}

// LoadWorker validates the configuration of "podium-judge work": the
// sandbox settings plus the server to pull work from.
func LoadWorker(get Getenv) (Judge, error) {
	r := newReader(get, JudgeService, JudgeVars)
	c := Judge{Mode: r.mode("PODIUM_MODE"), ServerURL: r.str("PODIUM_SERVER_URL")}
	if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		r.p.add("PODIUM_SERVER_URL must be the server's judge address, such as http://127.0.0.1:8081")
	}
	c.ServerURL = strings.TrimSuffix(c.ServerURL, "/")
	switch {
	case r.str("PODIUM_JUDGE_TOKEN") != "":
		c.Token = r.token("PODIUM_JUDGE_TOKEN")
		if c.Mode == ModeProduction && c.Token == TestingJudgeToken {
			r.p.add("PODIUM_JUDGE_TOKEN is the public testing token; production needs the server's own token")
		}
	case c.Mode != ModeProduction:
		c.Token = TestingJudgeToken
	default:
		r.p.add("PODIUM_JUDGE_TOKEN is required in production mode but missing")
	}
	c.IsolatePath = r.str("PODIUM_ISOLATE_PATH")
	c.Lanes = r.int("PODIUM_JUDGE_LANES", 1, 16)
	c.CacheDir = r.str("PODIUM_JUDGE_CACHE_DIR")
	c.BlobCacheMB = r.int("PODIUM_JUDGE_BLOB_CACHE_MB", 64, 1<<20)
	c.LogLevel = r.level("PODIUM_LOG_LEVEL")
	return c, r.p.err()
}

// LoadJudge validates the sandbox settings, which is all "podium-judge
// calibrate" needs.
func LoadJudge(get Getenv) (Judge, error) {
	r := newReader(get, JudgeService, JudgeVars)
	c := Judge{
		IsolatePath: r.str("PODIUM_ISOLATE_PATH"),
		Lanes:       r.int("PODIUM_JUDGE_LANES", 1, 16),
		CacheDir:    r.str("PODIUM_JUDGE_CACHE_DIR"),
		LogLevel:    r.level("PODIUM_LOG_LEVEL"),
	}
	return c, r.p.err()
}
