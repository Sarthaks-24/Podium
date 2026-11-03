// Package safego starts background work with panic recovery and logging.
package safego

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a goroutine, logs a panic with its stack, and closes the
// returned channel when the function exits.
func Go(log *slog.Logger, name string, fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil && log != nil {
				log.Error("background task panicked", "task", name, "panic", recovered, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
	return done
}
