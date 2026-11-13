package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/pprof"
	"sync"

	"github.com/Sarthaks-24/podium/internal/config"
)

// startCPUProfile starts the CPU profile PODIUM_CPU_PROFILE asks for and
// returns the function that ends it and closes the file. It is for a test
// run to find where the server spends its time (the file is read with
// "go tool pprof"). It is a file and never an HTTP endpoint, and it is
// refused outside testing mode here as well as by the configuration check,
// so no production path reaches pprof.StartCPUProfile. With no path set it
// does nothing.
func startCPUProfile(cfg config.Server, log *slog.Logger) (stop func(), err error) {
	if cfg.CPUProfile == "" {
		return func() {}, nil
	}
	if cfg.Mode != config.ModeTesting {
		return nil, fmt.Errorf("PODIUM_CPU_PROFILE is set, but a CPU profile is written in testing mode only; set PODIUM_MODE=testing or remove the variable")
	}
	f, err := os.OpenFile(cfg.CPUProfile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("PODIUM_CPU_PROFILE %q cannot be written: %w", cfg.CPUProfile, err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("PODIUM_CPU_PROFILE %q: %w", cfg.CPUProfile, err)
	}
	log.Warn("testing mode: writing a CPU profile until shutdown", "file", cfg.CPUProfile)
	var once sync.Once
	return func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				log.Warn("cpu profile file did not close cleanly", "file", cfg.CPUProfile, "err", err)
			}
		})
	}, nil
}
