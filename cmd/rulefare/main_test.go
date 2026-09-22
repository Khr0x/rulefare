package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khr0x/rulefare/engine"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "engine", "testdata", name)
}

func invoke(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func saveJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return saveFile(t, string(data))
}

func saveFile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadJSON[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestValidate(t *testing.T) {
	for _, file := range []string{"ruleset.json", "invalid_dates.json", "invalid_cel.json", "invalid_unknown_dimension.json", "invalid_outcome.json"} {
		for _, asJSON := range []bool{false, true} {
			args := []string{"rules", "validate", "--schema", fixture("schema.json"), "--ruleset", fixture(file)}
			if asJSON {
				args = append(args, "--json")
			}
			code, stdout, stderr := invoke(args...)
			valid := file == "ruleset.json"
			if (code == 0) != valid || stderr != "" || stdout == "" {
				t.Fatalf("validate %s: code %d, stdout %s, stderr %s", file, code, stdout, stderr)
			}
			if asJSON {
				var report engine.ValidationReport
				if err := json.Unmarshal([]byte(stdout), &report); err != nil || report.Valid() != valid {
					t.Fatalf("bad validation JSON: %s, %v", stdout, err)
				}
			} else if valid && stdout != "Valid ruleset.\n" || !valid && !strings.Contains(stdout, "/rules/") {
				t.Fatalf("bad human-readable report: %s", stdout)
			}
		}
	}
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name, layer string
		edit        func(map[string]any)
		status      engine.ResultStatus
		winner      string
		code        int
	}{
		{"percentage", "operation", nil, engine.ResultMatch, "HOTELBEDS_MX", 0},
		{"tiered", "split", nil, engine.ResultMatch, "CONTRACT_TIERS", 0},
		{"fixed", "operation", func(ctx map[string]any) { ctx["country"] = "US" }, engine.ResultMatch, "HOTELBEDS_FIXED", 0},
		{"no match", "split", func(ctx map[string]any) { ctx["contract"] = "other" }, engine.ResultNoMatch, "", 0},
		{"unknown layer", "missing", nil, engine.ResultInvalidInput, "", 1},
		{"missing variable", "operation", func(ctx map[string]any) { delete(ctx, "active") }, engine.ResultInvalidInput, "", 1},
		{"unknown variable", "operation", func(ctx map[string]any) { ctx["extra"] = "private-value" }, engine.ResultInvalidInput, "", 1},
		{"wrong int", "operation", func(ctx map[string]any) { ctx["amount_minor"] = "private-value" }, engine.ResultInvalidInput, "", 1},
		{"fraction", "operation", func(ctx map[string]any) { ctx["amount_minor"] = 1.5 }, engine.ResultInvalidInput, "", 1},
		{"timestamp invalid", "operation", func(ctx map[string]any) { ctx["travel_date"] = "private-value" }, engine.ResultInvalidInput, "", 1},
		{"exact large integer", "operation", func(ctx map[string]any) { ctx["amount_minor"] = json.Number("9223372036854775807") }, engine.ResultMatch, "HOTELBEDS_MX", 0},
		{"integer overflow", "operation", func(ctx map[string]any) { ctx["amount_minor"] = json.Number("9223372036854775808") }, engine.ResultInvalidInput, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := loadJSON[map[string]any](t, "context.json")
			if tc.edit != nil {
				tc.edit(ctx)
			}
			args := []string{"rules", "evaluate", "--schema", fixture("schema.json"), "--ruleset", fixture("ruleset.json"), "--context", saveJSON(t, ctx), "--layer", tc.layer, "--at", "2026-07-01T00:00:00Z"}
			code, stdout, stderr := invoke(args...)
			var result engine.Result
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatalf("bad result: %s, %v; stderr %s", stdout, err, stderr)
			}
			if code != tc.code || result.Status != tc.status || strings.Contains(stdout+stderr, "private-value") {
				t.Fatalf("unexpected result: code %d, stdout %s, stderr %s", code, stdout, stderr)
			}
			if tc.winner != "" {
				if result.Winner == nil || result.Winner.RuleID != tc.winner || stderr != "" {
					t.Fatalf("bad winner: %+v; stderr %s", result.Winner, stderr)
				}
			} else if result.Winner != nil {
				t.Fatal("unexpected winner")
			}
			if tc.status == engine.ResultInvalidInput {
				var report engine.ValidationReport
				if err := json.Unmarshal([]byte(stderr), &report); err != nil || report.Valid() || len(result.Trace.Candidates) != 0 {
					t.Fatalf("bad input report: %s, %v", stderr, err)
				}
			}
			_, again, _ := invoke(args...)
			if again != stdout {
				t.Fatal("result JSON is not deterministic")
			}
		})
	}
}

func TestEvaluateBlockingRuleErrors(t *testing.T) {
	rules := loadJSON[engine.RuleSet](t, "ruleset.json")
	duplicate := rules.Rules[3]
	duplicate.ID = "TIED"
	rules.Rules = append(rules.Rules, duplicate)
	args := []string{"rules", "evaluate", "--schema", fixture("schema.json"), "--ruleset", saveJSON(t, rules), "--context", fixture("context.json"), "--layer", "operation", "--at", "2026-07-01T00:00:00Z"}
	code, stdout, stderr := invoke(args...)
	var result engine.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || code != 1 || result.Status != engine.ResultAmbiguous || result.Winner != nil || stderr == "" {
		t.Fatalf("ambiguous match must fail: %d, %s, %s", code, stdout, stderr)
	}
	args[5] = fixture("invalid_cel.json")
	code, stdout, stderr = invoke(args...)
	var report engine.ValidationReport
	if err := json.Unmarshal([]byte(stderr), &report); err != nil || code != 1 || stdout != "" || report.Valid() {
		t.Fatalf("invalid rules must fail before evaluation: %d, %s, %s", code, stdout, stderr)
	}
}

func TestCLIUsage(t *testing.T) {
	for _, args := range [][]string{
		{}, {"serve"}, {"rules"}, {"rules", "missing"}, {"rules", "validate"},
		{"rules", "validate", "--unknown"},
		{"rules", "validate", "--schema", "s", "--ruleset", "r", "extra"},
		{"rules", "evaluate", "--schema", "s", "--ruleset", "r"},
		{"rules", "evaluate", "--schema", "s", "--ruleset", "r", "--context", "c", "--layer", "operation", "--at", "today"},
	} {
		if code, stdout, stderr := invoke(args...); code != 2 || stdout != "" || stderr == "" {
			t.Fatalf("usage %v: %d, %s, %s", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"rules", "--help"}, {"rules", "validate", "--help"}, {"rules", "evaluate", "-h"}} {
		if code, stdout, stderr := invoke(args...); code != 0 || stdout == "" || stderr != "" {
			t.Fatalf("help %v: %d, %s, %s", args, code, stdout, stderr)
		}
	}
}

func TestJSONBoundary(t *testing.T) {
	for _, content := range []string{
		"", "null", "[]", "{", "{} {}", "{} trailing", `{"unknown": 1}`,
		`{"version": 1}`, `{"variables": {"country": []}}`,
		strings.Repeat(" ", maxJSONBytes) + "{}",
	} {
		code, stdout, stderr := invoke("rules", "validate", "--schema", saveFile(t, content), "--ruleset", fixture("ruleset.json"))
		if code != 1 || stdout != "" || stderr == "" {
			t.Fatalf("invalid JSON accepted: %d, %s, %s", code, stdout, stderr)
		}
	}
	for _, content := range []string{
		`{"rules": [{"outcome": {"kind": "fixed", "fixed": {"amount": "1", "currency": "USD", "typo": true}}}]}`,
		`{"rules": [{"valid_from": "private-value"}]}`,
	} {
		code, stdout, stderr := invoke("rules", "validate", "--schema", fixture("schema.json"), "--ruleset", saveFile(t, content))
		if code != 1 || stdout != "" || stderr == "" || strings.Contains(stderr, "private-value") {
			t.Fatalf("bad nested JSON handling: %d, %s, %s", code, stdout, stderr)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	if code, stdout, stderr := invoke("rules", "validate", "--schema", missing, "--ruleset", fixture("ruleset.json")); code != 1 || stdout != "" || stderr == "" {
		t.Fatalf("missing file: %d, %s, %s", code, stdout, stderr)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestOutputFailure(t *testing.T) {
	for _, command := range []string{"validate", "evaluate"} {
		args := []string{"rules", command, "--schema", fixture("schema.json"), "--ruleset", fixture("ruleset.json")}
		if command == "evaluate" {
			args = append(args, "--context", fixture("context.json"), "--layer", "operation", "--at", "2026-07-01T00:00:00Z")
		}
		var stderr bytes.Buffer
		if code := run(args, brokenWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output unavailable") {
			t.Fatalf("output failure was ignored: %d, %s", code, stderr.String())
		}
	}
}

func TestEvaluateCostLimit(t *testing.T) {
	rules := loadJSON[engine.RuleSet](t, "ruleset.json")
	rules.Rules = rules.Rules[:1]
	rules.Rules[0].Scope = nil
	rules.Rules[0].Condition = "country.contains(supplier)"
	context := loadJSON[map[string]any](t, "context.json")
	context["country"] = strings.Repeat("a", 4096)
	context["supplier"] = strings.Repeat("a", 4096)
	code, stdout, stderr := invoke("rules", "evaluate", "--schema", fixture("schema.json"), "--ruleset", saveJSON(t, rules), "--context", saveJSON(t, context), "--layer", rules.Rules[0].Layer, "--at", "2026-07-01T00:00:00Z")
	var result engine.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || code != 1 || result.Status != engine.ResultLimitExceeded || result.Winner != nil || len(result.Trace.Candidates) != 0 || !strings.Contains(stderr, "evaluation resource limit exceeded") || strings.Contains(stdout+stderr, strings.Repeat("a", 32)) {
		t.Fatalf("cost limit must fail closed: %d %s %s", code, stdout, stderr)
	}
}
