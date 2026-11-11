package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/db"
)

// The hook that forgets provider keys is set while the services are built,
// before the scheduler starts the session sweep, so no goroutine reads it
// while it is written.
func TestNewServicesWiresSessionEndsToTheKeys(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	cfg := config.Server{OwnerEmail: "owner@college.edu", Mode: config.ModeTesting,
		TestsDir: filepath.Join(dir, "tests"), SessionSecret: []byte("0123456789abcdef0123456789abcdef")}
	svc, err := newServices(ctx, cfg, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if svc.auth.OnSessionsEnded == nil {
		t.Fatal("OnSessionsEnded is not set after newServices")
	}
	svc.aiKeys.Put("tok", ai.KeyEntry{Provider: ai.ProviderAnthropic, Key: ai.NewSecret("sk-" + "ant-test-fake")})
	sum := sha256.Sum256([]byte("tok"))
	svc.auth.OnSessionsEnded([]string{hex.EncodeToString(sum[:])})
	if _, ok := svc.aiKeys.Get("tok"); ok {
		t.Error("the key outlived the session")
	}
}
