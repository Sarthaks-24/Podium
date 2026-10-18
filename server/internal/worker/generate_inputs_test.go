package worker

import (
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judge"
)

func TestWithArgsCopiesTheRunCommand(t *testing.T) {
	prog := judge.Program{Lang: judge.Languages["python3"], Dir: "/tmp/x"}
	gen := withArgs(prog, []string{"7", "n=100", "-5"})
	want := []string{"/usr/bin/python3", "main.py", "7", "n=100", "-5"}
	if got := gen.Lang.RunCommand(256); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("run command %q", got)
	}
	if len(prog.Lang.Run) != 2 || len(judge.Languages["python3"].Run) != 2 {
		t.Error("withArgs changed the shared language table")
	}
}

func TestTailKeepsTheEnd(t *testing.T) {
	s := strings.Repeat("a", 300) + "ValueError: bad"
	if got := tail(s, 20); got != "aaaaaValueError: bad" {
		t.Errorf("tail %q", got)
	}
	if got := tail("é"+strings.Repeat("b", 10), 11); got != strings.Repeat("b", 10) {
		t.Errorf("split rune: %q", got)
	}
}
