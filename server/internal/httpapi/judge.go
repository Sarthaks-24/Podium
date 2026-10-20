package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Body limits on the judge API.
const (
	maxClaimBody  = 64 << 10
	maxReportBody = 1 << 20
)

// JudgeDeps are the services the judge worker API uses.
type JudgeDeps struct {
	Log   *slog.Logger
	Jobs  *jobs.Service
	Blobs problems.Blobs
	// Token is the shared worker token (PODIUM_JUDGE_TOKEN).
	Token string
}

// NewJudge returns the judge worker API (decision D44). It is served on
// its own listener, PODIUM_JUDGE_LISTEN, so these routes never sit behind
// the public reverse proxy, and every request needs the worker token.
func NewJudge(d JudgeDeps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/judge/claim", d.claim)
	mux.HandleFunc("POST /api/judge/toolchain", d.toolchain)
	mux.HandleFunc("GET /api/judge/blob/{sha}", d.blob)
	mux.HandleFunc("PUT /api/judge/blob/{sha}", d.uploadBlob)
	mux.HandleFunc("POST /api/judge/report", d.report)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "This judge API route does not exist.")
	})
	base := Deps{Log: d.Log}
	return base.logRequests(base.recoverer(d.authenticate(mux)))
}

// authenticate compares the bearer token in constant time. Both sides are
// hashed first so the comparison does not leak the token's length.
func (d JudgeDeps) authenticate(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(d.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || d.Token == "" || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "Send the judge token as a bearer token.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJudge(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		WriteError(w, http.StatusBadRequest, "bad_request", "The body is not valid JSON for this route.")
		return false
	}
	return true
}

// protocolOK answers 426 to a judge of another protocol version (B1.3).
// The judge stops on it with this message rather than retrying.
func (d JudgeDeps) protocolOK(w http.ResponseWriter, got int) bool {
	if got == 0 {
		got = 1
	}
	if got == judgeapi.ProtocolVersion {
		return true
	}
	d.Log.Warn("judge refused: protocol version differs", "judge_protocol", got, "server_protocol", judgeapi.ProtocolVersion)
	WriteError(w, http.StatusUpgradeRequired, "protocol",
		fmt.Sprintf("judge protocol %d needed, this judge speaks %d: install the same release on both", judgeapi.ProtocolVersion, got))
	return false
}

func (d JudgeDeps) claim(w http.ResponseWriter, r *http.Request) {
	var req judgeapi.ClaimRequest
	if !decodeJudge(w, r, &req, maxClaimBody) {
		return
	}
	if !d.protocolOK(w, req.Protocol) {
		return
	}
	c, err := d.Jobs.Claim(r.Context(), req)
	if err != nil {
		d.Log.Error("claim failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not hand out a job.")
		return
	}
	if c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	d.Log.Info("job leased", "job_id", c.JobID, "kind", c.Kind, "worker", jobs.Clip(req.Worker),
		"version", jobs.Clip(req.Version), "isolate", jobs.Clip(req.Isolate))
	WriteJSON(w, http.StatusOK, c)
}

// toolchain stores the judge's toolchain report (B1.4) and answers with
// the hash its claims carry.
func (d JudgeDeps) toolchain(w http.ResponseWriter, r *http.Request) {
	var req judgeapi.ToolchainReport
	if !decodeJudge(w, r, &req, maxClaimBody) {
		return
	}
	if !d.protocolOK(w, req.Protocol) {
		return
	}
	hash, err := d.Jobs.RegisterToolchain(r.Context(), req.Toolchain)
	var bad *jobs.ReportError
	switch {
	case errors.As(err, &bad):
		WriteError(w, http.StatusBadRequest, "bad_report", bad.Reason)
		return
	case err != nil:
		d.Log.Error("toolchain report failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not record the toolchain.")
		return
	}
	t := req.Toolchain
	d.Log.Info("judge toolchain", "hash", hash, "gcc", jobs.Clip(t.GCC), "java", jobs.Clip(t.Java), "python", jobs.Clip(t.Python),
		"node", jobs.Clip(t.Node), "isolate", jobs.Clip(t.Isolate), "kernel", jobs.Clip(t.Kernel))
	WriteJSON(w, http.StatusOK, judgeapi.ToolchainReply{Hash: hash})
}

// blob serves a file only to the worker holding a lease on a job that
// needs it (the job id and lease travel in headers, which are not logged).
func (d JudgeDeps) blob(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	jobID, _ := strconv.ParseInt(r.Header.Get(judgeapi.HeaderJob), 10, 64)
	if !judgeapi.ValidSHA(sha) || jobID <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "No such blob.")
		return
	}
	ok, err := d.Jobs.MayFetch(r.Context(), jobID, r.Header.Get(judgeapi.HeaderLease), sha)
	if err != nil {
		d.Log.Error("blob check failed", "job_id", jobID, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not check the lease.")
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "No such blob for this lease.")
		return
	}
	f, err := d.Blobs.Open(sha)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "No such blob.")
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}

func (d JudgeDeps) report(w http.ResponseWriter, r *http.Request) {
	var rep judgeapi.Report
	if !decodeJudge(w, r, &rep, maxReportBody) {
		return
	}
	err := d.Jobs.Report(r.Context(), rep)
	var bad *jobs.ReportError
	switch {
	case errors.Is(err, jobs.ErrLease):
		WriteError(w, http.StatusConflict, "lease", "This lease is no longer current. Drop the job.")
	case errors.As(err, &bad):
		d.Log.Warn("report refused", "job_id", rep.JobID, "reason", jobs.Clip(bad.Reason))
		WriteError(w, http.StatusBadRequest, "bad_report", bad.Reason)
	case err != nil:
		d.Log.Error("report failed", "job_id", rep.JobID, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not record the report.")
	default:
		if rep.Final || rep.Fatal != "" {
			d.Log.Info("job closed", "job_id", rep.JobID, "fatal", rep.Fatal != "")
		}
		WriteJSON(w, http.StatusOK, judgeapi.ReportReply{LeaseSeconds: judgeapi.LeaseSeconds})
	}
}
