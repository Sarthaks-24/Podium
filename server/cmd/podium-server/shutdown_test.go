package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/config"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/schedule"
)

// Stopping the server while the retention pass is inside a batch waits for
// that batch to commit; only then may the database close.
func TestShutdownWaitsForARetentionBatch(t *testing.T) {
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
	old := time.Now().Add(-40 * 24 * time.Hour).UnixMilli()
	if _, err := database.ExecContext(ctx, `INSERT INTO jobs (id, kind, priority, state, payload, created_at)
		VALUES (1, 'run', 10, 'done', '{"inputs":[]}', ?)`, old); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Server{OwnerEmail: "owner@college.edu", AllowedDomains: []string{"college.edu"}, Mode: config.ModeTesting,
		TestsDir: filepath.Join(dir, "tests"), SessionSecret: []byte("0123456789abcdef0123456789abcdef")}
	svc, err := newServices(ctx, cfg, database, log)
	if err != nil {
		t.Fatal(err)
	}
	inBatch, release := make(chan struct{}), make(chan struct{})
	svc.retention.Hold = func() {
		close(inBatch)
		<-release
	}
	tasks := svc.tasks()
	for i := range tasks {
		if tasks[i].Name == "retention" {
			tasks[i].Delay = 0
		}
	}
	sched := schedule.Start(tasks)
	<-inBatch

	stopped := make(chan struct{})
	go func() {
		shutdown(log, sched, &http.Server{}, &http.Server{})
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("shutdown returned while a retention batch was open")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not return after the batch")
	}
	var payload string
	if err := database.QueryRowContext(ctx, "SELECT payload FROM jobs WHERE id = 1").Scan(&payload); err != nil || payload != "{}" {
		t.Errorf("after shutdown the batch is %q, %v; want committed ({})", payload, err)
	}
}
