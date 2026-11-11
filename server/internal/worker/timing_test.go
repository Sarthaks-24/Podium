package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// TestJobTimingLine runs the compile path twice (a build, then a cache
// hit), records two tests and a checker run, and checks the one log line a
// finished job writes.
func TestJobTimingLine(t *testing.T) {
	src := []byte("int main(){}")
	sum := sha256.Sum256(src)
	sha := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/judge/blob/"+sha {
			w.Write(src)
			return
		}
		w.Write([]byte(`{"lease_seconds":60}`))
	}))
	defer srv.Close()
	client := &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	var logs bytes.Buffer
	w := &Worker{Client: client, Cache: &Cache{Dir: t.TempDir(), Client: client}, Compiled: &CompileCache{Dir: t.TempDir()},
		Log: slog.New(slog.NewJSONHandler(&logs, nil))}
	w.Compiled.build = func(_ context.Context, _ *judge.Runner, _ int, _ judge.Language, _ []byte, _ string) (judge.CompileResult, error) {
		return judge.CompileResult{Verdict: judge.OK, Wall: 900 * time.Millisecond,
			Timing: judge.StepTiming{LockWait: 300 * time.Millisecond, Setup: 20 * time.Millisecond, Cleanup: 10 * time.Millisecond}}, nil
	}
	claim := &judgeapi.Claim{JobID: 5, Kind: judgeapi.KindSubmit, Lease: "l"}
	rep := &reporter{client: client, jobID: 5, lease: "l"}
	rep.tm = jobTiming{kind: claim.Kind, language: "cpp17", lane: 1, started: time.Now().Add(-2 * time.Second)}
	lang := judge.Languages["cpp17"]
	held := &[]func(){}
	for i := 0; i < 2; i++ {
		if _, ok, err := w.compile(context.Background(), 1, Lease{5, "l"}, rep, judgeapi.RoleProgram,
			judgeapi.Program{Language: "cpp17", SourceSHA: sha}, lang, held); err != nil || !ok {
			t.Fatalf("compile %d: ok=%v %v", i, ok, err)
		}
	}
	step := judge.StepTiming{Setup: 5 * time.Millisecond, Cleanup: 7 * time.Millisecond}
	rep.tm.test(judge.ExecResult{Meta: judge.Meta{Time: 100 * time.Millisecond, WallTime: 120 * time.Millisecond}, Timing: step}, time.Second)
	rep.tm.test(judge.ExecResult{Meta: judge.Meta{Time: 400 * time.Millisecond, WallTime: 450 * time.Millisecond}, Timing: step}, time.Second)
	rep.tm.checker(judge.CheckerResult{Meta: judge.Meta{WallTime: 30 * time.Millisecond}})

	if err := w.finish(context.Background(), claim, rep, nil); err != nil {
		t.Fatal(err)
	}

	var line map[string]any
	for _, l := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(l, &m) == nil && m["msg"] == "job timing" {
			line = m
		}
	}
	if line == nil {
		t.Fatalf("no job timing line in %s", logs.String())
	}
	want := map[string]float64{
		"job_id": 5, "lane": 1, "compiles": 2, "compiles_cached": 1,
		"compile_wall_ms": 900, "lock_wait_ms": 300, "setup_ms": 30, "cleanup_ms": 24,
		"tests": 2, "tests_wall_ms": 570, "slowest_cpu_ms": 400, "slowest_wall_ms": 450, "slowest_limit_ms": 1000,
		"checker_runs": 1, "checker_wall_ms": 30,
	}
	for k, v := range want {
		if got, ok := line[k].(float64); !ok || got != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	if line["kind"] != judgeapi.KindSubmit || line["language"] != "cpp17" {
		t.Errorf("kind/language = %v/%v", line["kind"], line["language"])
	}
	for _, k := range []string{"total_ms", "report_ms"} {
		if _, ok := line[k].(float64); !ok {
			t.Errorf("%s missing", k)
		}
	}
	if total := line["total_ms"].(float64); total < 2000 {
		t.Errorf("total_ms = %v, want at least 2000", total)
	}
}

func TestOtherJobKindsWriteNoTimingLine(t *testing.T) {
	var logs bytes.Buffer
	w := &Worker{Log: slog.New(slog.NewJSONHandler(&logs, nil))}
	w.logTiming(&judgeapi.Claim{Kind: judgeapi.KindValidate}, &reporter{})
	if logs.Len() != 0 {
		t.Errorf("validate job wrote %s", logs.String())
	}
}
