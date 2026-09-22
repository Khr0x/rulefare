//go:build linux

package engine

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"syscall"
	"testing"
	"time"
)

// Opt-in system diagnostics; run via scripts/measure-system.sh. Sampling and
// marker writes belong to the harness, outside the measured Evaluate windows.
func TestSystemIdle(t *testing.T) {
	if os.Getenv("RULEFARE_SYSTEM") != "1" {
		t.Skip("set RULEFARE_SYSTEM=1")
	}
	program, report := Compile(loadFixture[Schema](t, "schema.json"), benchmarkUniqueRules(10000))
	if program == nil {
		t.Fatal(report)
	}
	debug.FreeOSMemory()
	time.Sleep(time.Second)
	var before, after syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &before); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	time.Sleep(10 * time.Second)
	elapsed := time.Since(start)
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &after); err != nil {
		t.Fatal(err)
	}
	user := time.Duration(after.Utime.Nano() - before.Utime.Nano())
	system := time.Duration(after.Stime.Nano() - before.Stime.Nano())
	runtime.KeepAlive(program)
	t.Logf("rules=10000 unique=true wall_ns=%d user_ns=%d system_ns=%d cpu_percent=%.6f", elapsed, user, system, 100*float64(user+system)/float64(elapsed))
}

func TestSystemEvaluationIO(t *testing.T) {
	if os.Getenv("RULEFARE_SYSTEM") != "1" {
		t.Skip("set RULEFARE_SYSTEM=1")
	}
	schema := loadFixture[Schema](t, "schema.json")
	noMatch := benchmarkEvaluation()
	noMatch.Context["active"] = false
	cases := []struct {
		name   string
		rules  RuleSet
		input  Evaluation
		count  int
		status ResultStatus
	}{
		{"repeated100", benchmarkRules(100), benchmarkEvaluation(), 1000, ResultMatch},
		{"unique100", benchmarkUniqueRules(100), benchmarkEvaluation(), 1000, ResultMatch},
		{"unique10000", benchmarkUniqueRules(10000), benchmarkEvaluation(), 100, ResultMatch},
		{"invalid", benchmarkRules(100), Evaluation{Layer: "operation"}, 1000, ResultInvalidInput},
		{"no_match", benchmarkRules(100), noMatch, 1000, ResultNoMatch},
	}
	// Positive control: the trace must actually observe file and socket access.
	fmt.Fprintln(os.Stderr, "RULEFARE_IO_BEGIN control")
	_, readErr := os.ReadFile("testdata/schema.json")
	fd, socketErr := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if socketErr == nil {
		syscall.Close(fd)
	}
	fmt.Fprintln(os.Stderr, "RULEFARE_IO_END control")
	if readErr != nil || socketErr != nil {
		t.Fatalf("trace control: read=%v socket=%v", readErr, socketErr)
	}
	for _, tc := range cases {
		program, report := Compile(schema, tc.rules)
		if program == nil {
			t.Fatal(report)
		}
		// The first evaluation is inside the observed window: no warm-up can hide I/O.
		if _, err := fmt.Fprintf(os.Stderr, "RULEFARE_IO_BEGIN %s\n", tc.name); err != nil {
			t.Fatal(err)
		}
		ok := true
		for i := 0; i < tc.count; i++ {
			result, err := program.Evaluate(tc.input)
			ok = ok && result.Status == tc.status && ((err != nil) == (tc.status == ResultInvalidInput))
		}
		if _, err := fmt.Fprintf(os.Stderr, "RULEFARE_IO_END %s\n", tc.name); err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("unexpected evaluation result for %s", tc.name)
		}
		t.Logf("scenario=%s evaluations=%d status=%s", tc.name, tc.count, tc.status)
	}
}
