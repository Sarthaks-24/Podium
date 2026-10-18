package config

import (
	"strings"
	"testing"
)

func TestProductionListensOnLoopbackOnly(t *testing.T) {
	cases := []struct {
		mode   Mode
		listen string
		ok     bool
	}{
		{ModeProduction, "127.0.0.1:8080", true},
		{ModeProduction, "[::1]:8080", true},
		{ModeProduction, "0.0.0.0:8080", false},
		{ModeProduction, ":8080", false},
		{ModeProduction, "10.0.0.5:8080", false},
		{ModeDev, "0.0.0.0:8080", true},
	}
	for _, c := range cases {
		err := CheckPublicListen(Server{Mode: c.mode, Listen: c.listen})
		if (err == nil) != c.ok {
			t.Errorf("%s %q: %v, want ok=%v", c.mode, c.listen, err, c.ok)
		}
		if err != nil && (!strings.Contains(err.Error(), "PODIUM_LISTEN must be a loopback address") || !strings.Contains(err.Error(), "/etc/podium/server.env")) {
			t.Errorf("unclear error: %v", err)
		}
		if err != nil && !IsProblems(err) {
			t.Errorf("not a configuration error (exit status 2): %v", err)
		}
	}
}
