package config

import "log/slog"

// JudgeVars lists every variable podium-judge reads, in the order it reads
// them. judge/.env.example and docs/OPERATIONS.md mirror this list.
// PODIUM_SERVER_URL and PODIUM_JUDGE_TOKEN join when the worker loop lands
// in milestone M3.
var JudgeVars = []Var{
	{Name: "PODIUM_ISOLATE_PATH", Default: "/usr/bin/isolate"},
	{Name: "PODIUM_JUDGE_LANES", Default: "2"},
	{Name: "PODIUM_JUDGE_CACHE_DIR", Default: "/var/cache/podium-judge"},
	{Name: "PODIUM_LOG_LEVEL", Default: "info"},
}

// Judge is the validated configuration of podium-judge.
type Judge struct {
	// IsolatePath is the setuid isolate binary.
	IsolatePath string
	// Lanes is the number of sandboxes run in parallel; each is one box id.
	Lanes int
	// CacheDir holds test blobs and compiled binaries.
	CacheDir string
	// LogLevel is the minimum level written to the JSON log.
	LogLevel slog.Level
}

// LoadJudge validates the judge configuration.
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
