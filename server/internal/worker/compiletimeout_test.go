package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// storeTimedOut puts a compile that ran out of time into the cache, as
// Compile would after the compiler hit its limit.
func storeTimedOut(t *testing.T, c *CompileCache, lang judge.Language, src []byte) string {
	t.Helper()
	tmp, err := os.MkdirTemp(c.Dir, ".build-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(tmp, "bin"), 0o700)
	dir := filepath.Join(c.Dir, compileKey(lang, src))
	res := judge.CompileResult{Verdict: judge.CompileError, Output: "Compilation took longer than 10 seconds.", Transient: true}
	if moved, err := c.store(tmp, dir, res); err != nil || !moved {
		t.Fatalf("store: moved=%v %v", moved, err)
	}
	return dir
}

func TestTimedOutCompileIsCachedBriefly(t *testing.T) {
	now := time.Now()
	c := &CompileCache{Dir: t.TempDir(), now: func() time.Time { return now }}
	lang := judge.Languages["cpp17"]
	dir := storeTimedOut(t, c, lang, []byte("slow"))

	now = now.Add(transientTTL - time.Minute)
	res, ok := c.load(dir)
	if !ok || res.Verdict != judge.CompileError || !res.Transient || !strings.Contains(res.Output, "longer than") {
		t.Fatalf("within the TTL: ok=%v %+v", ok, res)
	}

	now = now.Add(time.Minute)
	if _, ok := c.load(dir); ok {
		t.Fatal("a timed-out compile was still cached after the TTL")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the expired entry was left in place")
	}
	// The key is free for a new build.
	tmp, _ := os.MkdirTemp(c.Dir, ".build-*")
	if moved, err := c.store(tmp, dir, judge.CompileResult{Verdict: judge.OK}); err != nil || !moved {
		t.Fatalf("rebuild after expiry: moved=%v %v", moved, err)
	}

	// An ordinary result does not expire.
	now = now.Add(24 * time.Hour)
	if res, ok := c.load(dir); !ok || res.Verdict != judge.OK || res.Transient {
		t.Errorf("ordinary entry after a day: ok=%v %+v", ok, res)
	}
}

func TestCompileReportCarriesTimedOut(t *testing.T) {
	src := []byte("template<int N> struct F { enum { v = F<N-1>::v }; };")
	sum := sha256.Sum256(src)
	sha := hex.EncodeToString(sum[:])
	var mu sync.Mutex
	var reports []judgeapi.Report
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/judge/blob/"+sha:
			w.Write(src)
		case r.URL.Path == "/api/judge/report":
			var rep judgeapi.Report
			json.NewDecoder(r.Body).Decode(&rep)
			mu.Lock()
			reports = append(reports, rep)
			mu.Unlock()
			w.Write([]byte(`{"lease_seconds":60}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	lang := judge.Languages["cpp17"]
	w := &Worker{Client: client, Cache: &Cache{Dir: t.TempDir(), Client: client}, Compiled: &CompileCache{Dir: t.TempDir()}}
	storeTimedOut(t, w.Compiled, lang, src)

	rep := &reporter{client: client, jobID: 7, lease: "l"}
	held := &[]func(){}
	prog, ok, err := w.compile(context.Background(), 0, Lease{7, "l"}, rep, judgeapi.RoleProgram,
		judgeapi.Program{Language: "cpp17", SourceSHA: sha}, lang, held)
	if err != nil || ok || prog != nil {
		t.Fatalf("compile: prog=%v ok=%v %v", prog, ok, err)
	}
	if len(reports) != 1 || len(reports[0].Compile) != 1 {
		t.Fatalf("reports: %+v", reports)
	}
	if c := reports[0].Compile[0]; c.Verdict != "CE" || !c.TimedOut || c.Role != judgeapi.RoleProgram {
		t.Errorf("compile report %+v, want CE with timed_out", c)
	}
}
