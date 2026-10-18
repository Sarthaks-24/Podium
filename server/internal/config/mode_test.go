package config

import (
	"strings"
	"testing"
)

func TestTestingModeNeedsNothing(t *testing.T) {
	c, err := LoadServer(env(map[string]string{"PODIUM_MODE": "testing"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://localhost:5173" || c.OwnerEmail != "owner@podium.test" || len(c.SessionSecret) < 32 ||
		c.JudgeToken != TestingJudgeToken || c.GoogleClientID != "" {
		t.Errorf("testing defaults: %+v", c)
	}
	again, _ := LoadServer(env(map[string]string{"PODIUM_MODE": "testing"}))
	if string(again.SessionSecret) == string(c.SessionSecret) {
		t.Error("testing session secret is not random per run")
	}
}

func TestTestingModeStaysOnThisMachine(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"public base URL": {"PODIUM_BASE_URL": "https://contest.example.edu"},
		"public listener": {"PODIUM_LISTEN": "0.0.0.0:8080"},
	} {
		m["PODIUM_MODE"] = "testing"
		if _, err := LoadServer(env(m)); err == nil || !strings.Contains(err.Error(), "only on this machine") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDevModeNeedsSignInSecrets(t *testing.T) {
	_, err := LoadServer(env(map[string]string{"PODIUM_MODE": "dev", "PODIUM_BASE_URL": "http://localhost:5173"}))
	if err == nil {
		t.Fatal("dev mode without Google settings accepted")
	}
	for _, v := range []string{"PODIUM_OWNER_EMAIL", "PODIUM_SESSION_SECRET", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"} {
		if !strings.Contains(err.Error(), v+" is required in dev mode") {
			t.Errorf("missing %s not reported:\n%v", v, err)
		}
	}
	if strings.Contains(err.Error(), "PODIUM_JUDGE_TOKEN") {
		t.Error("dev mode should fall back to the testing judge token")
	}
}

func TestProductionIsStrict(t *testing.T) {
	m := validServer(map[string]string{"PODIUM_MODE": "production", "PODIUM_BASE_URL": "http://contest.example.edu"})
	if _, err := LoadServer(env(m)); err == nil || !strings.Contains(err.Error(), "https in production") {
		t.Errorf("http in production: %v", err)
	}
	m = validServer(map[string]string{"PODIUM_MODE": "production", "PODIUM_JUDGE_TOKEN": TestingJudgeToken})
	if _, err := LoadServer(env(m)); err == nil || !strings.Contains(err.Error(), "public testing token") {
		t.Errorf("testing token in production: %v", err)
	}
	m = validServer(nil)
	delete(m, "PODIUM_MODE")
	delete(m, "PODIUM_JUDGE_TOKEN")
	if _, err := LoadServer(env(m)); err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_TOKEN is required in production mode") {
		t.Errorf("unset mode should mean production: %v", err)
	}
	if _, err := LoadServer(env(map[string]string{"PODIUM_MODE": "staging"})); err == nil || !strings.Contains(err.Error(), "testing, dev or production") {
		t.Errorf("unknown mode: %v", err)
	}
}

func TestWorkerTokenByMode(t *testing.T) {
	c, err := LoadWorker(env(map[string]string{"PODIUM_MODE": "testing"}))
	if err != nil || c.Token != TestingJudgeToken {
		t.Errorf("testing worker: %v %q", err, c.Token)
	}
	if _, err := LoadWorker(env(map[string]string{"PODIUM_MODE": "production", "PODIUM_JUDGE_TOKEN": TestingJudgeToken})); err == nil {
		t.Error("production worker accepted the testing token")
	}
}
