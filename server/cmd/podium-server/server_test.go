package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// A stream must outlive ReadTimeout: Go clears the read deadline once the
// request is read, so the handler's context stays alive.
func TestEventStreamOutlivesReadTimeout(t *testing.T) {
	cancelled := make(chan bool, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for i := 0; i < 6; i++ {
			select {
			case <-r.Context().Done():
				cancelled <- true
				return
			case <-time.After(150 * time.Millisecond):
			}
			io.WriteString(w, "event: clock\ndata: {}\n\n")
			f.Flush()
		}
		cancelled <- false
	})
	srv := apiServer("127.0.0.1:0", h, context.Background())
	srv.ReadTimeout = 300 * time.Millisecond // the stream runs three times as long
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	resp, err := http.Get("http://" + ln.Addr().String() + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if <-cancelled || len(b) != 6*len("event: clock\ndata: {}\n\n") {
		t.Errorf("stream cut after %d bytes", len(b))
	}
	if srv.ReadTimeout == 0 || apiServer("", h, context.Background()).ReadTimeout != 15*time.Second {
		t.Error("the API server's ReadTimeout is not 15 s")
	}
}
