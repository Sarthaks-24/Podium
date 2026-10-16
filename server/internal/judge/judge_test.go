package judge

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunArgs(t *testing.T) {
	req := RunRequest{
		Box:    1,
		Limits: Limits{CPU: 2 * time.Second, MemoryKB: 262144, Processes: 1},
		Args:   []string{"./main", "--flag"},
		Stdin:  "input.txt", Stdout: "output.txt", Stderr: "stderr.txt",
		Env:  []string{"PATH=/usr/bin:/bin"},
		Dirs: []string{"/etc/java-21-openjdk:maybe"},
	}
	got := RunArgs(req, "/tmp/meta")
	want := []string{
		"--cg", "--silent", "--box-id=1",
		"--time=2.000", "--wall-time=5.000", "--extra-time=0.500",
		"--cg-mem=262144", "--fsize=16384", "--processes=1",
		"--meta=/tmp/meta",
		"--stdin=input.txt", "--stdout=output.txt", "--stderr=stderr.txt",
		"--dir=/etc/java-21-openjdk:maybe",
		"--env=PATH=/usr/bin:/bin",
		"--run", "--", "./main", "--flag",
	}
	if !slices.Equal(got, want) {
		t.Errorf("RunArgs:\n got %q\nwant %q", got, want)
	}
}

func TestRunArgsNeverSharesNetwork(t *testing.T) {
	for _, l := range Languages {
		args := RunArgs(RunRequest{Args: l.RunCommand(256), Dirs: l.Dirs, Env: sandboxEnv, Limits: l.LimitsFor(time.Second, 256)}, "m")
		for _, a := range args {
			if a == "--share-net" || a == "--full-env" || a == "--inherit-fds" || strings.HasPrefix(a, "--dir=/etc:") || a == "--dir=/etc" {
				t.Errorf("%s: forbidden isolate flag %q", l.ID, a)
			}
		}
	}
}

func TestParseMeta(t *testing.T) {
	m, err := ParseMeta(strings.NewReader("time:1.234\ntime-wall:1.500\nmax-rss:2048\ncg-mem:4096\ncg-oom-killed:1\nexitsig:9\nkilled:1\nstatus:SG\nmessage:Caught fatal signal 9\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Time != 1234*time.Millisecond || m.WallTime != 1500*time.Millisecond || m.MaxRSSKB != 2048 || m.CgMemKB != 4096 ||
		!m.OOMKilled || m.ExitSig != 9 || !m.Killed || m.Status != "SG" || m.Message != "Caught fatal signal 9" {
		t.Errorf("unexpected meta: %+v", m)
	}
	if m.MemoryKB() != 4096 {
		t.Errorf("MemoryKB = %d", m.MemoryKB())
	}
}

func TestClassify(t *testing.T) {
	cpp, java := Languages["cpp17"], Languages["java21"]
	lim := Limits{CPU: time.Second}
	cases := []struct {
		name   string
		m      Meta
		lang   Language
		stderr string
		want   Verdict
	}{
		{"clean exit", Meta{Time: 100 * time.Millisecond}, cpp, "", OK},
		{"timeout", Meta{Status: "TO", Killed: true}, cpp, "", TimeLimit},
		{"over cpu inside extra time", Meta{Time: 1200 * time.Millisecond}, cpp, "", TimeLimit},
		{"oom kill wins over signal", Meta{Status: "SG", ExitSig: 9, OOMKilled: true}, cpp, "", MemoryLimit},
		{"output limit", Meta{Status: "SG", ExitSig: 25}, cpp, "", OutputLimit},
		{"segfault", Meta{Status: "SG", ExitSig: 11}, cpp, "", RuntimeError},
		{"non-zero exit", Meta{Status: "RE", ExitCode: 1}, cpp, "", RuntimeError},
		{"java heap", Meta{Status: "RE", ExitCode: 1}, java, "Exception in thread \"main\" java.lang.OutOfMemoryError: Java heap space", MemoryLimit},
		{"sandbox failure", Meta{Status: "XX"}, cpp, "", InternalError},
	}
	for _, c := range cases {
		if got := Classify(c.m, lim, c.lang, []byte(c.stderr)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestLimitsFor(t *testing.T) {
	l := Languages["java21"].LimitsFor(time.Second, 256)
	if l.CPU != 2*time.Second || l.MemoryKB != (256+256)*1024 || l.Processes != 64 {
		t.Errorf("java limits: %+v", l)
	}
	if got := Languages["java21"].RunCommand(256); !slices.Contains(got, "-Xmx256m") {
		t.Errorf("java run command lacks -Xmx256m: %q", got)
	}
}

func TestCheckers(t *testing.T) {
	cases := []struct {
		kind     CheckerType
		exp, act string
		ok       bool
		msg      string
	}{
		{CheckTokens, "1 2 3\n", "1  2\n3", true, ""},
		{CheckTokens, "1 2 3", "1 2 4", false, "Token 3 differs"},
		{CheckTokens, "1 2 3", "1 2", false, "Output ended early"},
		{CheckTokens, "1 2", "1 2 3", false, "Extra output"},
		{CheckTokens, "", "\n\n", true, ""},
		{CheckExact, "a b\r\nc\r\n", "a b\nc", true, ""},
		{CheckExact, "a b\nc\n", "a  b\nc\n", false, "line 1"},
		{CheckExact, "x\ny\n", "x\nz\n", false, "line 2"},
		{CheckFloat, "0.333333 yes", "0.3333331 yes", true, ""},
		{CheckFloat, "1000000", "1000000.5", true, ""},
		{CheckFloat, "1.5", "1.6", false, "Token 1 differs"},
		{CheckFloat, "1.5", "nan", false, ""},
		{CheckFloat, "yes", "YES", false, ""},
	}
	for _, c := range cases {
		r, err := Check(c.kind, strings.NewReader(c.exp), strings.NewReader(c.act), 0)
		if err != nil {
			t.Fatalf("%s %q vs %q: %v", c.kind, c.exp, c.act, err)
		}
		if r.OK != c.ok || !strings.Contains(r.Message, c.msg) {
			t.Errorf("%s %q vs %q: got %+v, want ok=%v containing %q", c.kind, c.exp, c.act, r, c.ok, c.msg)
		}
	}
}

func TestExactCheckerStreaming(t *testing.T) {
	cases := []struct {
		exp, act string
		ok       bool
	}{
		{"a\n\nb\n", "a\n\nb", true},
		{"a\n\nb\n", "a\nb\n", false},
		{"a\r\n\r\n", "a", true},
		{"a\rb", "a\rb\n", true},
		{"a\rb", "a\nb", false},
		{"\n\n", "", true},
		{"x", "x\n\n\ny", false},
	}
	for _, c := range cases {
		r, err := Check(CheckExact, strings.NewReader(c.exp), strings.NewReader(c.act), 0)
		if err != nil || r.OK != c.ok {
			t.Errorf("exact %q vs %q: got %+v %v, want ok=%v", c.exp, c.act, r, err, c.ok)
		}
	}
}

func TestOverlongStudentTokenIsWrongAnswer(t *testing.T) {
	long := strings.Repeat("x", maxToken+1)
	r, err := Check(CheckTokens, strings.NewReader("x"), strings.NewReader(long), 0)
	if err != nil || r.OK || !strings.Contains(r.Message, "longer than") {
		t.Fatalf("got %+v, %v; want a wrong answer, not an error", r, err)
	}
}

func TestMemoryBudget(t *testing.T) {
	r := &Runner{MemoryBudgetKB: 1000}
	ctx := context.Background()
	rel1, err := r.reserve(ctx, 600)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		rel2, err := r.reserve(ctx, 600)
		if err == nil {
			rel2()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("second reservation should wait while the budget is used")
	case <-time.After(50 * time.Millisecond):
	}
	rel1()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("second reservation never proceeded")
	}

	// A request larger than the budget takes all of it rather than blocking forever.
	relAll, err := r.reserve(ctx, 5000)
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := r.reserve(cctx, 1); err == nil {
		t.Fatal("reservation should fail when its context ends while waiting")
	}
	relAll()
}

func TestCheckerMessagesAreShort(t *testing.T) {
	long := strings.Repeat("x", 10000)
	r, _ := Check(CheckTokens, strings.NewReader(long), strings.NewReader(long+"y"), 0)
	if len(r.Message) > 200 {
		t.Errorf("message too long: %d bytes", len(r.Message))
	}
}
