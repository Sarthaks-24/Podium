package config

import (
	"strings"
	"testing"
)

// A CPU profile is written in testing mode only.
func TestCPUProfileSwitch(t *testing.T) {
	modeOnly := map[string]string{"PODIUM_MODE": "testing", "PODIUM_BASE_URL": "http://localhost:5173"}
	c, err := LoadServer(env(validServer(map[string]string{"PODIUM_MODE": "testing", "PODIUM_BASE_URL": "http://localhost:5173", "PODIUM_CPU_PROFILE": "run.pprof"})))
	if err != nil || c.CPUProfile != "run.pprof" {
		t.Fatalf("testing mode: %q %v", c.CPUProfile, err)
	}
	c, err = LoadServer(env(validServer(modeOnly)))
	if err != nil || c.CPUProfile != "" {
		t.Fatalf("unset: %q %v", c.CPUProfile, err)
	}
	for _, mode := range []string{"dev", "production"} {
		_, err := LoadServer(env(validServer(map[string]string{"PODIUM_MODE": mode, "PODIUM_CPU_PROFILE": "run.pprof"})))
		if err == nil || !strings.Contains(err.Error(), "PODIUM_CPU_PROFILE") || !strings.Contains(err.Error(), "testing mode") {
			t.Errorf("%s mode: error %v, want a message naming PODIUM_CPU_PROFILE and testing mode", mode, err)
		}
	}
}

func TestStandingsEncodeOnceSwitch(t *testing.T) {
	for in, want := range map[string]bool{"": false, "off": false, "0": false, "false": false, "on": true, "1": true, "true": true} {
		c, err := LoadServer(env(validServer(map[string]string{"PODIUM_STANDINGS_ENCODE_ONCE": in})))
		if err != nil || c.StandingsEncodeOnce != want {
			t.Errorf("%q: %v %v, want %v", in, c.StandingsEncodeOnce, err, want)
		}
	}
	_, err := LoadServer(env(validServer(map[string]string{"PODIUM_STANDINGS_ENCODE_ONCE": "maybe"})))
	if err == nil || !strings.Contains(err.Error(), "PODIUM_STANDINGS_ENCODE_ONCE") {
		t.Errorf("bad value: %v", err)
	}
}
