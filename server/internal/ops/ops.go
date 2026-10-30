// Package ops serves what the people running the server watch: the health
// check that install.sh, the deploy scripts and podium-check poll, and the
// metrics organisers read on contest day.
package ops

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/Sarthaks-24/podium/internal/db/migrations"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/jobs"
)

// Handlers serve GET /healthz and GET /api/admin/metrics.
type Handlers struct {
	DB  *sql.DB
	Log *slog.Logger
	// Jobs is the judge queue; Hub the live connections. Either may be
	// nil, and their figures are then left out.
	Jobs *jobs.Service
	Hub  *events.Hub
	// Version is the server's release version.
	Version string
}

// started is when this process began, for uptime in the metrics.
var started = time.Now()

// Health is GET /healthz: whether the database answers and, for a caller
// on this machine only, the release and the judge queue.
func (d *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := d.DB.PingContext(ctx); err != nil {
		d.Log.Error("health check: database unreachable", "err", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "db": "unreachable"})
		return
	}
	out := map[string]any{"status": "ok", "db": "ok"}
	if httpx.Loopback(r) {
		// Only a caller on this machine learns the release (install.sh
		// verify, the deploy scripts): through Caddy the version stays
		// private, so a scanner cannot match it against known bugs.
		out["version"], out["schema"] = d.Version, migrations.Latest()
		// The judge queue, for podium-check's stalled-judge alert: jobs
		// waiting, the oldest one's age, and how long since
		// a judge last asked for work (null when none has since start).
		if d.Jobs != nil {
			if queued, oldest, err := d.Jobs.Waiting(ctx); err == nil {
				now := time.Now()
				out["jobs_queued"] = queued
				out["oldest_queued_s"] = int64(0)
				if queued > 0 {
					out["oldest_queued_s"] = max(0, int64(now.Sub(oldest).Seconds()))
				}
				out["judge_last_claim_s"] = nil
				if at, ok := d.Jobs.LastClaimAt(); ok {
					out["judge_last_claim_s"] = max(0, int64(now.Sub(at).Seconds()))
				}
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// Metrics is the body of GET /api/admin/metrics: what an organiser
// watches on contest day (docs/RUNBOOK.md) and what the load test
// records (tools/loadtest). Cheap enough to poll every few seconds: two
// indexed queries on the jobs table and in-memory counters.
type Metrics struct {
	ServerNow int64      `json:"server_now"`
	UptimeS   int64      `json:"uptime_s"`
	Version   string     `json:"version"`
	Queue     jobs.Stats `json:"queue"`
	Events    struct {
		Connections int `json:"connections"`
		Users       int `json:"users"`
	} `json:"events"`
	DB struct {
		MaxOpen int   `json:"max_open"`
		Open    int   `json:"open"`
		InUse   int   `json:"in_use"`
		Idle    int   `json:"idle"`
		Waits   int64 `json:"waits"`
		WaitMS  int64 `json:"wait_ms"`
	} `json:"db"`
	Process struct {
		Goroutines int    `json:"goroutines"`
		HeapMB     uint64 `json:"heap_mb"`
		SysMB      uint64 `json:"sys_mb"`
		GCCycles   uint32 `json:"gc_cycles"`
	} `json:"process"`
}

// Metrics answers GET /api/admin/metrics. It needs Jobs.
func (d *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	var m Metrics
	q, err := d.Jobs.Stats(r.Context())
	if err != nil {
		httpx.InternalError(d.Log, w, r, err)
		return
	}
	m.Queue = q
	m.ServerNow = time.Now().UnixMilli()
	m.UptimeS = int64(time.Since(started).Seconds())
	m.Version = d.Version
	if d.Hub != nil {
		m.Events.Connections = d.Hub.Connections()
		m.Events.Users = len(d.Hub.Users())
	}
	s := d.DB.Stats()
	m.DB.MaxOpen, m.DB.Open, m.DB.InUse, m.DB.Idle = s.MaxOpenConnections, s.OpenConnections, s.InUse, s.Idle
	m.DB.Waits, m.DB.WaitMS = s.WaitCount, s.WaitDuration.Milliseconds()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.Process.Goroutines = runtime.NumGoroutine()
	m.Process.HeapMB, m.Process.SysMB, m.Process.GCCycles = ms.HeapAlloc>>20, ms.Sys>>20, ms.NumGC
	httpx.WriteJSON(w, http.StatusOK, m)
}
