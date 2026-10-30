package ops

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Sarthaks-24/podium/internal/httpx"
)

// Live is the body of GET /api/admin/live, the Dashboard's Live tab
// (V4-17): the few lines an admin watches during a test run. Everything
// comes from memory or a stat call, so the tab may poll it every 5 s.
type Live struct {
	ServerNow int64 `json:"server_now"`
	// SignedInNow counts the people with a live page open (an event
	// stream), not every valid session.
	SignedInNow int `json:"signed_in_now"`
	// JudgeLastSeenS is how long ago a judge last asked for work; null
	// when none has since the server started.
	JudgeLastSeenS *int64 `json:"judge_last_seen_s"`
	Queued         int    `json:"queued"`
	// SubmitWaitP50MS is the median wait from Submit to a judge taking the
	// job, over the last 10 minutes; 0 with no submissions.
	SubmitWaitP50MS int64 `json:"submit_wait_p50_ms"`
	// DiskFreeMB is the space left where the database lives; -1 where the
	// server cannot tell (anything but Linux).
	DiskFreeMB int64 `json:"disk_free_mb"`
	// DatabaseMB is the database file and its write-ahead log.
	DatabaseMB int64 `json:"database_mb"`
	// LastBackupAt is when the newest nightly backup was taken, read from
	// the "latest" link podium-backup keeps in the backup folder; null when
	// there is none.
	LastBackupAt *int64 `json:"last_backup_at"`
}

// Live answers GET /api/admin/live. It needs Jobs.
func (d *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	q, err := d.Jobs.Stats(r.Context())
	if err != nil {
		httpx.InternalError(d.Log, w, r, err)
		return
	}
	now := time.Now()
	out := Live{ServerNow: now.UnixMilli(), Queued: q.Queued, SubmitWaitP50MS: q.SubmitWaitP50MS, DiskFreeMB: -1}
	if d.Hub != nil {
		out.SignedInNow = len(d.Hub.Users())
	}
	if at, ok := d.Jobs.LastClaimAt(); ok {
		s := max(0, int64(now.Sub(at).Seconds()))
		out.JudgeLastSeenS = &s
	}
	if d.DBPath != "" {
		out.DatabaseMB = (fileSize(d.DBPath) + fileSize(d.DBPath+"-wal")) >> 20
		if free, ok := diskFree(filepath.Dir(d.DBPath)); ok {
			out.DiskFreeMB = int64(free >> 20)
		}
	}
	if at, ok := lastBackup(d.BackupDir); ok {
		ms := at.UnixMilli()
		out.LastBackupAt = &ms
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// lastBackup reads the time of the newest backup from dir/latest, the link
// podium-backup points at the folder it just finished, named by its UTC
// start time (deploy/backup/podium-backup).
func lastBackup(dir string) (time.Time, bool) {
	if dir == "" {
		return time.Time{}, false
	}
	target, err := os.Readlink(filepath.Join(dir, "latest"))
	if err != nil {
		return time.Time{}, false
	}
	at, err := time.Parse("2006-01-02T150405Z", filepath.Base(target))
	return at, err == nil
}
