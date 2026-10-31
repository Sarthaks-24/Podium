package config

import (
	"log/slog"
	"net"
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
	{Name: "PODIUM_JUDGE_BOX_BASE", Default: "0"},
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
	// BoxBase is the first lane's isolate box id; lane n uses BoxBase+n,
	// so a second judge on the same host can take other boxes.
	BoxBase int
	// CacheDir holds test blobs and compiled binaries.
	CacheDir string
	// BlobCacheMB caps the downloaded files in CacheDir/blobs; the least
	// recently used ones no running job holds are removed above it.
	BlobCacheMB int
	// LogLevel is the minimum level written to the JSON log.
	LogLevel slog.Level
}

// SelfTestBox is the isolate box "podium-judge selftest" and "calibrate"
// use by default, clear of the lanes.
const SelfTestBox = 80

// MaxBoxBase keeps the last lane under isolate's default of 1,000 boxes.
const MaxBoxBase = 1000 - 16

// LoadWorker validates the configuration of "podium-judge work": the
// sandbox settings plus the server to pull work from.
func LoadWorker(get Getenv) (Judge, error) {
	r := newReader(get, JudgeService, JudgeVars)
	c := Judge{Mode: r.mode("PODIUM_MODE"), ServerURL: r.str("PODIUM_SERVER_URL")}
	if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		r.p.add("PODIUM_SERVER_URL must be the server's judge address, such as http://127.0.0.1:8081")
	} else if c.Mode == ModeProduction && u.Scheme == "http" && !IsLoopback(net.JoinHostPort(u.Hostname(), "0")) {
		// The token travels in every request: over plain http it must not
		// leave the machine. A remote judge uses the local end of a tunnel.
		r.p.add("PODIUM_SERVER_URL must be a loopback address in production, such as http://127.0.0.1:8081 (for a judge on another machine, the local end of the tunnel), so the judge token never crosses the network in clear")
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
	c.BoxBase = r.int("PODIUM_JUDGE_BOX_BASE", 0, MaxBoxBase)
	if c.Lanes > 0 && c.BoxBase <= SelfTestBox && c.BoxBase+c.Lanes > SelfTestBox {
		r.p.add("PODIUM_JUDGE_BOX_BASE and PODIUM_JUDGE_LANES give boxes %d to %d, which include box %d that selftest and calibrate use; choose a base that keeps clear of it",
			c.BoxBase, c.BoxBase+c.Lanes-1, SelfTestBox)
	}
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
