package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

const (
	headerRequestID   = "X-Request-ID"
	headerTraceparent = "traceparent"
)

// A caller-supplied request ID is kept only if it is short and plain, so it
// cannot inject content into logs or headers.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// W3C Trace Context: version-traceid-parentid-flags, lowercase hex.
var validTraceparent = regexp.MustCompile(`^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`)

type ctxKey struct{}

// requestInfo is what the middleware attaches to every request context.
type requestInfo struct {
	requestID string
	traceID   string // inherited from a valid traceparent, else new
	spanID    string // always new: this server's span
	logger    *slog.Logger
}

// Logger returns the request-scoped logger, carrying request_id and
// trace_id. Outside a request it returns slog.Default().
func Logger(ctx context.Context) *slog.Logger {
	if info, ok := ctx.Value(ctxKey{}).(*requestInfo); ok {
		return info.logger
	}
	return slog.Default()
}

// RequestID returns the request ID of ctx, or "" outside a request.
func RequestID(ctx context.Context) string {
	if info, ok := ctx.Value(ctxKey{}).(*requestInfo); ok {
		return info.requestID
	}
	return ""
}

// Traceparent returns the W3C traceparent to send on outbound calls made
// while handling ctx, or "" outside a request.
func Traceparent(ctx context.Context) string {
	if info, ok := ctx.Value(ctxKey{}).(*requestInfo); ok {
		return "00-" + info.traceID + "-" + info.spanID + "-01"
	}
	return ""
}

// observe wraps next with request IDs, trace context, one access log line
// per request and panic recovery. It never logs headers, query strings or
// bodies, which may carry credentials or personal data.
func observe(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &requestInfo{
			requestID: r.Header.Get(headerRequestID),
			traceID:   traceID(r.Header.Get(headerTraceparent)),
			spanID:    randomHex(8),
		}
		if !validRequestID.MatchString(info.requestID) {
			info.requestID = randomHex(16)
		}
		info.logger = logger.With("request_id", info.requestID, "trace_id", info.traceID)
		w.Header().Set(headerRequestID, info.requestID)

		rec := &recorder{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // net/http's signal to abort silently.
				}
				info.logger.Error("panic", "panic", v, "stack", string(debug.Stack()))
				if !rec.wroteHeader {
					writeInternalError(rec, info.requestID)
				}
			}
			if !rec.wroteHeader {
				rec.status = http.StatusOK // net/http's implicit status.
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			info.logger.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", float64(time.Since(start).Microseconds())/1000,
			)
		}()
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), ctxKey{}, info)))
	})
}

// traceID keeps the trace ID of a valid traceparent so logs join the
// caller's trace; otherwise it starts a new trace.
func traceID(header string) string {
	m := validTraceparent.FindStringSubmatch(header)
	if m == nil || m[1] == "ff" || m[2] == zeros(32) || m[3] == zeros(16) {
		return randomHex(16)
	}
	return m[2]
}

func zeros(n int) string { return strings.Repeat("0", n) }

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) // crypto/rand.Read never returns an error.
	return hex.EncodeToString(b)
}

// writeInternalError answers a request whose handler panicked. F2 T3.1
// generalizes this into the API's problem+json errors.
func writeInternalError(w http.ResponseWriter, requestID string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]any{
		"type":       "about:blank",
		"title":      "Internal Server Error",
		"status":     http.StatusInternalServerError,
		"code":       "INTERNAL_ERROR",
		"request_id": requestID,
	})
}

// recorder captures the status and size of a response.
type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
