// Package config reads each service's configuration from environment
// variables and refuses to start when a required variable is missing or
// malformed. It is the start-up validator described in docs/OPERATIONS.md:
// errors name the variable and the file it belongs in, and log summaries
// print "set" or "missing", never a value.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Getenv looks up one variable. os.Getenv satisfies it; tests pass a map.
type Getenv func(key string) string

// Var describes one variable a service reads.
type Var struct {
	Name     string
	Required bool
	Default  string
	// Secret marks values that must never appear in logs or errors.
	Secret bool
}

// Service names the env files for one service, used in error messages.
type Service struct {
	Name      string // "server" or "judge"
	LocalFile string // e.g. server/.env
	VMFile    string // e.g. /etc/podium/server.env
}

var (
	// ServerService names podium-server's env files.
	ServerService = Service{"server", "server/.env", "/etc/podium/server.env"}
	// JudgeService names podium-judge's env files.
	JudgeService = Service{"judge", "judge/.env", "/etc/podium/judge.env"}
)

// Problems collects every configuration error so the operator fixes them in
// one pass instead of one restart per variable.
type Problems struct {
	Service Service
	List    []string
}

func (p *Problems) add(format string, args ...any) {
	p.List = append(p.List, fmt.Sprintf(format, args...))
}

// Error lists every problem and names the files to edit.
func (p *Problems) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "podium %s cannot start: configuration is incomplete.\n", p.Service.Name)
	for _, s := range p.List {
		fmt.Fprintf(&b, "  - %s\n", s)
	}
	fmt.Fprintf(&b, "Set these in %s (local) or %s (VM). See docs/OPERATIONS.md.", p.Service.LocalFile, p.Service.VMFile)
	return b.String()
}

func (p *Problems) err() error {
	if len(p.List) == 0 {
		return nil
	}
	return p
}

// reader resolves variables against their declarations and records problems.
type reader struct {
	get  Getenv
	vars map[string]Var
	p    *Problems
}

func newReader(get Getenv, svc Service, vars []Var) *reader {
	m := make(map[string]Var, len(vars))
	for _, v := range vars {
		m[v.Name] = v
	}
	return &reader{get: get, vars: m, p: &Problems{Service: svc}}
}

func (r *reader) str(name string) string {
	v, ok := r.vars[name]
	if !ok {
		panic("config: undeclared variable " + name)
	}
	s := strings.TrimSpace(r.get(name))
	if s == "" {
		if v.Required {
			r.p.add("%s is required but missing", name)
		}
		return v.Default
	}
	return s
}

func (r *reader) int(name string, min, max int) int {
	s := r.str(name)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		r.p.add("%s must be a whole number from %d to %d", name, min, max)
		return 0
	}
	return n
}

func (r *reader) level(name string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(r.str(name))); err != nil {
		r.p.add("%s must be one of debug, info, warn, error", name)
		return slog.LevelInfo
	}
	return l
}

func (r *reader) baseURL(name string) string {
	s := r.str(name)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.Contains(s, "change-me") {
		r.p.add("%s must be an absolute http or https URL such as https://contest.example.edu", name)
		return ""
	}
	// podium serves from the root of its host: the value is compared with
	// browsers' Origin header, which never carries a path.
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		r.p.add("%s must be just the scheme and host, such as https://contest.example.edu (no path); podium runs at the root of its own host", name)
		return ""
	}
	// Browsers send Origin without the default port and with the host in
	// ASCII (punycode), so normalise to that form or every write would be
	// refused as cross-site.
	host := strings.ToLower(u.Hostname())
	for _, c := range host {
		if c > 0x7e {
			r.p.add("%s must use the ASCII (punycode) form of the host name, such as https://xn--bcher-kva.example", name)
			return ""
		}
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

// Summary returns slog attributes reporting each variable as "set" or
// "missing" (with "default" when a default applies). Values never appear.
func Summary(get Getenv, vars []Var) []any {
	attrs := make([]any, 0, len(vars))
	for _, v := range vars {
		state := "set"
		if strings.TrimSpace(get(v.Name)) == "" {
			state = "missing"
			if v.Default != "" {
				state = "default"
			}
		}
		attrs = append(attrs, slog.String(v.Name, state))
	}
	return attrs
}

// LoadEnvFile reads KEY=VALUE lines from path into the process environment
// for local development (`podium-server -env-file server/.env`). Variables
// already present in the environment win. Blank lines and lines starting
// with # are ignored; one pair of surrounding quotes is stripped. A # later
// in a line is part of the value, as systemd's EnvironmentFile reads it. A
// leading UTF-8 byte order mark (written by Notepad) is ignored.
func LoadEnvFile(path string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		text := sc.Text()
		if n == 1 {
			text = strings.TrimPrefix(text, byteOrderMark)
		}
		line := strings.TrimSpace(text)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if !ok || key == "" {
			// The line may hold a secret, so report only its number.
			return fmt.Errorf("env file %s line %d: expected KEY=VALUE", path, n)
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, present := os.LookupEnv(key); !present {
			if err := os.Setenv(key, val); err != nil {
				return fmt.Errorf("env file %s line %d: %w", path, n, err)
			}
		}
	}
	return sc.Err()
}

// byteOrderMark is U+FEFF, which Notepad writes at the start of UTF-8 files.
var byteOrderMark = string(rune(0xFEFF))

// IsProblems reports whether err is a configuration error (exit code 2).
func IsProblems(err error) bool {
	var p *Problems
	return errors.As(err, &p)
}
