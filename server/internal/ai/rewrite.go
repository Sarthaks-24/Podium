package ai

// Rewrite with AI (v2.1.0 stage W): the wizard's paste mode with the old
// problem as the text. The model sees the old problem's title, settings,
// statement, reference solution and sample tests, never a hidden test, and
// what the organiser wants changed. The result is an ordinary draft.

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// rewriteSampleLimit bounds the bytes of one sample shown to the model.
const rewriteSampleLimit = 4 << 10

// rewriteLink is what a run remembers about the problem it rewrites: the
// old problem and the fingerprint of its content when the run began, which
// Replace checks later. It is also the `rewrite_of` object of a run's JSON.
type rewriteLink struct {
	ProblemID int64  `json:"problem_id"`
	Base      string `json:"base"`
	Title     string `json:"title"`
}

// checkRewriteChange validates what the organiser wrote under "What should
// change".
func checkRewriteChange(change string) error {
	switch {
	case strings.TrimSpace(change) == "":
		return &Error{Code: CodeInput, Message: "Say what should change."}
	case !utf8.ValidString(change):
		return &Error{Code: CodeInput, Message: "The text is not valid UTF-8."}
	case utf8.RuneCountInString(change) > MaxInstruction:
		return &Error{Code: CodeInput, Message: fmt.Sprintf("Keep what should change under %d characters.", MaxInstruction)}
	}
	return nil
}

// rewriteText describes problem p for the model: settings, statement,
// reference solution and the samples. read gives a sample file by hash.
// Hidden tests are not listed.
func rewriteText(p *problems.Problem, read func(sha string) (string, error)) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nTime limit: %d ms\nMemory limit: %d MB\nChecker: %s\n", p.Title, p.TimeLimitMS, p.MemoryLimitMB, p.Checker)
	if p.Checker == problems.CheckerFloat {
		fmt.Fprintf(&b, "Epsilon: %g\n", p.Epsilon)
	}
	if p.Difficulty != "" {
		fmt.Fprintf(&b, "Difficulty: %s\n", p.Difficulty)
	}
	if len(p.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(p.Tags, ", "))
	}
	if p.IsFunction() {
		b.WriteString("Students write: one function between locked code\n")
	} else {
		b.WriteString("Students write: a whole program\n")
	}
	fmt.Fprintf(&b, "\nStatement:\n%s\n\nReference solution (%s):\n%s\n", p.Statement, p.Reference.Language, p.Reference.Source)
	for n, idx := range p.Samples {
		in, out, err := rewriteSample(p, idx, read)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\nSample %d input:\n%s\nSample %d output:\n%s\n", n+1, in, n+1, out)
	}
	return b.String(), nil
}

// rewriteSample is the input and output of the sample test with number idx.
func rewriteSample(p *problems.Problem, idx int, read func(sha string) (string, error)) (in, out string, err error) {
	for _, t := range p.Tests {
		if t.Idx != idx {
			continue
		}
		if in, err = rewriteFile(t.InputSHA, t.InputSize, read); err != nil {
			return "", "", err
		}
		out, err = rewriteFile(t.OutputSHA, t.OutputSize, read)
		return in, out, err
	}
	return "(missing)", "(missing)", nil
}

func rewriteFile(sha string, size int64, read func(sha string) (string, error)) (string, error) {
	switch {
	case sha == "":
		return "(not generated yet)", nil
	case size > rewriteSampleLimit:
		return "(too large to show)", nil
	}
	return read(sha)
}

// rewriteSource describes problem p for a rewrite: the text for the model,
// and the link a run remembers. The text and the fingerprint come from one
// read of the problem, so they describe the same content.
func (d *Handlers) rewriteSource(p *problems.Problem) (text string, link *rewriteLink, err error) {
	base, err := problems.RewriteBase(p)
	if err != nil {
		return "", nil, err
	}
	text, err = rewriteText(p, func(sha string) (string, error) {
		b, err := d.Problems.Blobs.Read(sha, rewriteSampleLimit)
		return string(b), err
	})
	if err != nil {
		return "", nil, err
	}
	return text, &rewriteLink{ProblemID: p.ID, Base: base, Title: p.Title}, nil
}

// copyRewriteArchived is the refusal to rewrite an archived problem.
const copyRewriteArchived = "%s is archived. Restore it first, then rewrite it."

// loadRewrite loads the problem a rewrite starts from, or answers why it
// cannot be rewritten.
func (d *Handlers) loadRewrite(w http.ResponseWriter, r *http.Request, id int64) (string, *rewriteLink, bool) {
	p, err := d.Problems.Get(r.Context(), id)
	if err == nil && p.Archived {
		httpx.WriteError(w, http.StatusConflict, "archived", fmt.Sprintf(copyRewriteArchived, p.Title))
		return "", nil, false
	}
	var text string
	var link *rewriteLink
	if err == nil {
		text, link, err = d.rewriteSource(p)
	}
	if err != nil {
		d.editProblemError(w, r, err)
		return "", nil, false
	}
	return text, link, true
}
