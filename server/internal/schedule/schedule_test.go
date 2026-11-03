package schedule

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTasksRunAndStopWaits(t *testing.T) {
	var fast, slow atomic.Int64
	running := make(chan struct{})
	finished := make(chan struct{})
	s := Start([]Task{
		{Name: "fast", Every: time.Millisecond, Run: func(context.Context) { fast.Add(1) }},
		{Name: "slow", Every: time.Hour, Run: func(ctx context.Context) {
			if slow.Add(1) == 1 {
				close(running)
				<-ctx.Done()
				time.Sleep(50 * time.Millisecond) // the end of its transaction
				close(finished)
			}
		}},
		{Name: "later", Delay: time.Hour, Run: func(context.Context) { t.Error("a delayed task ran before its delay") }},
	})
	<-running
	for fast.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	if !s.Stop(5 * time.Second) {
		t.Fatal("Stop timed out")
	}
	select {
	case <-finished:
	default:
		t.Fatal("Stop returned while a run was still going")
	}
	n := fast.Load()
	time.Sleep(20 * time.Millisecond)
	if fast.Load() != n || slow.Load() != 1 {
		t.Error("a task ran after Stop")
	}
}

func TestStopGivesUpAfterTimeout(t *testing.T) {
	release := make(chan struct{})
	running := make(chan struct{})
	s := Start([]Task{{Name: "stuck", Every: time.Hour, Run: func(context.Context) { close(running); <-release }}})
	<-running
	if s.Stop(20 * time.Millisecond) {
		t.Error("Stop reported done while a run was stuck")
	}
	close(release)
}

func TestPanickingTaskLogsAndLoopContinues(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var calls atomic.Int64
	continued := make(chan struct{})
	s := Start([]Task{{Name: "recoverable", Every: time.Millisecond, Run: func(context.Context) {
		if calls.Add(1) == 1 {
			panic("task failure")
		}
		select {
		case <-continued:
		default:
			close(continued)
		}
	}}})
	select {
	case <-continued:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not continue after panic")
	}
	s.Stop(time.Second)
	if !strings.Contains(logs.String(), "task failure") {
		t.Fatalf("panic was not logged: %s", logs.String())
	}
}
