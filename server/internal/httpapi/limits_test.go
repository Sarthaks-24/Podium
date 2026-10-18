package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sendSlowly posts body to url a few bytes at a time over pause each and
// returns the status, or 0 when the server closed the connection.
func sendSlowly(t *testing.T, addr, path, body string, pause time.Duration) int {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n", path, len(body))
	for i := 0; i < len(body); i += 4 {
		time.Sleep(pause)
		if _, err := conn.Write([]byte(body[i:min(i+4, len(body))])); err != nil {
			return 0
		}
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestSlowBodyOnlyOnUploadRoutes(t *testing.T) {
	read := func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", slowBody(read))
	mux.HandleFunc("POST /api", read)
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	body := strings.Repeat("x", 32) // 8 writes, 100 ms apart: 800 ms
	if code := sendSlowly(t, addr, "/upload", body, 100*time.Millisecond); code != 200 {
		t.Errorf("upload route: %d, want 200", code)
	}
	if code := sendSlowly(t, addr, "/api", body, 100*time.Millisecond); code == 200 {
		t.Errorf("ordinary route read a body slower than ReadTimeout")
	}
}

func TestInFlightCapPerSession(t *testing.T) {
	release := make(chan struct{})
	var entered sync.WaitGroup
	h := newInFlight().limit("podium_session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/slow" || r.URL.Path == "/api/events" {
			entered.Done()
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	get := func(path, cookie string) int {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "podium_session", Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return 0
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	var done sync.WaitGroup
	// MaxInFlight slow requests plus two event streams for session a.
	for i := 0; i < MaxInFlight+2; i++ {
		entered.Add(1)
		done.Add(1)
		path := "/api/slow"
		if i >= MaxInFlight {
			path = "/api/events"
		}
		go func() { defer done.Done(); get(path, "a") }()
	}
	entered.Wait()
	if code := get("/api/fast", "a"); code != http.StatusTooManyRequests {
		t.Errorf("request over the cap: %d, want 429", code)
	}
	if code := get("/api/fast", "b"); code != 200 {
		t.Errorf("another session: %d, want 200", code)
	}
	if code := get("/api/fast", ""); code != 200 {
		t.Errorf("no session: %d, want 200", code)
	}
	close(release)
	done.Wait()
	if code := get("/api/fast", "a"); code != 200 {
		t.Errorf("after the slow requests ended: %d, want 200", code)
	}
}
