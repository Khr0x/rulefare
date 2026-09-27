package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serveOnce sends one request through NewHandler and returns the response,
// its decoded JSON body and the raw logs (debug level included).
func serveOnce(t *testing.T, method, path string, checks ...Check) (*httptest.ResponseRecorder, map[string]any, string) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := httptest.NewRecorder()
	NewHandler(logger, checks...).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var body map[string]any
	if method != "HEAD" && rec.Code != http.StatusMethodNotAllowed {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
		}
	}
	return rec, body, logs.String()
}

func passing(name string) Check {
	return Check{Name: name, Check: func(context.Context) error { return nil }}
}

func failing(name, reason string) Check {
	return Check{Name: name, Check: func(context.Context) error { return errors.New(reason) }}
}

func TestHealthz(t *testing.T) {
	// A failing dependency must not affect liveness.
	rec, body, logs := serveOnce(t, "GET", "/healthz", failing("database", "down"))
	if rec.Code != 200 || body["status"] != "ok" {
		t.Fatalf("healthz = %d %v", rec.Code, body)
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !strings.Contains(logs, `"level":"DEBUG","msg":"request"`) {
		t.Fatalf("successful probes must log at debug level: %s", logs)
	}
}

func TestHealthzMethods(t *testing.T) {
	if rec, _, _ := serveOnce(t, "HEAD", "/healthz"); rec.Code != 200 {
		t.Fatalf("HEAD /healthz = %d", rec.Code)
	}
	if rec, _, _ := serveOnce(t, "POST", "/healthz"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz = %d", rec.Code)
	}
}

func TestReadyzAllChecksPass(t *testing.T) {
	rec, body, _ := serveOnce(t, "GET", "/readyz", passing("database"), passing("migrations"))
	checks, _ := body["checks"].(map[string]any)
	if rec.Code != 200 || body["status"] != "ready" || checks["database"] != "ok" || checks["migrations"] != "ok" {
		t.Fatalf("readyz = %d %v", rec.Code, body)
	}
}

func TestReadyzFailingCheck(t *testing.T) {
	rec, body, logs := serveOnce(t, "GET", "/readyz", passing("database"), failing("migrations", "2 migrations pending at host db.internal"))
	checks, _ := body["checks"].(map[string]any)
	if rec.Code != 503 || body["status"] != "not_ready" || checks["database"] != "ok" || checks["migrations"] != "fail" {
		t.Fatalf("readyz = %d %v", rec.Code, body)
	}
	if strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatal("response leaks the failure detail")
	}
	if !strings.Contains(logs, "2 migrations pending at host db.internal") || !strings.Contains(logs, `"level":"WARN","msg":"request"`) {
		t.Fatalf("logs must explain the failure and warn on the probe: %s", logs)
	}
}

func TestReadyzTimesOutHungCheck(t *testing.T) {
	old := readyTimeout
	readyTimeout = 50 * time.Millisecond
	defer func() { readyTimeout = old }()
	hung := Check{Name: "database", Check: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	start := time.Now()
	rec, _, _ := serveOnce(t, "GET", "/readyz", hung)
	if rec.Code != 503 || time.Since(start) > 2*time.Second {
		t.Fatalf("readyz = %d after %s; want a prompt 503", rec.Code, time.Since(start))
	}
}

func TestReadyzWithoutChecks(t *testing.T) {
	if rec, body, _ := serveOnce(t, "GET", "/readyz"); rec.Code != 200 || body["status"] != "ready" {
		t.Fatalf("readyz = %d %v", rec.Code, body)
	}
}
