// Package judgeserver serves the judge worker API: a judge claims a job,
// fetches the files its lease allows, uploads generated outputs and
// reports results. It runs on its own listener, PODIUM_JUDGE_LISTEN, and
// every request needs the shared worker token. The request and response
// bodies are in judgeapi.
package judgeserver

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

	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Body limits on the judge API.
const (
	maxClaimBody  = 64 << 10
	maxReportBody = 1 << 20
)

// Deps are the services the judge worker API uses.
type Deps struct {
	Log   *slog.Logger
	Jobs  *jobs.Service
	Blobs problems.Blobs
	// Token is the shared worker token (PODIUM_JUDGE_TOKEN).
	Token string
}

// New returns the judge worker API. It is served on
// its own listener, PODIUM_JUDGE_LISTEN, so these routes never sit behind
// the public reverse proxy, and every request needs the worker token.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/judge/claim", d.claim)
	mux.HandleFunc("POST /api/judge/toolchain", d.toolchain)
	mux.HandleFunc("GET /api/judge/blob/{sha}", d.blob)
	mux.HandleFunc("PUT /api/judge/blob/{sha}", d.uploadBlob)
	mux.HandleFunc("POST /api/judge/report", d.report)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "This judge API route does not exist.")
	})
	return httpx.LogRequests(d.Log, httpx.Recover(d.Log, d.authenticate(mux)))
}

// authenticate compares the bearer token in constant time. Both sides are
// hashed first so the comparison does not leak the token's length.
func (d Deps) authenticate(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(d.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || d.Token == "" || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "Send the judge token as a bearer token.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJudge(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The body is not valid JSON for this route.")
		return false
	}
	return true
}

// protocolOK answers 426 to a judge of another protocol version.
// The judge stops on it with this message rather than retrying.
func (d Deps) protocolOK(w http.ResponseWriter, got int) bool {
	if got == 0 {
		got = 1
	}
	if got == judgeapi.ProtocolVersion {
		return true
	}
	d.Log.Warn("judge refused: protocol version differs", "judge_protocol", got, "server_protocol", judgeapi.ProtocolVersion)
	httpx.WriteError(w, http.StatusUpgradeRequired, "protocol",
		fmt.Sprintf("judge protocol %d needed, this judge speaks %d: install the same release on both", judgeapi.ProtocolVersion, got))
	return false
}

func (d Deps) claim(w http.ResponseWriter, r *http.Request) {
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
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The server could not hand out a job.")
		return
	}
	if c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	d.Log.Info("job leased", "job_id", c.JobID, "kind", c.Kind, "worker", jobs.Clip(req.Worker),
		"version", jobs.Clip(req.Version), "isolate", jobs.Clip(req.Isolate))
	httpx.WriteJSON(w, http.StatusOK, c)
}

// toolchain checks and logs the judge's toolchain report and
// answers with the hash its claims carry.
func (d Deps) toolchain(w http.ResponseWriter, r *http.Request) {
	var req judgeapi.ToolchainReport
	if !decodeJudge(w, r, &req, maxClaimBody) {
		return
	}
	if !d.protocolOK(w, req.Protocol) {
		return
	}
	hash, err := d.Jobs.RegisterToolchain(req.Toolchain)
	var bad *jobs.ReportError
	if errors.As(err, &bad) {
		httpx.WriteError(w, http.StatusBadRequest, "bad_report", bad.Reason)
		return
	}
	t := req.Toolchain
	d.Log.Info("judge toolchain", "hash", hash, "gcc", jobs.Clip(t.GCC), "java", jobs.Clip(t.Java), "python", jobs.Clip(t.Python),
		"node", jobs.Clip(t.Node), "isolate", jobs.Clip(t.Isolate), "kernel", jobs.Clip(t.Kernel))
	httpx.WriteJSON(w, http.StatusOK, judgeapi.ToolchainReply{Hash: hash})
}

// blob serves a file only to the worker holding a lease on a job that
// needs it (the job id and lease travel in headers, which are not logged).
func (d Deps) blob(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	jobID, _ := strconv.ParseInt(r.Header.Get(judgeapi.HeaderJob), 10, 64)
	if !judgeapi.ValidSHA(sha) || jobID <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such blob.")
		return
	}
	ok, err := d.Jobs.MayFetch(r.Context(), jobID, r.Header.Get(judgeapi.HeaderLease), sha)
	if err != nil {
		d.Log.Error("blob check failed", "job_id", jobID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The server could not check the lease.")
		return
	}
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such blob for this lease.")
		return
	}
	f, err := d.Blobs.Open(sha)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such blob.")
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}

func (d Deps) report(w http.ResponseWriter, r *http.Request) {
	var rep judgeapi.Report
	if !decodeJudge(w, r, &rep, maxReportBody) {
		return
	}
	err := d.Jobs.Report(r.Context(), rep)
	var bad *jobs.ReportError
	switch {
	case errors.Is(err, jobs.ErrLease):
		httpx.WriteError(w, http.StatusConflict, "lease", "This lease is no longer current. Drop the job.")
	case errors.As(err, &bad):
		d.Log.Warn("report refused", "job_id", rep.JobID, "reason", jobs.Clip(bad.Reason))
		httpx.WriteError(w, http.StatusBadRequest, "bad_report", bad.Reason)
	case err != nil:
		d.Log.Error("report failed", "job_id", rep.JobID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The server could not record the report.")
	default:
		if rep.Final || rep.Fatal != "" {
			d.Log.Info("job closed", "job_id", rep.JobID, "fatal", rep.Fatal != "")
		}
		httpx.WriteJSON(w, http.StatusOK, judgeapi.ReportReply{LeaseSeconds: judgeapi.LeaseSeconds})
	}
}

// uploadBlob takes an output from the worker of a generate job: lease
// checked, at most 16 MB, stored only if it hashes to its name.
func (d Deps) uploadBlob(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	jobID, _ := strconv.ParseInt(r.Header.Get(judgeapi.HeaderJob), 10, 64)
	ok, err := d.Jobs.MayUpload(r.Context(), jobID, r.Header.Get(judgeapi.HeaderLease))
	if err != nil {
		d.Log.Error("upload check failed", "job_id", jobID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The server could not check the lease.")
		return
	}
	if !ok {
		httpx.WriteError(w, http.StatusConflict, "lease", "This lease does not allow more uploads.")
		return
	}
	// The slot is reserved: give it back unless the upload is recorded.
	// The body streams to a temporary file while it is hashed.
	err = d.Blobs.UploadOutput(http.MaxBytesReader(w, r.Body, problems.MaxPackFile), sha, problems.MaxPackFile)
	switch {
	case errors.Is(err, problems.ErrUploadTooLarge):
		d.Jobs.ReleaseUpload(jobID)
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "Outputs are limited to 16 MB.")
		return
	case errors.Is(err, problems.ErrUploadHash):
		d.Jobs.ReleaseUpload(jobID)
		httpx.WriteError(w, http.StatusBadRequest, "bad_upload", "The upload does not match its hash.")
		return
	case err != nil:
		d.Jobs.ReleaseUpload(jobID)
		d.Log.Error("upload failed", "job_id", jobID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The server could not store the upload.")
		return
	}
	d.Jobs.RecordUpload(jobID, sha)
	w.WriteHeader(http.StatusNoContent)
}
