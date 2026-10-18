package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

// validServer is a complete configuration with fake values.
func validServer(overrides map[string]string) map[string]string {
	m := map[string]string{
		"PODIUM_MODE":           "dev",
		"PODIUM_BASE_URL":       "https://contest.example.edu/",
		"PODIUM_OWNER_EMAIL":    "Owner@Example.edu",
		"PODIUM_SESSION_SECRET": strings.Repeat("s", 40),
		"GOOGLE_CLIENT_ID":      "123-test.apps.googleusercontent.com",
		"GOOGLE_CLIENT_SECRET":  "fake-test-secret",
		"PODIUM_JUDGE_TOKEN":    strings.Repeat("j", 40),
	}
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

func TestLoadServerDefaults(t *testing.T) {
	c, err := LoadServer(env(validServer(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://contest.example.edu" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed", c.BaseURL)
	}
	if c.Listen != "127.0.0.1:8080" || c.DBPath != "data/podium.db" || c.LogLevel != slog.LevelInfo {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.OwnerEmail != "owner@example.edu" {
		t.Errorf("OwnerEmail = %q, want lowercased", c.OwnerEmail)
	}
}

func TestBaseURLNormalisedToOriginForm(t *testing.T) {
	for in, want := range map[string]string{
		"https://Contest.Example.edu:443/": "https://contest.example.edu",
		"http://localhost:5173":            "http://localhost:5173",
		"http://localhost:80":              "http://localhost",
		"https://x.edu:8443":               "https://x.edu:8443",
	} {
		c, err := LoadServer(env(validServer(map[string]string{"PODIUM_BASE_URL": in})))
		if err != nil || c.BaseURL != want {
			t.Errorf("%q -> %q (%v), want %q", in, c.BaseURL, err, want)
		}
	}
	if _, err := LoadServer(env(validServer(map[string]string{"PODIUM_BASE_URL": "https://bücher.example"}))); err == nil {
		t.Error("non-ASCII host accepted")
	}
}

func TestLoadServerDomainsAndSecrets(t *testing.T) {
	c, err := LoadServer(env(validServer(map[string]string{"PODIUM_ALLOWED_DOMAINS": " @College.edu, cs.college.edu ,"})))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.AllowedDomains, ",") != "college.edu,cs.college.edu" {
		t.Errorf("AllowedDomains = %v", c.AllowedDomains)
	}
	bad := []map[string]string{
		{"PODIUM_ALLOWED_DOMAINS": "college"},
		{"PODIUM_SESSION_SECRET": "short"},
		{"PODIUM_SESSION_SECRET": "change-me-change-me-change-me-change-me"},
		{"PODIUM_OWNER_EMAIL": "not-an-email"},
		{"PODIUM_OWNER_EMAIL": "Owner <o@x.edu>"},
		{"GOOGLE_CLIENT_ID": "your-google-client-id"},
		{"GOOGLE_CLIENT_SECRET": "change-me"},
	}
	for _, o := range bad {
		_, err := LoadServer(env(validServer(o)))
		if err == nil {
			t.Errorf("%v accepted, want rejection", o)
			continue
		}
		for _, v := range o {
			if strings.Contains(err.Error(), v) && v != "college" {
				t.Errorf("error message echoes the value %q: %s", v, err)
			}
		}
	}
}

func TestNormaliseDomain(t *testing.T) {
	for in, want := range map[string]string{"@Example.EDU": "example.edu", " cs.uni.ac.in ": "cs.uni.ac.in"} {
		if got, ok := NormaliseDomain(in); !ok || got != want {
			t.Errorf("NormaliseDomain(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "localhost", "a..b.com", "-a.com", "a b.com", "x.c"} {
		if _, ok := NormaliseDomain(in); ok {
			t.Errorf("NormaliseDomain(%q) accepted", in)
		}
	}
}

func TestLoadServerListsEveryProblem(t *testing.T) {
	_, err := LoadServer(env(map[string]string{"PODIUM_LOG_LEVEL": "loud"}))
	if err == nil || !IsProblems(err) {
		t.Fatalf("want a Problems error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"PODIUM_BASE_URL is required", "PODIUM_LOG_LEVEL must be one of", "GOOGLE_CLIENT_SECRET is required",
		"PODIUM_SESSION_SECRET is required", "PODIUM_OWNER_EMAIL is required", "server/.env", "/etc/podium/server.env"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}

func TestLoadServerRejectsPlaceholderURL(t *testing.T) {
	for _, u := range []string{"https://change-me.example.edu", "contest.example.edu", "ftp://x",
		"https://x.edu/podium", "https://x.edu/?a=b", "https://user:pw@x.edu"} {
		if _, err := LoadServer(env(validServer(map[string]string{"PODIUM_BASE_URL": u}))); err == nil {
			t.Errorf("%q accepted, want rejection", u)
		}
	}
}

func TestLoadJudgeLaneRange(t *testing.T) {
	if _, err := LoadJudge(env(map[string]string{"PODIUM_JUDGE_LANES": "0"})); err == nil {
		t.Error("0 lanes accepted")
	}
	c, err := LoadJudge(env(nil))
	if err != nil || c.Lanes != 2 {
		t.Errorf("default lanes: %v %+v", err, c)
	}
}

func TestSummaryNeverPrintsValues(t *testing.T) {
	vars := []Var{{Name: "A_SECRET", Secret: true}, {Name: "B", Default: "x"}, {Name: "C"}}
	got := Summary(env(map[string]string{"A_SECRET": "hunter2"}), vars)
	var b strings.Builder
	for _, a := range got {
		b.WriteString(a.(slog.Attr).String() + " ")
	}
	s := b.String()
	if strings.Contains(s, "hunter2") {
		t.Fatalf("summary leaked a value: %s", s)
	}
	if !strings.Contains(s, "A_SECRET=set") || !strings.Contains(s, "B=default") || !strings.Contains(s, "C=missing") {
		t.Errorf("unexpected summary: %s", s)
	}
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.env")
	content := byteOrderMark + "PODIUM_T_BOM=first\n# comment\n\nPODIUM_T_ONE=\"quoted value\"\nexport PODIUM_T_TWO=plain\nPODIUM_T_KEEP=from-file\n"
	os.Unsetenv("PODIUM_T_BOM")
	t.Cleanup(func() { os.Unsetenv("PODIUM_T_BOM") })
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PODIUM_T_KEEP", "from-env")
	t.Setenv("PODIUM_T_ONE", "")
	os.Unsetenv("PODIUM_T_ONE")
	t.Setenv("PODIUM_T_TWO", "")
	os.Unsetenv("PODIUM_T_TWO")
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if v := os.Getenv("PODIUM_T_BOM"); v != "first" {
		t.Errorf("a byte order mark hid the first key: PODIUM_T_BOM = %q", v)
	}
	if v := os.Getenv("PODIUM_T_ONE"); v != "quoted value" {
		t.Errorf("PODIUM_T_ONE = %q", v)
	}
	if v := os.Getenv("PODIUM_T_TWO"); v != "plain" {
		t.Errorf("PODIUM_T_TWO = %q", v)
	}
	if v := os.Getenv("PODIUM_T_KEEP"); v != "from-env" {
		t.Errorf("environment should win, got %q", v)
	}
}

func TestLoadEnvFileErrorHidesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(path, []byte("this-is-a-secret-without-equals\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := LoadEnvFile(path)
	if err == nil || strings.Contains(err.Error(), "secret-without") {
		t.Fatalf("want an error that hides the line content, got %v", err)
	}
}
