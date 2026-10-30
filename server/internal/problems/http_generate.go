package problems

// Generating tests over HTTP. Generate inputs: the problem's generator runs
// on the judge once per planned test; when every input arrived, output
// generation and then validation follow on their own. Generate outputs
// runs the reference solution on the tests' inputs.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

type inputGenerationJSON struct {
	ID         int64                      `json:"id"`
	ProblemID  int64                      `json:"problem_id"`
	State      string                     `json:"state"`
	Planned    int                        `json:"planned"`
	QueuedAt   int64                      `json:"queued_at"`
	FinishedAt *int64                     `json:"finished_at"`
	Results    []inputResultJSON          `json:"results"`
	Compile    string                     `json:"compile_output"`
	Summary    *inputGenerationSummaryOut `json:"summary"`
	Error      *string                    `json:"error"`
	ServerNow  int64                      `json:"server_now"`
}

type inputResultJSON struct {
	Idx     int    `json:"idx"`
	Verdict string `json:"verdict"`
	Message string `json:"message,omitempty"`
}

type inputGenerationSummaryOut struct {
	Applied           int      `json:"applied"`
	GeneratingOutputs bool     `json:"generating_outputs"`
	Problems          []string `json:"problems"`
}

func inputGenerationOut(g *InputGeneration) inputGenerationJSON {
	out := inputGenerationJSON{ID: g.ID, ProblemID: g.ProblemID, State: g.State, Planned: g.Planned,
		QueuedAt: g.QueuedAt.UnixMilli(), FinishedAt: httpx.Millis(g.FinishedAt), Results: []inputResultJSON{},
		ServerNow: time.Now().UnixMilli()}
	for _, c := range g.Compile {
		out.Compile = c.Output
	}
	for _, t := range g.Tests {
		out.Results = append(out.Results, inputResultJSON{t.Idx, t.Verdict, t.Message})
	}
	if g.Summary != nil {
		out.Summary = &inputGenerationSummaryOut{Applied: g.Summary.Applied, GeneratingOutputs: g.Summary.Generating,
			Problems: httpx.NonNil(g.Summary.Problems)}
	}
	if g.Error != "" {
		out.Error = &g.Error
	}
	return out
}

func (d *Handlers) GenerateInputs(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	g, err := d.Problems.RequestInputGeneration(r.Context(), id, auth.UserFrom(r.Context()).ID)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("input generation queued", "by", auth.UserFrom(r.Context()).ID, "problem_id", id, "input_generation_id", g.ID)
	httpx.WriteJSON(w, http.StatusAccepted, inputGenerationOut(g))
}

func (d *Handlers) GetInputGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	g, err := d.Problems.LatestInputGeneration(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Inputs have not been generated for this problem.")
		return
	}
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, inputGenerationOut(g))
}

// InputGenerationEvents sends an "input_generation" event to whoever
// queued a generate_inputs job each time it changes. Its OnJobChange is a
// jobs OnChange hook: it never blocks, and one goroutine sends the events
// in the order the changes happened.
type InputGenerationEvents struct {
	Problems *Service
	Hub      *events.Hub

	once sync.Once
	ch   chan int64
}

// OnJobChange implements the jobs OnChange hook.
func (n *InputGenerationEvents) OnJobChange(jobID int64, kind string) {
	if kind != judgeapi.KindGenerateInputs || n.Hub == nil {
		return
	}
	n.once.Do(func() {
		n.ch = make(chan int64, 256)
		go n.loop()
	})
	select {
	case n.ch <- jobID:
	default: // the editor also polls; a dropped event costs nothing
	}
}

func (n *InputGenerationEvents) loop() {
	for jobID := range n.ch {
		n.send(jobID)
	}
}

func (n *InputGenerationEvents) send(jobID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	problemID, by, err := n.Problems.InputGenerationJob(ctx, jobID)
	if err != nil || by == 0 || !n.Hub.Has(by) {
		return
	}
	g, err := n.Problems.LatestInputGeneration(ctx, problemID)
	if err != nil {
		return
	}
	n.Hub.Publish(by, "input_generation", inputGenerationOut(g))
}

type generationJSON struct {
	ID         int64                  `json:"id"`
	State      string                 `json:"state"`
	All        bool                   `json:"all"`
	QueuedAt   int64                  `json:"queued_at"`
	FinishedAt *int64                 `json:"finished_at"`
	Results    []generationResultJSON `json:"results"`
	Compile    string                 `json:"compile_output"`
	Summary    *GenerationSummary     `json:"summary"`
	Error      *string                `json:"error"`
}

type generationResultJSON struct {
	Idx     int    `json:"idx"`
	Verdict string `json:"verdict"`
	TimeMS  int    `json:"time_ms"`
}

func generationOut(g *Generation) generationJSON {
	out := generationJSON{ID: g.ID, State: g.State, All: g.All, QueuedAt: g.QueuedAt.UnixMilli(), FinishedAt: httpx.Millis(g.FinishedAt),
		Results: []generationResultJSON{}, Summary: g.Summary}
	for _, c := range g.Compile {
		out.Compile = c.Output
	}
	for _, t := range g.Tests {
		out.Results = append(out.Results, generationResultJSON{t.Idx, t.Verdict, t.TimeMS})
	}
	if g.Error != "" {
		out.Error = &g.Error
	}
	return out
}

func (d *Handlers) GenerateOutputs(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	var req struct {
		All bool `json:"all"`
		// ThenValidate is the editor's Validate on a problem with missing
		// outputs: those are made first, then the validation is queued.
		ThenValidate bool `json:"then_validate"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.All && req.ThenValidate {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Send all or then_validate, not both.")
		return
	}
	var g *Generation
	var err error
	if req.ThenValidate {
		g, err = d.Problems.RequestGenerationThenValidation(r.Context(), id, auth.UserFrom(r.Context()).ID)
	} else {
		g, err = d.Problems.RequestGeneration(r.Context(), id, auth.UserFrom(r.Context()).ID, req.All)
	}
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("output generation queued", "by", auth.UserFrom(r.Context()).ID, "problem_id", id, "all", req.All, "then_validate", req.ThenValidate)
	httpx.WriteJSON(w, http.StatusAccepted, generationOut(g))
}

func (d *Handlers) GetGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	g, err := d.Problems.LatestGeneration(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Outputs have not been generated for this problem.")
		return
	}
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, generationOut(g))
}
