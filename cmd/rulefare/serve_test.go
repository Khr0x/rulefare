package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khr0x/rulefare/internal/postgres/pgtest"
)

// unusedDB is a syntactically valid URL for tests that fail before connecting.
const unusedDB = "postgres://rulefare@127.0.0.1:1/rulefare?sslmode=disable&connect_timeout=1"

// lockedBuffer lets the test read logs while serve is still writing them.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// logLines decodes serve's JSON log lines.
func logLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		lines = append(lines, entry)
	}
	return lines
}

func TestServeStartsAndStopsCleanly(t *testing.T) {
	dbURL := pgtest.SchemaURL(t)
	var logs lockedBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- serve(ctx, nil, envOf(map[string]string{
			"RULEFARE_HTTP_ADDR":    "127.0.0.1:0",
			"RULEFARE_DATABASE_URL": dbURL,
		}), &logs)
	}()

	var addr string
	deadline := time.Now().Add(10 * time.Second)
	for addr == "" && time.Now().Before(deadline) {
		if raw := logs.String(); raw != "" {
			for _, line := range logLines(t, raw) {
				if line["msg"] == "listening" {
					addr, _ = line["addr"].(string)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("serve never logged its address: %q", logs.String())
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("server not accepting connections on %s: %v", addr, err)
	}
	conn.Close()

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("serve = %d, want 0; logs: %s", code, logs.String())
	}
	lines := logLines(t, logs.String())
	if got := lines[len(lines)-1]["msg"]; got != "stopped" {
		t.Fatalf("last log msg = %v, want stopped", got)
	}
}

func TestServeRejectsInvalidConfiguration(t *testing.T) {
	var logs bytes.Buffer
	code := serve(context.Background(), nil, envOf(map[string]string{"RULEFARE_SHUTDOWN_TIMEOUT": "never"}), &logs)
	if code != 1 || !strings.Contains(logs.String(), "RULEFARE_SHUTDOWN_TIMEOUT") {
		t.Fatalf("serve = %d, logs %q", code, logs.String())
	}
}

func TestServeReportsListenFailure(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	var logs bytes.Buffer
	code := serve(context.Background(), nil, envOf(map[string]string{
		"RULEFARE_HTTP_ADDR":    busy.Addr().String(),
		"RULEFARE_DATABASE_URL": unusedDB,
	}), &logs)
	if code != 1 || !strings.Contains(logs.String(), "listen failed") {
		t.Fatalf("serve = %d, logs %q", code, logs.String())
	}
}

func TestServeFailsWithoutDatabase(t *testing.T) {
	var logs bytes.Buffer
	code := serve(context.Background(), nil, envOf(map[string]string{
		"RULEFARE_HTTP_ADDR":    "127.0.0.1:0",
		"RULEFARE_DATABASE_URL": "postgres://rulefare:s3cret@127.0.0.1:1/rulefare?sslmode=disable&connect_timeout=1",
	}), &logs)
	if code != 1 || !strings.Contains(logs.String(), "database unavailable") {
		t.Fatalf("serve = %d, logs %q", code, logs.String())
	}
	if strings.Contains(logs.String(), "s3cret") {
		t.Fatalf("logs leak the database password: %q", logs.String())
	}
}

// The rules subcommands must not read platform configuration: a broken
// platform environment cannot affect them.
func TestRulesIgnorePlatformConfiguration(t *testing.T) {
	t.Setenv("RULEFARE_HTTP_ADDR", "not-an-address")
	t.Setenv("RULEFARE_SHUTDOWN_TIMEOUT", "never")
	code, stdout, stderr := invoke("rules", "validate", "--schema", fixture("schema.json"), "--ruleset", fixture("ruleset.json"))
	if code != 0 || stdout != "Valid ruleset.\n" || stderr != "" {
		t.Fatalf("validate: %d, %q, %q", code, stdout, stderr)
	}
}
