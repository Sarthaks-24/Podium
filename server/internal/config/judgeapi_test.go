package config

import (
	"strings"
	"testing"
)

func TestJudgeAPISettings(t *testing.T) {
	c, err := LoadServer(env(validServer(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.JudgeListen != "127.0.0.1:8081" || c.JudgeToken != strings.Repeat("j", 40) {
		t.Errorf("judge settings: listen %q, token set %v", c.JudgeListen, c.JudgeToken != "")
	}
	for name, bad := range map[string]map[string]string{
		"short token":       {"PODIUM_JUDGE_TOKEN": "short"},
		"placeholder token": {"PODIUM_JUDGE_TOKEN": "change-me-change-me-change-me-change-me"},
		"missing token":     {"PODIUM_JUDGE_TOKEN": "", "PODIUM_MODE": "production"},
		"shared listener":   {"PODIUM_LISTEN": "127.0.0.1:9000", "PODIUM_JUDGE_LISTEN": "127.0.0.1:9000"},
	} {
		_, err := LoadServer(env(validServer(bad)))
		if err == nil {
			t.Errorf("%s accepted", name)
			continue
		}
		if v := bad["PODIUM_JUDGE_TOKEN"]; v != "" && strings.Contains(err.Error(), v) {
			t.Errorf("%s: the error repeats the token value", name)
		}
	}
}

func TestWorkerNeedsServerAndToken(t *testing.T) {
	if _, err := LoadJudge(env(nil)); err != nil {
		t.Fatalf("calibrate settings should not need the token: %v", err)
	}
	if _, err := LoadWorker(env(nil)); err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_TOKEN is required") {
		t.Errorf("worker without token: %v", err)
	}
	c, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": strings.Repeat("j", 40), "PODIUM_SERVER_URL": "http://127.0.0.1:8081/"}))
	if err != nil || c.ServerURL != "http://127.0.0.1:8081" || c.Lanes != 2 {
		t.Errorf("worker: %+v %v", c, err)
	}
	if _, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": strings.Repeat("j", 40), "PODIUM_SERVER_URL": "127.0.0.1:8081"})); err == nil {
		t.Error("server URL without scheme accepted")
	}
}

func TestJudgeBoxBase(t *testing.T) {
	tok := strings.Repeat("j", 40)
	c, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok}))
	if err != nil || c.BoxBase != 0 {
		t.Fatalf("default: %d %v", c.BoxBase, err)
	}
	c, err = LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok, "PODIUM_JUDGE_BOX_BASE": "100", "PODIUM_JUDGE_LANES": "4"}))
	if err != nil || c.BoxBase != 100 {
		t.Fatalf("100: %d %v", c.BoxBase, err)
	}
	for _, bad := range []map[string]string{
		{"PODIUM_JUDGE_BOX_BASE": "-1"},
		{"PODIUM_JUDGE_BOX_BASE": "985"},
		{"PODIUM_JUDGE_BOX_BASE": "two"},
		{"PODIUM_JUDGE_BOX_BASE": "78", "PODIUM_JUDGE_LANES": "3"}, // 78 to 80 takes the selftest box
	} {
		bad["PODIUM_JUDGE_TOKEN"] = tok
		if _, err := LoadWorker(env(bad)); err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_BOX_BASE") {
			t.Errorf("%v accepted or unclear: %v", bad, err)
		}
	}
}
