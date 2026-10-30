//go:build sandbox

// M2 acceptance on the real judge (PLAN.md section 19): import the example
// packs, validate them through the judge API and isolate, then break one
// expected output and check validation fails naming that test. Also checks
// that a hostile custom checker is contained. Linux only, as root dropping
// to the judge user: make sandbox-test.
package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"flag"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

var (
	examplesDir = flag.String("examples", "", "path to the repository's problems/ directory")
	isolatePath = flag.String("isolate", "/usr/bin/isolate", "isolate binary")
)

const testToken = "sandbox-test-token-sandbox-test-token-0001"

type rig struct {
	bank *problems.Service
	subs *submissions.Service
}

func newRig(t *testing.T) *rig {
	t.Helper()
	if *examplesDir == "" {
		t.Skip("pass -examples /path/to/problems")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	blobs := problems.Blobs{Dir: filepath.Join(t.TempDir(), "tests")}
	jobSvc := &jobs.Service{DB: d}
	jobSvc.Kinds = map[string]jobs.Kind{
		judgeapi.KindValidate:       problems.ValidateKind{},
		judgeapi.KindGenerate:       problems.GenerateKind{Store: blobs, Jobs: jobSvc},
		judgeapi.KindGenerateInputs: problems.GenerateInputsKind{Store: blobs, Jobs: jobSvc},
		judgeapi.KindSubmit:         submissions.SubmitKind{},
		judgeapi.KindRun:            submissions.RunKind{},
	}
	bank := &problems.Service{DB: d, Blobs: blobs, Jobs: jobSvc}
	subs := &submissions.Service{DB: d, Jobs: jobSvc, Problems: bank}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'setter@college.edu', 'admin', 0)`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewJudge(httpapi.JudgeDeps{Log: log, Jobs: jobSvc, Blobs: blobs, Token: testToken}))
	t.Cleanup(srv.Close)

	client := &Client{BaseURL: srv.URL, Token: testToken, HTTP: &http.Client{Timeout: time.Minute}}
	work := t.TempDir()
	os.MkdirAll(filepath.Join(work, "blobs"), 0o700)
	os.MkdirAll(filepath.Join(work, "jobs"), 0o700)
	os.MkdirAll(filepath.Join(work, "compiled"), 0o700)
	w := &Worker{
		Runner: &judge.Runner{Iso: judge.Isolate{Path: *isolatePath}},
		Client: client, Cache: &Cache{Dir: filepath.Join(work, "blobs"), Client: client},
		Compiled: &CompileCache{Dir: filepath.Join(work, "compiled")},
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Lanes:    2, WorkDir: filepath.Join(work, "jobs"), Name: "sandbox-test", Version: "test",
	}
	done := make(chan struct{})
	go func() { w.Drain(ctx, ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &rig{bank: bank, subs: subs}
}

func zipPack(t *testing.T, dir string, edit map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if s, ok := edit[rel]; ok {
			body = []byte(s)
		}
		fw, err := zw.Create(rel)
		if err != nil {
			return err
		}
		_, err = fw.Write(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

func (r *rig) importPack(t *testing.T, slug string, edit map[string]string) *problems.Problem {
	t.Helper()
	pk, err := problems.ReadPack(zipPack(t, filepath.Join(*examplesDir, slug), edit))
	if err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	p, err := r.bank.Import(context.Background(), pk, 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// validate queues a validation and waits for it to finish.
func (r *rig) validate(t *testing.T, id int64) *problems.Validation {
	t.Helper()
	ctx := context.Background()
	if _, err := r.bank.RequestValidation(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		v, err := r.bank.LatestValidation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		switch v.State {
		case problems.ValidationPassed, problems.ValidationFailed, problems.ValidationError:
			return v
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("validation did not finish in 4 minutes")
	return nil
}

func describe(v *problems.Validation) string {
	var b strings.Builder
	b.WriteString(v.State + " " + v.Error + "\n")
	for _, c := range v.Compile {
		b.WriteString("compile " + c.Role + " " + c.Verdict + "\n" + c.Output + "\n")
	}
	if v.Summary != nil {
		for _, p := range v.Summary.Problems {
			b.WriteString("problem: " + p + "\n")
		}
		for _, p := range v.Summary.Warnings {
			b.WriteString("warning: " + p + "\n")
		}
	}
	return b.String()
}

func TestExamplePacksValidate(t *testing.T) {
	r := newRig(t)
	for _, slug := range []string{"a-plus-b", "average-speed", "pair-with-sum"} {
		start := time.Now()
		p := r.importPack(t, slug, nil)
		v := r.validate(t, p.ID)
		if v.State != problems.ValidationPassed {
			t.Errorf("%s did not validate:\n%s", slug, describe(v))
			continue
		}
		got, _ := r.bank.Get(context.Background(), p.ID)
		if !got.Validated() {
			t.Errorf("%s passed but is not marked validated", slug)
		}
		t.Logf("%s: passed in %s; reference %d/%d, brute %d/%d, validator %d/%d, slowest %d of %d ms",
			slug, time.Since(start).Round(time.Millisecond), v.Summary.ReferencePassed, v.Summary.ReferenceTotal,
			v.Summary.BruteAgreed, v.Summary.BruteTotal, v.Summary.ValidatorPassed, v.Summary.ValidatorTotal,
			v.Summary.MaxTimeMS, v.Summary.TimeLimitMS)
	}
}

func TestBrokenOutputNamesTheTest(t *testing.T) {
	r := newRig(t)
	broken, _ := os.ReadFile(filepath.Join(*examplesDir, "a-plus-b", "tests", "05.out"))
	lines := strings.SplitN(string(broken), "\n", 2)
	lines[0] = lines[0] + "1" // one wrong digit on the first line
	p := r.importPack(t, "a-plus-b", map[string]string{"tests/05.out": strings.Join(lines, "\n")})
	v := r.validate(t, p.ID)
	if v.State != problems.ValidationFailed {
		t.Fatalf("broken output passed:\n%s", describe(v))
	}
	all := strings.Join(v.Summary.Problems, "\n")
	if !strings.Contains(all, "Test 5: reference solution got wrong answer.") || !strings.Contains(all, "Test 5: the brute force disagrees") {
		t.Errorf("failure does not name test 5:\n%s", describe(v))
	}
	for _, other := range []string{"Test 1:", "Test 3:", "Test 6:"} {
		if strings.Contains(all, other) {
			t.Errorf("an untouched test failed:\n%s", describe(v))
		}
	}
}

func TestHostileCheckerIsContained(t *testing.T) {
	r := newRig(t)
	for name, src := range map[string]string{
		"fork bomb":     "#include <unistd.h>\nint main(){ for(;;) fork(); }\n",
		"infinite loop": "int main(){ volatile int x = 0; for(;;) x++; }\n",
		"reads secrets": "#include <cstdio>\nint main(){ FILE* f = fopen(\"/etc/shadow\", \"r\"); return f ? 0 : 3; }\n",
	} {
		p := r.importPack(t, "pair-with-sum", map[string]string{"checker.cpp": src, "problem.yaml": strings.Replace(
			mustRead(t, filepath.Join(*examplesDir, "pair-with-sum", "problem.yaml")), "slug: pair-with-sum", "slug: hostile-"+strings.ReplaceAll(name, " ", "-"), 1)})
		v := r.validate(t, p.ID)
		if v.State != problems.ValidationFailed {
			t.Errorf("%s: checker should fail validation, got:\n%s", name, describe(v))
			continue
		}
		if !strings.Contains(strings.Join(v.Summary.Problems, "\n"), "reference solution got an internal error") {
			t.Errorf("%s: expected the checker failure to be reported:\n%s", name, describe(v))
		}
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
