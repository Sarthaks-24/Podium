package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/worker"
)

// The first signal drains (stops claiming), the second abandons the
// running jobs, and nothing happens without a signal (C1-027).
func TestStopOnSignals(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sigs := make(chan os.Signal, 2)
	done := make(chan struct{})
	stopped, abandoned := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	go func() {
		stopOnSignals(sigs, done, func() { close(stopped) }, func() { close(abandoned) }, log)
		close(finished)
	}()
	wait := func(c chan struct{}, what string) {
		t.Helper()
		select {
		case <-c:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not happen", what)
		}
	}
	select {
	case <-stopped:
		t.Fatal("stopped claiming without a signal")
	case <-time.After(50 * time.Millisecond):
	}
	sigs <- syscall.SIGTERM
	wait(stopped, "the drain after the first signal")
	select {
	case <-abandoned:
		t.Fatal("one signal abandoned the running jobs")
	case <-time.After(50 * time.Millisecond):
	}
	sigs <- syscall.SIGTERM
	wait(abandoned, "abandoning after the second signal")
	wait(finished, "the return after the second signal")

	// A normal end (done closes) returns without touching either.
	done2 := make(chan struct{})
	ret := make(chan struct{})
	go func() {
		stopOnSignals(make(chan os.Signal), done2, func() { t.Error("stopClaims on a normal end") }, func() { t.Error("abandon on a normal end") }, log)
		close(ret)
	}()
	close(done2)
	wait(ret, "the return on a normal end")
}

// fail exits 3 on a protocol mismatch and 4 on a refused token, so
// systemd's RestartPreventExitStatus=3 4 keeps such a judge down and its
// unit fails and alerts (C1-027, C1-048). The exit is checked in a child
// process.
func TestFailExitsThreeOnProtocolMismatch(t *testing.T) {
	if os.Getenv("PODIUM_JUDGE_TEST_CHILD") == "protocol" {
		fail(&worker.ProtocolError{Message: "judge protocol 4 needed, this judge speaks 3"})
		return
	}
	if os.Getenv("PODIUM_JUDGE_TEST_CHILD") == "auth" {
		fail(&worker.AuthError{Op: "claim"})
		return
	}
	if os.Getenv("PODIUM_JUDGE_TEST_CHILD") == "other" {
		fail(errors.New("the server is away"))
		return
	}
	for _, c := range []struct {
		child string
		want  int
	}{{"protocol", exitProtocol}, {"auth", exitAuth}, {"other", 1}} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFailExitsThreeOnProtocolMismatch$")
		cmd.Env = append(os.Environ(), "PODIUM_JUDGE_TEST_CHILD="+c.child)
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != c.want {
			t.Errorf("%s: %v, want exit %d", c.child, err, c.want)
		}
	}
}
