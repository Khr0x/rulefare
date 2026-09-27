package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// readyTimeout bounds all readiness checks of one /readyz request, so a
// hung dependency makes the probe fail instead of hang.
var readyTimeout = 2 * time.Second

// Check is one readiness dependency, such as the database. Check returns
// nil when the dependency is usable. httpapi receives checks instead of
// importing the packages that implement them.
type Check struct {
	Name  string
	Check func(context.Context) error
}

// healthz reports that the process is alive. It never touches
// dependencies, so an orchestrator does not restart the process because the
// database is down.
func healthz(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, map[string]any{"status": "ok"})
}

// readyz reports whether the process can serve traffic: every check must
// pass. Failure details go to the log, never to the response.
func readyz(checks []Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		status, results := http.StatusOK, map[string]string{}
		for _, c := range checks {
			if err := c.Check(ctx); err != nil {
				Logger(r.Context()).Warn("readiness check failed", "check", c.Name, "error", err)
				results[c.Name] = "fail"
				status = http.StatusServiceUnavailable
				continue
			}
			results[c.Name] = "ok"
		}
		body := map[string]any{"status": "ready", "checks": results}
		if status != http.StatusOK {
			body["status"] = "not_ready"
		}
		writeHealth(w, status, body)
	}
}

func writeHealth(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
