package httpapi

import (
	"net/http"
	"runtime"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
)

// started is when this process began, for uptime in the metrics.
var started = time.Now()

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

func (d Deps) metrics(w http.ResponseWriter, r *http.Request) {
	var m Metrics
	q, err := d.Submissions.Jobs.Stats(r.Context())
	if err != nil {
		d.internalError(w, r, err)
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
	WriteJSON(w, http.StatusOK, m)
}
