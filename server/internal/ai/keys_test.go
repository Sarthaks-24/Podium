package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// testKey is a fake key written in the test itself (CLAUDE.md 3a).
// Split so tools/check-secrets.sh does not read the fake as a credential.
const testKey = "sk-" + "ant-test-0000-not-a-real-key-redaction-probe"

func TestSecretNeverPrints(t *testing.T) {
	s := NewSecret(testKey)
	entry := KeyEntry{Provider: ProviderAnthropic, Model: "m-1", Key: s, Expires: time.Unix(0, 0)}
	type hidden struct {
		key   Secret
		Other string
	}
	h := hidden{key: s, Other: "x"}
	ptr := &entry

	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%T %v"} {
		out = append(out, fmt.Sprintf(verb, s), fmt.Sprintf(verb, entry), fmt.Sprintf(verb, ptr), fmt.Sprintf(verb, h), fmt.Sprintf(verb, &s))
	}
	out = append(out, fmt.Sprint(s, entry), fmt.Sprintln(entry), s.String(), s.GoString())
	for _, v := range []any{s, entry, ptr, h, map[string]Secret{"k": s}, []KeyEntry{entry}} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	text, _ := s.MarshalText()
	out = append(out, string(text))

	var jsonLog, textLog bytes.Buffer
	for _, l := range []*slog.Logger{slog.New(slog.NewJSONHandler(&jsonLog, nil)), slog.New(slog.NewTextHandler(&textLog, nil))} {
		l.Info("key saved", "key", s, "entry", entry, "ptr", ptr, "hidden", h)
		l.Info("group", slog.Group("g", slog.Any("key", s)))
		l.Error("failed", "err", fmt.Errorf("wrapped: %v", s))
	}
	out = append(out, jsonLog.String(), textLog.String())

	for _, o := range out {
		if strings.Contains(o, testKey) || strings.Contains(o, "not-a-real-key") {
			t.Fatalf("key leaked in %q", o)
		}
	}
	if !strings.Contains(jsonLog.String(), `"key":"[redacted]"`) {
		t.Errorf("json log did not redact as expected: %s", jsonLog.String())
	}
	if s.reveal() != testKey {
		t.Error("reveal lost the key")
	}
}

func TestKeysExpireAndForget(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	k := &Keys{Now: func() time.Time { return now }}
	st := k.Put("session-token-a", KeyEntry{Provider: ProviderAnthropic, Model: "m-1", Key: NewSecret(testKey)})
	if !st.ActiveUntil.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("active until %v", st.ActiveUntil)
	}
	if b, _ := json.Marshal(st); strings.Contains(string(b), testKey) {
		t.Error("status carries the key")
	}
	// The map is keyed by a hash of the session token, never the token.
	for h := range k.m {
		if strings.Contains(h, "session-token-a") || len(h) != 64 {
			t.Errorf("map key %q", h)
		}
	}
	if _, ok := k.Get("session-token-b"); ok {
		t.Error("another session sees the key")
	}
	if e, ok := k.Get("session-token-a"); !ok || e.Key.reveal() != testKey {
		t.Fatal("key not found")
	}
	now = now.Add(2*time.Hour - time.Second)
	if _, ok := k.Status("session-token-a"); !ok {
		t.Error("expired early")
	}
	now = now.Add(time.Second)
	if _, ok := k.Get("session-token-a"); ok {
		t.Error("key outlived its two hours")
	}
	k.Put("session-token-a", KeyEntry{Provider: ProviderAnthropic, Key: NewSecret(testKey)})
	k.Forget("session-token-a")
	if _, ok := k.Get("session-token-a"); ok {
		t.Error("Forget kept the key")
	}
	k.Put("old", KeyEntry{Provider: ProviderOpenAI, Key: NewSecret(testKey)})
	now = now.Add(3 * time.Hour)
	k.Sweep()
	if len(k.m) != 0 {
		t.Errorf("sweep left %d keys", len(k.m))
	}
}

// TestKeysPerProvider: one key per provider per session; the active one
// is the one saved last; a model change keeps the expiry.
func TestKeysPerProvider(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	k := &Keys{Now: func() time.Time { return now }}
	k.Put("s", KeyEntry{Provider: ProviderAnthropic, Key: NewSecret("a")})
	now = now.Add(time.Minute)
	k.Put("s", KeyEntry{Provider: ProviderOpenAI, Key: NewSecret("o")})
	if e, _ := k.Get("s"); e.Provider != ProviderOpenAI {
		t.Errorf("active %s", e.Provider)
	}
	now = now.Add(time.Minute)
	st, ok := k.SetModel("s", ProviderAnthropic, "m-2")
	if !ok || st.Model != "m-2" || !st.ActiveUntil.Equal(time.Unix(1_800_000_000, 0).Add(KeyTTL)) {
		t.Errorf("set model %v %v", st, ok)
	}
	if e, _ := k.Get("s"); e.Provider != ProviderAnthropic || e.Key.reveal() != "a" {
		t.Errorf("active after a model change: %s", e.Provider)
	}
	if _, ok := k.SetModel("s", ProviderGemini, "x"); ok {
		t.Error("model set without a key")
	}
	k.SetModels("s", ProviderOpenAI, []Model{{ID: "x", Name: "x"}})
	if e, _ := k.GetProvider("s", ProviderOpenAI); len(e.Models) != 1 || e.ListedAt.IsZero() {
		t.Errorf("models %v", e)
	}
	k.ForgetProvider("s", ProviderAnthropic)
	if e, _ := k.Get("s"); e.Provider != ProviderOpenAI {
		t.Errorf("active after forgetting one: %s", e.Provider)
	}
	// The first key expires first; the other stays.
	now = time.Unix(1_800_000_000, 0).Add(KeyTTL)
	if _, ok := k.GetProvider("s", ProviderOpenAI); !ok {
		t.Error("the later key expired with the first")
	}
}

func TestNewProvider(t *testing.T) {
	if _, err := NewProvider(Config{Provider: ProviderFake}, false); err == nil {
		t.Error("fake provider outside testing mode")
	}
	if p, err := NewProvider(Config{Provider: ProviderFake}, true); err != nil || p == nil {
		t.Errorf("fake in testing mode: %v", err)
	}
	if _, err := NewProvider(Config{Provider: "other", Key: NewSecret(testKey)}, true); err == nil {
		t.Error("unknown provider accepted")
	}
	if _, err := NewProvider(Config{Provider: ProviderOpenAI, Model: "gpt/../x", Key: NewSecret(testKey)}, true); err == nil {
		t.Error("model id with a path accepted")
	}
	if _, err := NewProvider(Config{Provider: ProviderAnthropic}, true); err == nil {
		t.Error("missing key accepted")
	}
	for _, c := range []struct{ provider, model, want string }{
		{ProviderAnthropic, "", "*ai.Anthropic"}, {ProviderOpenAI, "gpt-x", "*ai.OpenAI"}, {ProviderGemini, "models/gemini-x", "*ai.Gemini"},
	} {
		p, err := NewProvider(Config{Provider: c.provider, Model: c.model, Key: NewSecret(testKey)}, false)
		if err != nil || fmt.Sprintf("%T", p) != c.want {
			t.Errorf("%s: %T %v", c.provider, p, err)
			continue
		}
		if s := fmt.Sprintf("%+v %#v", p, p); strings.Contains(s, testKey) {
			t.Errorf("%s provider printed the key: %s", c.provider, s)
		}
	}
	if g, _ := NewProvider(Config{Provider: ProviderGemini, Model: "models/gemini-x", Key: NewSecret(testKey)}, false); g.(*Gemini).Model != "gemini-x" {
		t.Errorf("models/ prefix kept: %q", g.(*Gemini).Model)
	}
}
