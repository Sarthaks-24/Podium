package config

import "log/slog"

// ServerVars lists every variable podium-server reads, in the order it reads
// them. server/.env.example and docs/OPERATIONS.md mirror this list.
var ServerVars = []Var{
	{Name: "PODIUM_BASE_URL", Required: true},
	{Name: "PODIUM_LISTEN", Default: "127.0.0.1:8080"},
	{Name: "PODIUM_DB_PATH", Default: "data/podium.db"},
	{Name: "PODIUM_TESTS_DIR", Default: "data/tests"},
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
	// LogLevel is the minimum level written to the JSON log.
	LogLevel slog.Level
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
		LogLevel: r.level("PODIUM_LOG_LEVEL"),
	}
	return c, r.p.err()
}
