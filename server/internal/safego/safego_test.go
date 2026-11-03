package safego

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestGoRecoversAndLogsPanic(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	done := Go(log, "test-task", func() { panic("broken task") })
	<-done
	if !strings.Contains(logs.String(), "background task panicked") || !strings.Contains(logs.String(), "test-task") {
		t.Fatalf("panic was not logged with its task name: %s", logs.String())
	}
}
