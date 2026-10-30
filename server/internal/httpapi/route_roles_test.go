package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// routeRoles is who may call each route: "public" (no sign-in),
// "student", "organiser" or "owner" (that role or above), or "judge" (the
// worker token on the judge listener). Routes registered only in testing
// mode carry the suffix " testing".
var routeRoles = map[string]string{
	"GET /healthz":                                                 "public",
	"GET /api/auth/config":                                         "public",
	"POST /api/auth/test-signin":                                   "public testing",
	"GET /api/auth/google/start":                                   "public",
	"GET /api/auth/google/callback":                                "public",
	"POST /api/auth/logout":                                        "public",
	"GET /api/me":                                                  "student",
	"PUT /api/me/preferences":                                      "student",
	"GET /api/submissions/{id}":                                    "student",
	"GET /api/runs/{id}":                                           "student",
	"GET /api/events":                                              "student",
	"GET /api/contests":                                            "student",
	"GET /api/contests/{slug}":                                     "student",
	"POST /api/contests/{slug}/start":                              "student",
	"GET /api/contests/{slug}/problems/{label}":                    "student",
	"PUT /api/contests/{slug}/problems/{label}/draft":              "student",
	"POST /api/contests/{slug}/problems/{label}/run":               "student",
	"POST /api/contests/{slug}/problems/{label}/submit":            "student",
	"POST /api/contests/{slug}/problems/{label}/events":            "student",
	"GET /api/contests/{slug}/standings":                           "student",
	"POST /api/contests/{slug}/clarifications":                     "student",
	"GET /api/admin/people":                                        "owner",
	"POST /api/admin/people":                                       "owner",
	"DELETE /api/admin/people/{id}":                                "owner",
	"POST /api/admin/accounts/reset-google":                        "owner",
	"POST /api/admin/accounts/end-sessions":                        "owner",
	"POST /api/admin/accounts/disable":                             "owner",
	"POST /api/admin/accounts/enable":                              "owner",
	"GET /api/admin/accounts":                                      "organiser",
	"POST /api/admin/domains":                                      "owner",
	"DELETE /api/admin/domains/{domain}":                           "owner",
	"GET /api/admin/site-audit":                                    "owner",
	"PUT /api/admin/site-settings":                                 "owner",
	"GET /api/admin/metrics":                                       "organiser",
	"GET /api/admin/ai/providers":                                  "organiser",
	"GET /api/admin/ai/models":                                     "organiser",
	"GET /api/admin/ai/key":                                        "organiser",
	"PUT /api/admin/ai/key":                                        "organiser",
	"DELETE /api/admin/ai/key":                                     "organiser",
	"POST /api/admin/ai/wizard":                                    "organiser",
	"GET /api/admin/ai/wizard/{id}":                                "organiser",
	"GET /api/admin/problems":                                      "organiser",
	"POST /api/admin/problems":                                     "organiser",
	"POST /api/admin/problems/import":                              "organiser",
	"GET /api/admin/problems/{id}":                                 "organiser",
	"PUT /api/admin/problems/{id}":                                 "organiser",
	"DELETE /api/admin/problems/{id}":                              "organiser",
	"PUT /api/admin/problems/{id}/tests":                           "organiser",
	"GET /api/admin/problems/{id}/tests/{idx}/{part}":              "organiser",
	"GET /api/admin/problems/{id}/export":                          "organiser",
	"POST /api/admin/problems/{id}/validate":                       "organiser",
	"GET /api/admin/problems/{id}/validation":                      "organiser",
	"POST /api/admin/problems/{id}/generate":                       "organiser",
	"GET /api/admin/problems/{id}/generation":                      "organiser",
	"POST /api/admin/problems/{id}/generate-inputs":                "organiser",
	"GET /api/admin/problems/{id}/input-generation":                "organiser",
	"GET /api/workspace/{problem}":                                 "organiser",
	"PUT /api/workspace/{problem}/draft":                           "organiser",
	"POST /api/workspace/{problem}/run":                            "organiser",
	"POST /api/workspace/{problem}/submit":                         "organiser",
	"POST /api/workspace/{problem}/events":                         "organiser",
	"GET /api/admin/contests":                                      "organiser",
	"POST /api/admin/contests":                                     "organiser",
	"GET /api/admin/contests/{id}":                                 "organiser",
	"PUT /api/admin/contests/{id}":                                 "organiser",
	"DELETE /api/admin/contests/{id}":                              "organiser",
	"POST /api/admin/contests/{id}/clone":                          "organiser",
	"PUT /api/admin/contests/{id}/problems":                        "organiser",
	"PUT /api/admin/contests/{id}/problems/{label}/revision":       "organiser",
	"PUT /api/admin/contests/{id}/extensions/{user}":               "organiser",
	"POST /api/admin/contests/{id}/extend":                         "organiser",
	"POST /api/admin/contests/{id}/pause":                          "organiser",
	"POST /api/admin/contests/{id}/resume":                         "organiser",
	"GET /api/admin/contests/{id}/submissions":                     "organiser",
	"POST /api/admin/contests/{id}/rejudge":                        "organiser",
	"GET /api/admin/contests/{id}/announcements":                   "organiser",
	"POST /api/admin/contests/{id}/announcements":                  "organiser",
	"GET /api/admin/contests/{id}/clarifications":                  "organiser",
	"PUT /api/admin/contests/{id}/clarifications/{cid}":            "organiser",
	"GET /api/admin/contests/{id}/standings":                       "organiser",
	"GET /api/admin/contests/{id}/reveal":                          "organiser",
	"POST /api/admin/contests/{id}/reveal":                         "organiser",
	"GET /api/admin/contests/{id}/export.csv":                      "organiser",
	"GET /api/admin/contests/{id}/export.json":                     "organiser",
	"GET /api/admin/contests/{id}/stats":                           "organiser",
	"PUT /api/admin/contests/{id}/editorial":                       "organiser",
	"GET /api/admin/contests/{id}/audit":                           "organiser",
	"POST /api/admin/contests/{id}/integrity/run":                  "organiser",
	"GET /api/admin/contests/{id}/integrity":                       "organiser",
	"GET /api/admin/contests/{id}/integrity/pairs.csv":             "organiser",
	"GET /api/admin/contests/{id}/integrity/signals/{signal}/pair": "organiser",
	"GET /api/admin/contests/{id}/integrity/timeline/{user}":       "organiser",
	"GET /api/admin/contests/{id}/integrity/counts":                "organiser",
	"GET /api/admin/contests/{id}/integrity/cases":                 "organiser",
	"PUT /api/admin/contests/{id}/integrity/cases/{user}":          "organiser",
	"POST /api/judge/claim":                                        "judge",
	"POST /api/judge/toolchain":                                    "judge",
	"GET /api/judge/blob/{sha}":                                    "judge",
	"PUT /api/judge/blob/{sha}":                                    "judge",
	"POST /api/judge/report":                                       "judge",
}

// routeFiles are where routes are registered: this package's route table
// and the judge worker API on its own listener.
var routeFiles = []string{"*.go", "../jobs/judgeserver/*.go"}

// registeredRoutes reads every "METHOD path" pattern the production files
// in routeFiles register, so a new route without a row in routeRoles fails
// the test.
func registeredRoutes(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range routeFiles {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matched...)
	}
	fset := token.NewFileSet()
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			pattern, _ := strconv.Unquote(lit.Value)
			if strings.Contains(pattern, " ") {
				out = append(out, pattern)
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

var pathParam = regexp.MustCompile(`\{[a-z]+\}`)

// probePath fills a route's parameters with values that name nothing.
func probePath(pattern string) string {
	return pathParam.ReplaceAllStringFunc(pattern, func(p string) string {
		switch p {
		case "{slug}":
			return "no-such-contest"
		case "{label}":
			return "A"
		case "{part}":
			return "input"
		case "{domain}":
			return "example.org"
		case "{sha}":
			return strings.Repeat("0", 64)
		}
		return "999999"
	})
}

// TestRouteRoles probes every route as signed out, student, admin and
// owner, and checks that exactly the roles in routeRoles get past the
// role check: below the role a signed-out caller gets 401 signed_out and a
// signed-in one 403 forbidden; at or above it neither.
func TestRouteRoles(t *testing.T) {
	routes := registeredRoutes(t)
	for _, r := range routes {
		if _, ok := routeRoles[r]; !ok {
			t.Errorf("route %q has no row in routeRoles", r)
		}
	}
	seen := map[string]bool{}
	for _, r := range routes {
		seen[r] = true
	}
	for r := range routeRoles {
		if !seen[r] {
			t.Errorf("routeRoles lists %q, which is not registered", r)
		}
	}

	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, body := owner.do("POST", "/api/admin/people", map[string]any{"email": "admin@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("make admin: %d %v", code, body)
	}
	callers := []struct {
		name string
		rank int
		b    *browser
	}{
		{"signed out", 0, s.browser()},
		{"student", 1, s.signedIn("asha@college.edu", "Asha")},
		{"organiser", 2, s.signedIn("admin@college.edu", "Admin")},
		{"owner", 3, owner},
	}
	ranks := map[string]int{"public": 0, "student": 1, "organiser": 2, "owner": 3}

	// Signing out ends the caller's session, so it goes last.
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i] != "POST /api/auth/logout" && routes[j] == "POST /api/auth/logout"
	})
	for _, route := range routes {
		role := routeRoles[route]
		method, pattern, _ := strings.Cut(route, " ")
		path := probePath(pattern)
		if role == "judge" {
			code, _ := probe(t, http.DefaultClient, s.judge.URL, method, path, "")
			if code != http.StatusUnauthorized {
				t.Errorf("%s without the judge token: %d, want 401", route, code)
			}
			t.Logf("%-70s judge", route)
			continue
		}
		if strings.HasSuffix(role, " testing") {
			// Not registered in production: the catch-all answers.
			code, _ := probe(t, http.DefaultClient, s.srv.URL, method, path, s.srv.URL)
			if code != http.StatusNotFound {
				t.Errorf("%s in production mode: %d, want 404", route, code)
			}
			t.Logf("%-70s %s", route, role)
			continue
		}
		need := ranks[role]
		for _, c := range callers {
			client := &http.Client{Jar: c.b.client.Jar, Timeout: 10 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			code, errCode := probe(t, client, s.srv.URL, method, path, s.srv.URL)
			refused := code == http.StatusUnauthorized || (code == http.StatusForbidden && errCode == "forbidden")
			switch {
			case c.rank >= need && refused:
				t.Errorf("%s as %s: %d %s, want past the role check", route, c.name, code, errCode)
			case c.rank < need && c.rank == 0 && (code != http.StatusUnauthorized || errCode != "signed_out"):
				t.Errorf("%s as %s: %d %s, want 401 signed_out", route, c.name, code, errCode)
			case c.rank < need && c.rank > 0 && (code != http.StatusForbidden || errCode != "forbidden"):
				t.Errorf("%s as %s: %d %s, want 403 forbidden", route, c.name, code, errCode)
			}
		}
		t.Logf("%-70s %s", route, role)
	}
	t.Logf("%d routes", len(routes))
}

// probe sends an empty JSON body and returns the status and the error
// code, if any. It reads only the headers of an event stream.
func probe(t *testing.T, client *http.Client, base, method, path, origin string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var body io.Reader
	if method != http.MethodGet {
		body = bytes.NewReader([]byte("{}"))
	}
	req, _ := http.NewRequestWithContext(ctx, method, base+path, body)
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp.StatusCode, ""
	}
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return resp.StatusCode, out.Error.Code
}
