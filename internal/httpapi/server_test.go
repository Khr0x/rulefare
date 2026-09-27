package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// startSlow serves a handler that blocks until release is closed and
// returns once a request is in flight.
func startSlow(t *testing.T, shutdownTimeout time.Duration) (cancel func(), release chan struct{}, served <-chan error, response <-chan error) {
	t.Helper()
	ln := listen(t)
	release = make(chan struct{})
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "done")
	})
	ctx, cancel := context.WithCancel(context.Background())
	servedc := make(chan error, 1)
	go func() { servedc <- Serve(ctx, ln, mux, shutdownTimeout) }()

	responsec := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		responsec <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}
	return cancel, release, servedc, responsec
}

func TestServeDrainsInFlightRequests(t *testing.T) {
	cancel, release, served, response := startSlow(t, 5*time.Second)
	cancel()
	time.Sleep(50 * time.Millisecond) // Shutdown has begun; the request is still running.
	close(release)

	if err := <-response; err != nil {
		t.Fatalf("in-flight request failed: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve = %v, want nil", err)
	}
}

func TestServeReportsShutdownTimeout(t *testing.T) {
	cancel, release, served, _ := startSlow(t, 50*time.Millisecond)
	defer close(release)
	cancel()
	if err := <-served; err == nil {
		t.Fatal("Serve = nil, want shutdown timeout error")
	}
}

func TestServeStopsWhenContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, listen(t), NewHandler(slog.New(slog.DiscardHandler)), time.Second); err != nil {
		t.Fatalf("Serve = %v, want nil", err)
	}
}

func TestServeReturnsListenerErrors(t *testing.T) {
	ln := listen(t)
	ln.Close()
	if err := Serve(context.Background(), ln, NewHandler(slog.New(slog.DiscardHandler)), time.Second); err == nil {
		t.Fatal("Serve = nil, want error from closed listener")
	}
}
