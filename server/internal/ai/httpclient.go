package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"
)

// What the OpenAI and Gemini clients share: one JSON request with a
// bounded answer and a retry on overload, and the sentences every
// provider uses. The key travels only in a request header; it is never in
// a URL, so it cannot reach a log line through an error that prints one.

const (
	// maxListed bounds a model listing, so a provider that keeps handing
	// out pages cannot hold a request forever.
	maxListed = 5000
	// maxAnswer bounds a provider's answer: a pack is far smaller.
	maxAnswer = 16 << 20
	// maxErrorBody is how much of a refusal is read to classify it. The
	// body is never shown, logged or wrapped.
	maxErrorBody = 64 << 10
	// httpRetries is how often a 5xx or a rate limit is tried again.
	httpRetries = 2
)

// retryDelay is the wait before retry n (1, 2); tests shorten it.
var retryDelay = func(n int) time.Duration { return time.Duration(n) * time.Second }

func errRateLimited() *Error {
	return &Error{Code: CodeRateLimited, Message: "Rate limited by the provider; try again in a minute."}
}

func errUnavailable() *Error {
	return &Error{Code: CodeUnavailable, Message: "The provider is unavailable. Try again in a few minutes."}
}

// transportError is the error of a call that got no HTTP answer.
func transportError(ctx context.Context) *Error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &Error{Code: CodeTimeout, Message: "The provider took too long to answer. Try again."}
	case ctx.Err() != nil:
		return &Error{Code: CodeCanceled, Message: "The request was cancelled."}
	}
	return errUnavailable()
}

// httpFailure is a non-2xx answer: the status and the start of the body,
// for the provider's own mapping to a sentence.
type httpFailure struct {
	status int
	body   []byte
}

// noRedirect keeps the key header from following a redirect anywhere.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// apiClient is the http.Client of the two net/http providers: no client
// timeout (the caller's context bounds each call, 3 minutes for a
// generation) and no redirects.
func apiClient(hc *http.Client) *http.Client {
	if hc != nil {
		return hc
	}
	return &http.Client{CheckRedirect: noRedirect}
}

// doJSON sends one request with header set, decoding a 2xx answer into
// out. A non-2xx answer comes back as *httpFailure; a call that got no
// answer as *Error. retryable says which failures are worth another try.
func doJSON(ctx context.Context, hc *http.Client, method, url string, header map[string]string, body any, out any,
	retryable func(*httpFailure) bool) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return errUnavailable()
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := hc.Do(req)
		if err != nil {
			// The error names the URL (never the key, which is a header);
			// it is dropped all the same.
			return transportError(ctx)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(out)
			resp.Body.Close()
			if err != nil {
				if ctx.Err() != nil {
					return transportError(ctx)
				}
				return errUnavailable()
			}
			return nil
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
		f := &httpFailure{status: resp.StatusCode, body: raw}
		if attempt >= httpRetries || !retryable(f) {
			return f
		}
		select {
		case <-ctx.Done():
			return transportError(ctx)
		case <-time.After(retryDelay(attempt + 1)):
		}
	}
}

func (f *httpFailure) Error() string { return http.StatusText(f.status) }

// strictSchema returns a copy of a JSON schema in the form OpenAI's strict
// mode wants: every object lists all its properties as required and
// allows no others. PackSchema already has that form; the copy makes the
// rule hold for any schema handed in.
func strictSchema(s map[string]any) map[string]any {
	out := make(map[string]any, len(s))
	for k, v := range s {
		out[k] = v
	}
	if props, ok := s["properties"].(map[string]any); ok {
		cp := make(map[string]any, len(props))
		req := make([]string, 0, len(props))
		for k, v := range props {
			if sub, ok := v.(map[string]any); ok {
				v = strictSchema(sub)
			}
			cp[k] = v
			req = append(req, k)
		}
		sort.Strings(req)
		out["properties"], out["required"], out["additionalProperties"] = cp, req, false
	}
	if items, ok := s["items"].(map[string]any); ok {
		out["items"] = strictSchema(items)
	}
	return out
}
