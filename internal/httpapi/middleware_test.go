package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

const parentTrace = "4bf92f3577b34da6a3ce929d0e0e4736"

// do sends one request through observe(h) and returns the response and the
// decoded log lines.
func do(t *testing.T, h http.Handler, req *http.Request) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	var logs bytes.Buffer
	rec := httptest.NewRecorder()
	observe(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(rec, req)
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		lines = append(lines, entry)
	}
	return rec, lines
}

func accessLog(t *testing.T, lines []map[string]any) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, l := range lines {
		if l["msg"] == "request" {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one access log line, got %d: %v", len(found), lines)
	}
	return found[0]
}

var hello = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") })

func TestObserveLogsOneLinePerRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/things?email=someone@example.com", nil)
	req.Header.Set("Authorization", "Bearer rf_secret_value")
	rec, lines := do(t, hello, req)

	id := rec.Header().Get("X-Request-ID")
	if !hex32.MatchString(id) {
		t.Fatalf("generated request ID %q is not 32 hex chars", id)
	}
	entry := accessLog(t, lines)
	for key, want := range map[string]any{
		"level": "INFO", "method": "GET", "path": "/v1/things", "status": 200.0, "bytes": 5.0, "request_id": id,
	} {
		if entry[key] != want {
			t.Errorf("%s = %v, want %v", key, entry[key], want)
		}
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms missing: %v", entry)
	}
	if !hex32.MatchString(entry["trace_id"].(string)) {
		t.Errorf("trace_id %v is not 32 hex chars", entry["trace_id"])
	}
	raw, _ := json.Marshal(lines)
	for _, secret := range []string{"someone@example.com", "rf_secret_value"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("logs contain %q: %s", secret, raw)
		}
	}
}

func TestObserveRequestID(t *testing.T) {
	for name, tc := range map[string]struct {
		header string
		keep   bool
	}{
		"plain id kept":     {"client-123_abc.def", true},
		"newline replaced":  {"abc\ninjected", false},
		"spaces replaced":   {"two words", false},
		"too long replaced": {strings.Repeat("a", 129), false},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Request-ID", tc.header)
			rec, lines := do(t, hello, req)
			got := rec.Header().Get("X-Request-ID")
			if tc.keep != (got == tc.header) {
				t.Fatalf("request ID = %q (header %q), keep = %v", got, tc.header, tc.keep)
			}
			if !tc.keep && !hex32.MatchString(got) {
				t.Fatalf("replacement %q is not 32 hex chars", got)
			}
			if accessLog(t, lines)["request_id"] != got {
				t.Fatal("log and response disagree on request_id")
			}
		})
	}
}

func TestObserveTraceparent(t *testing.T) {
	for name, tc := range map[string]struct {
		header  string
		inherit bool
	}{
		"valid":            {"00-" + parentTrace + "-00f067aa0ba902b7-01", true},
		"missing":          {"", false},
		"version ff":       {"ff-" + parentTrace + "-00f067aa0ba902b7-01", false},
		"zero trace id":    {"00-" + strings.Repeat("0", 32) + "-00f067aa0ba902b7-01", false},
		"zero parent id":   {"00-" + parentTrace + "-" + strings.Repeat("0", 16) + "-01", false},
		"uppercase":        {"00-" + strings.ToUpper(parentTrace) + "-00f067aa0ba902b7-01", false},
		"wrong field size": {"00-" + parentTrace + "-00f067aa0ba902-01", false},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set("traceparent", tc.header)
			}
			_, lines := do(t, hello, req)
			got := accessLog(t, lines)["trace_id"].(string)
			if tc.inherit != (got == parentTrace) || !hex32.MatchString(got) {
				t.Fatalf("trace_id = %q, inherit = %v", got, tc.inherit)
			}
		})
	}
}

func TestObserveExposesRequestContext(t *testing.T) {
	var gotTraceparent, gotID string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = Traceparent(r.Context())
		gotID = RequestID(r.Context())
		Logger(r.Context()).Info("handler log")
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("traceparent", "00-"+parentTrace+"-00f067aa0ba902b7-01")
	_, lines := do(t, h, req)

	if gotID != "req-1" {
		t.Errorf("RequestID = %q", gotID)
	}
	parts := strings.Split(gotTraceparent, "-")
	if len(parts) != 4 || parts[1] != parentTrace || parts[2] == "00f067aa0ba902b7" || len(parts[2]) != 16 {
		t.Errorf("Traceparent = %q; want the inherited trace with a new span", gotTraceparent)
	}
	if lines[0]["msg"] != "handler log" || lines[0]["request_id"] != "req-1" || lines[0]["trace_id"] != parentTrace {
		t.Errorf("handler log line lacks request context: %v", lines[0])
	}
}

func TestContextHelpersOutsideRequest(t *testing.T) {
	ctx := context.Background()
	if RequestID(ctx) != "" || Traceparent(ctx) != "" || Logger(ctx) != slog.Default() {
		t.Fatal("helpers must return zero values outside a request")
	}
}

func TestObserveRecoversPanics(t *testing.T) {
	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	rec, lines := do(t, boom, httptest.NewRequest("POST", "/explode", nil))

	if rec.Code != 500 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("response = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "INTERNAL_ERROR" || body["request_id"] != rec.Header().Get("X-Request-ID") {
		t.Fatalf("body = %v", body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatal("response leaks the panic value")
	}
	if lines[0]["msg"] != "panic" || lines[0]["level"] != "ERROR" || lines[0]["panic"] != "boom" || lines[0]["stack"] == "" {
		t.Fatalf("panic log = %v", lines[0])
	}
	entry := accessLog(t, lines)
	if entry["status"] != 500.0 || entry["level"] != "ERROR" {
		t.Fatalf("access log = %v", entry)
	}
}

func TestObservePanicAfterHeadersKeepsResponse(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("late")
	})
	rec, lines := do(t, h, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Fatalf("response = %d %q; the written status must not be overwritten", rec.Code, rec.Body.String())
	}
	if accessLog(t, lines)["status"] != 202.0 {
		t.Fatal("access log must record the status actually sent")
	}
}

func TestObserveRepanicsAbortHandler(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) })
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("http.ErrAbortHandler must propagate to net/http")
		}
	}()
	observe(slog.New(slog.DiscardHandler), h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestObserveImplicitStatus(t *testing.T) {
	silent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, lines := do(t, silent, httptest.NewRequest("GET", "/", nil))
	if accessLog(t, lines)["status"] != 200.0 {
		t.Fatal("a handler that writes nothing is logged as 200")
	}
}
